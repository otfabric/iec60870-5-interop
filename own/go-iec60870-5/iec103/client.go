// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/client"
	"github.com/otfabric/go-iec60870-5/link"
)

// clientOptions are the flags of one client operation.
type clientOptions struct {
	op                               string
	host                             string
	port, ca, linkAddr               int
	timeoutMs, connectMs, collectMs  int
	holdMs, scn, fun, inf, dco, rii  int
	durationMs, maxAsdus             int
	broadcast                        bool
	time                             time.Time
	gin                              iec103.GIN
	kod                              int
	value                            iec103.Value
	svcFun, fan                      int // -1: not given
	genericType, genericSize         int
	genericText                      string
	timeout, connectTimeout, collect time.Duration
}

func parseDCO(s string) (int, error) {
	switch strings.ToLower(s) {
	case "off":
		return 1, nil
	case "on":
		return 2, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 3 {
		return 0, usagef("--dco: must be 0..3, off or on, got '%s'", s)
	}
	return n, nil
}

// parseGIN reads "G.E", each 0..255.
func parseGIN(s string) (iec103.GIN, error) {
	g, e, ok := strings.Cut(s, ".")
	gn, err1 := strconv.Atoi(g)
	en, err2 := strconv.Atoi(e)
	if !ok || err1 != nil || err2 != nil || gn < 0 || gn > 255 || en < 0 || en > 255 {
		return iec103.GIN{}, usagef("--gin: '%s' is not GROUP.ENTRY, each 0..255", s)
	}
	return iec103.GIN{Group: uint8(gn), Entry: uint8(en)}, nil
}

// parseGenericValue reads --value for --type and --size: the text (1), an
// integer that fits (3, 4: --size 1, 2 or 4) or a number (7: --size 4).
func parseGenericValue(typ, size int, s string) (iec103.Value, error) {
	switch typ {
	case 1:
		raw, _ := json.Marshal(s)
		v, err := genericValue(1, size, raw)
		if err != nil {
			return v, usagef("--value: %v", err)
		}
		return v, nil
	case 3, 4:
		if size != 1 && size != 2 && size != 4 {
			return iec103.Value{}, usagef("--size: %d is not 1, 2 or 4 for --type %d", size, typ)
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return iec103.Value{}, usagef("--value: '%s' is not an integer", s)
		}
		v, err := intValue(typ, size, n)
		if err != nil {
			return v, usagef("--value: %v", err)
		}
		return v, nil
	case 7:
		if size != 4 {
			return iec103.Value{}, usagef("--size: %d is not 4 for --type 7", size)
		}
		x, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return iec103.Value{}, usagef("--value: '%s' is not a number of single precision", s)
		}
		return iec103.Float32s(float32(x)), nil
	}
	return iec103.Value{}, usagef("--type: %d is not 1, 3, 4 or 7", typ)
}

func parseClient(op string, a *args) (clientOptions, error) {
	o := clientOptions{op: op, svcFun: -1, fan: -1}
	if !slices.Contains(clientOperations, op) {
		return o, usagef("client: unknown or unsupported operation '%s'", op)
	}
	var err error
	ints := []struct {
		dst          *int
		name         string
		def, lo, max int
	}{
		{&o.port, "--port", defaultPort, 1, 65535},
		{&o.ca, "--common-address", 1, 0, 255},
		{&o.timeoutMs, "--timeout-ms", 5000, 1, 3600000},
		{&o.connectMs, "--connect-timeout-ms", 5000, 1, 3600000},
		{&o.collectMs, "--collect-ms", 0, 0, 3600000},
	}
	if o.host, err = a.str("--host", ""); err != nil {
		return o, err
	}
	for _, f := range ints {
		if *f.dst, err = a.rng(f.name, f.def, f.lo, f.max); err != nil {
			return o, err
		}
	}
	if o.linkAddr, err = a.link(); err != nil {
		return o, err
	}
	switch op {
	case "connect":
		o.holdMs, err = a.rng("--hold-ms", 0, 0, 3600000)
	case "interrogate":
		o.scn, err = a.rng("--scn", 0, 0, 255)
	case "clock-sync":
		o.broadcast = a.flag("--broadcast")
		var s string
		if s, err = a.str("--time", ""); err == nil {
			o.time, err = parseTime(s)
		}
	case "command":
		if o.fun, err = a.required(op, "--fun", 0, 255); err != nil {
			break
		}
		if o.inf, err = a.required(op, "--inf", 0, 255); err != nil {
			break
		}
		var s string
		if s, err = a.str("--dco", ""); err != nil {
			break
		}
		if s == "" {
			err = usagef("command: --dco is required")
			break
		}
		if o.dco, err = parseDCO(s); err != nil {
			break
		}
		o.rii, err = a.rng("--rii", 1, 0, 255)
	case "monitor":
		if o.durationMs, err = a.rng("--duration-ms", 1000, 0, 3600000); err == nil {
			o.maxAsdus, err = a.rng("--max-asdus", 0, 0, 1000000)
		}
	case "generic-read", "generic-write":
		err = parseGeneric(&o, a)
	case "disturbance-list", "disturbance-transfer":
		if o.svcFun, err = a.rng("--fun", -1, 0, 255); err != nil {
			break
		}
		if op == "disturbance-transfer" {
			o.fan, err = a.required(op, "--fan", 0, 65535)
		}
	}
	if err != nil {
		return o, err
	}
	if err := a.finish("client " + op); err != nil {
		return o, err
	}
	if o.host == "" {
		return o, usagef("client: --host is required")
	}
	// client.Config: a common address of zero means the link address. The
	// library's own procedures (generic services, disturbance data) send
	// and match with the client's common address.
	if o.ca == 0 && o.linkAddr != 0 && (strings.HasPrefix(op, "generic-") || strings.HasPrefix(op, "disturbance-")) {
		return o, usagef("client %s: go-iec60870-5 takes common address 0 for the link address: --common-address 0 needs --link-address 0", op)
	}
	o.timeout = time.Duration(o.timeoutMs) * time.Millisecond
	o.connectTimeout = time.Duration(o.connectMs) * time.Millisecond
	o.collect = time.Duration(o.collectMs) * time.Millisecond
	return o, nil
}

func parseGeneric(o *clientOptions, a *args) error {
	s, err := a.str("--gin", "")
	if err != nil {
		return err
	}
	if s == "" {
		return usagef("client %s: --gin is required", o.op)
	}
	if o.gin, err = parseGIN(s); err != nil {
		return err
	}
	if o.kod, err = a.rng("--kod", int(iec103.KODActualValue), 0, 255); err != nil {
		return err
	}
	if o.op == "generic-read" {
		return nil
	}
	if o.genericType, err = a.required(o.op, "--type", 0, 255); err != nil {
		return err
	}
	if o.genericSize, err = a.required(o.op, "--size", 1, 255); err != nil {
		return err
	}
	if !a.has("--value") {
		return usagef("generic-write: --value is required")
	}
	if o.genericText, err = a.str("--value", ""); err != nil {
		return err
	}
	o.value, err = parseGenericValue(o.genericType, o.genericSize, o.genericText)
	return err
}

func runClient(op string, a *args) (int, error) {
	o, err := parseClient(op, a)
	if err != nil {
		return exitUsage, err
	}
	r := &result{op: op, start: time.Now()}
	code := execute(r, o)
	r.emit()
	return code, nil
}

// result is the client result document.
type result struct {
	op            string
	ok            bool
	errCode       string
	errMsg        string
	connected     bool
	confirmations []object
	terminated    bool
	asdus         []object
	generic       object
	disturbances  []object
	disturbance   object
	start         time.Time
}

func (r *result) fail(code, msg string, exit int) int {
	r.ok, r.errCode, r.errMsg = false, code, msg
	return exit
}

func (r *result) confirm(cot int, negative bool) {
	r.confirmations = append(r.confirmations, object{"cot": cot, "negative": negative})
}

func (r *result) document() object {
	var e any
	if !r.ok {
		e = object{"code": r.errCode, "message": r.errMsg}
	}
	if r.confirmations == nil {
		r.confirmations = []object{}
	}
	if r.asdus == nil {
		r.asdus = []object{}
	}
	doc := object{
		"schemaVersion": schemaVersion, "adapter": adapter, "operation": r.op,
		"ok": r.ok, "error": e, "connected": r.connected,
		"confirmations": r.confirmations, "terminated": r.terminated,
		"asdus": r.asdus, "elapsedMs": time.Since(r.start).Milliseconds(),
	}
	if r.generic != nil {
		doc["generic"] = r.generic
	}
	if r.disturbances != nil {
		doc["disturbances"] = r.disturbances
	}
	if r.disturbance != nil {
		doc["disturbance"] = r.disturbance
	}
	return doc
}

func (r *result) emit() { emit(r.document()) }

// recorder keeps the octets of every ASDU the link layer received, in the
// order of the line: what the library hands it, decodable or not.
type recorder struct {
	mu     sync.Mutex
	raw    [][]byte
	notify chan struct{}
	// broadcast is closed when the writer of the link layer hands a
	// frame to the broadcast address to the transport.
	broadcast     chan struct{}
	broadcastOnce sync.Once
}

func newRecorder() *recorder {
	return &recorder{notify: make(chan struct{}, 1), broadcast: make(chan struct{})}
}

func (rc *recorder) frame(tx bool, f ft12.Frame) {
	if tx && f.Kind == ft12.KindVariable && f.Addr == client.BroadcastAddr {
		rc.broadcastOnce.Do(func() { close(rc.broadcast) })
	}
	if tx || f.Kind != ft12.KindVariable || len(f.Data) == 0 {
		return
	}
	rc.mu.Lock()
	rc.raw = append(rc.raw, append([]byte{}, f.Data...))
	rc.mu.Unlock()
	select {
	case rc.notify <- struct{}{}:
	default:
	}
}

func (rc *recorder) take() [][]byte {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	out := rc.raw
	rc.raw = nil
	return out
}

// session is one client operation on one connection.
type session struct {
	r       *result
	o       clientOptions
	c       *client.Client
	rec     *recorder
	pending [][]byte
}

var errLost = errors.New("connection lost")

// pump records the ASDUs that arrive until done returns true for one of
// them (finished), the time d passed, or the connection or the link was
// lost (errLost). ASDUs after the one that finished stay pending.
func (s *session) pump(d time.Duration, done func(object) bool) (bool, error) {
	if d <= 0 {
		return false, nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		s.pending = append(s.pending, s.rec.take()...)
		for len(s.pending) > 0 {
			doc := asduDoc(s.pending[0])
			s.pending = s.pending[1:]
			s.r.asdus = append(s.r.asdus, doc)
			if done != nil && done(doc) {
				return true, nil
			}
		}
		select {
		case <-s.rec.notify:
		case <-timer.C:
			s.pending = append(s.pending, s.rec.take()...)
			return false, nil
		case <-s.c.Done():
			s.flush()
			return false, errLost
		case <-tick.C:
			if s.c.State() != iec103.StateUp {
				s.flush()
				return false, errLost
			}
		}
	}
}

// flush records everything received so far.
func (s *session) flush() {
	s.pending = append(s.pending, s.rec.take()...)
	for _, raw := range s.pending {
		s.r.asdus = append(s.r.asdus, asduDoc(raw))
	}
	s.pending = nil
}

// settle fetches the class 1 data the reset queued (the identification)
// before the request: it records until nothing but cyclic data arrived for
// a moment, at most --timeout-ms.
func (s *session) settle() error {
	const quiet = 250 * time.Millisecond
	limit := time.Now().Add(s.o.timeout)
	for time.Now().Before(limit) {
		busy := false
		finished, err := s.pump(min(quiet, time.Until(limit)), func(d object) bool {
			busy = d["cot"] != 2
			return busy
		})
		if err != nil {
			return err
		}
		if !finished && !busy {
			return nil
		}
	}
	return nil
}

// lost classifies the end of the line during an operation.
func (s *session) lost(err error) int {
	if s.c.Err() != nil {
		return s.r.fail("connection-lost", "the connection closed: "+s.c.Err().Error(), exitOperationFailed)
	}
	if err != nil && !errors.Is(err, errLost) {
		return s.r.fail("connection-lost", err.Error(), exitOperationFailed)
	}
	return s.r.fail("connection-lost", "the link is no longer up (the station stopped answering)", exitOperationFailed)
}

// requestFailed maps the error of a request the library sent or ran.
func (s *session) requestFailed(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return s.r.fail("timeout", "the station did not answer in time: "+err.Error(), exitOperationFailed)
	case errors.Is(err, iec103.ErrLinkDown), errors.Is(err, iec103.ErrClosed), s.c.Err() != nil:
		return s.lost(err)
	}
	return s.r.fail("failed", err.Error(), exitOperationFailed)
}

func execute(r *result, o clientOptions) int {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(o.host, strconv.Itoa(o.port)), o.connectTimeout)
	if err != nil {
		return r.fail("connect-failed", err.Error(), exitConnectFailed)
	}
	rec := newRecorder()
	linkTimeout := min(500*time.Millisecond, o.timeout)
	c, err := client.New(conn, client.Config{
		Link:     link.Config{Timeout: linkTimeout, OnFrame: rec.frame},
		LinkAddr: uint8(o.linkAddr), CommonAddr: uint8(o.ca),
		// The contract's operations run the synchronization and the
		// interrogation themselves, never after the start-up.
		NoInitTimeSync: true, NoInitGI: true,
		PollInterval:   20 * time.Millisecond,
		RequestTimeout: o.timeout,
		RetryInterval:  linkTimeout,
	})
	if err != nil {
		_ = conn.Close()
		return r.fail("failed", err.Error(), exitOperationFailed)
	}
	defer func() { _ = c.Close() }()
	s := &session{r: r, o: o, c: c, rec: rec}

	// ---- link start-up: request status of link, reset of the CU ----
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	err = c.WaitUp(ctx)
	cancel()
	if err != nil {
		s.flush()
		if c.Err() != nil || !errors.Is(err, context.DeadlineExceeded) {
			return r.fail("connection-lost", "the connection closed during the link start-up: "+err.Error(), exitConnectFailed)
		}
		return r.fail("link-init-timeout", "the station did not confirm the reset of the communication unit", exitConnectFailed)
	}
	r.connected = true
	if err := s.settle(); err != nil {
		return s.lost(err)
	}
	if strings.HasPrefix(o.op, "generic-") || strings.HasPrefix(o.op, "disturbance-") {
		return s.service()
	}

	// ---- the operation ----
	var done func(object) bool
	limit := o.timeout
	send := func(a *iec103.ASDU) error {
		ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
		defer cancel()
		return c.Send(ctx, a)
	}
	switch o.op {
	case "connect":
		limit = time.Duration(o.holdMs) * time.Millisecond
	case "monitor":
		n := 0
		done = func(object) bool { n++; return o.maxAsdus > 0 && n >= o.maxAsdus }
		limit = time.Duration(o.durationMs) * time.Millisecond
	case "interrogate":
		// ASDU 7 with the scan number of --scn: Client.Interrogate picks
		// its own.
		if err := send(iec103.New(iec103.CauseGI, uint8(o.ca), iec103.FunGlobal, iec103.InfGIEnd, iec103.GIInit{SCN: uint8(o.scn)})); err != nil {
			return s.requestFailed(err)
		}
		done = func(d object) bool {
			if d["typeId"] == int(iec103.TypeGITermination) && objField(d, "scn") == o.scn {
				r.terminated = true
				return true
			}
			return false
		}
	case "clock-sync":
		// The time of --time as given: Client.ClockSync and
		// BroadcastClockSync advance it by the time it waited for the link.
		ts := iec103.New(iec103.CauseTimeSync, uint8(o.ca), iec103.FunGlobal, iec103.InfTimeSync, iec103.TimeSync{Time: cp.At(o.time)})
		if o.broadcast {
			ts.CommonAddr = iec103.CommonAddrGlobal
			if err := c.Broadcast(ts); err != nil {
				return s.requestFailed(err)
			}
			// Client.Broadcast returns when the link loop queued the
			// frame for the writer, and Close drops a frame still queued:
			// wait until it was written before the line closes.
			select {
			case <-rec.broadcast:
			case <-time.After(o.timeout):
				return r.fail("timeout", "the broadcast frame was not written in time", exitOperationFailed)
			}
			limit = 0
			break
		}
		if err := send(ts); err != nil {
			return s.requestFailed(err)
		}
		done = func(d object) bool {
			if d["typeId"] == int(iec103.TypeTimeSync) && d["cot"] == int(iec103.CauseTimeSync) {
				r.confirm(int(iec103.CauseTimeSync), false)
				return true
			}
			return false
		}
	case "command":
		// ASDU 20 with the RII of --rii, and DCO 0 and 3 as they are:
		// Client.Command picks its own RII and refuses those.
		cmd := iec103.New(iec103.CauseGeneralCommand, uint8(o.ca), iec103.FunctionType(o.fun), iec103.InfoNumber(o.inf),
			iec103.GeneralCommand{DCO: iec103.DPI(o.dco), RII: uint8(o.rii)})
		if err := send(cmd); err != nil {
			return s.requestFailed(err)
		}
		done = func(d object) bool {
			t, cot := d["typeId"], d["cot"]
			if (t == 1 || t == 2) && (cot == 20 || cot == 21) && objField(d, "fun") == o.fun &&
				objField(d, "inf") == o.inf && objField(d, "sin") == o.rii {
				r.confirm(cot.(int), cot == 21)
				return true
			}
			return false
		}
	}

	finished, err := s.pump(limit, done)
	if err != nil {
		return s.lost(err)
	}
	switch {
	case o.op == "connect" || o.op == "monitor":
		// The link must still be up after the hold or the duration.
		if c.Err() != nil || c.State() != iec103.StateUp {
			return s.lost(errLost)
		}
	case o.broadcast:
	case !finished:
		return r.fail("timeout", "the station did not answer in time", exitOperationFailed)
	}
	if _, err := s.pump(o.collect, nil); err != nil && (o.op == "connect" || o.op == "monitor") {
		return s.lost(err)
	}
	if o.op == "command" && r.confirmations[0]["negative"] == true {
		return r.fail("negative-confirmation", "the station refused the command (cause 21)", exitOperationFailed)
	}
	r.ok = true
	return exitOK
}

// service runs a generic service or the transmission of disturbance data
// with the library's own procedure.
func (s *session) service() int {
	r, o, c := s.r, s.o, s.c
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	fun := iec103.FunctionType(o.svcFun)
	if o.svcFun < 0 {
		// The library's choice: the function type of the device, from its
		// identification; the global function type without one.
		fun = iec103.FunGlobal
		id := c.IdentificationFor(iec103.CauseResetCU)
		if id == nil {
			id = c.Identification()
		}
		if id != nil {
			fun = id.Fun
		}
	}
	var err error
	switch o.op {
	case "generic-read", "generic-write":
		var res *client.GenericResult
		if o.op == "generic-read" {
			res, err = c.ReadEntry(ctx, o.gin, iec103.KOD(o.kod))
		} else {
			res, err = c.GenericWrite(ctx, iec103.InfWriteExecute, iec103.DataSet{GIN: o.gin, KOD: iec103.KOD(o.kod), Value: o.value})
		}
		if res != nil && res.Cause != 0 {
			cot := int(res.Cause)
			negative := res.Cause == iec103.CauseGenericReadInvalid || res.Cause == iec103.CauseGenericWriteNack
			r.confirm(cot, negative)
			r.generic = object{"cot": cot, "sets": setDocs(res.Sets)}
			if negative {
				s.flush()
				return r.fail("negative-confirmation", "the station answered with cause "+strconv.Itoa(cot), exitOperationFailed)
			}
		}
	case "disturbance-list":
		var faults []iec103.RecordedFault
		faults, err = c.DisturbanceList(ctx, fun)
		if err == nil {
			r.disturbances = []object{}
			for _, f := range faults {
				r.disturbances = append(r.disturbances, faultDoc(f))
			}
		}
	case "disturbance-transfer":
		var rec *iec103.Disturbance
		rec, err = c.Disturbance(ctx, fun, uint16(o.fan))
		if rec != nil {
			r.disturbance = disturbanceDoc(rec)
		}
	}
	s.flush()
	if err != nil {
		return s.requestFailed(err)
	}
	if _, err := s.pump(o.collect, nil); err != nil {
		return s.lost(err)
	}
	r.ok = true
	return exitOK
}

// objField is an integer field of the first object of an ASDU document,
// -1 when there is none.
func objField(d object, name string) int {
	objs, _ := d["objects"].([]object)
	if len(objs) == 0 {
		return -1
	}
	if v, ok := objs[0][name].(int); ok {
		return v
	}
	return -1
}

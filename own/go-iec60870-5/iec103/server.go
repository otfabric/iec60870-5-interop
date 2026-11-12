// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec103"
	"github.com/otfabric/go-iec60870-5/iec103/server"
	"github.com/otfabric/go-iec60870-5/link"
)

// serverOptions are the flags of server.
type serverOptions struct {
	fixture, bind, readyFile string
	port, linkAddr, ca       int
}

func parseServer(a *args) (serverOptions, error) {
	var o serverOptions
	var err error
	if o.fixture, err = a.str("--fixture", "/fixtures/baseline/fixture.json"); err != nil {
		return o, err
	}
	if o.bind, err = a.str("--bind-address", "0.0.0.0"); err != nil {
		return o, err
	}
	if o.port, err = a.rng("--port", defaultPort, 0, 65535); err != nil {
		return o, err
	}
	if o.readyFile, err = a.str("--ready-file", "/run/iec60870-5-interop/ready"); err != nil {
		return o, err
	}
	if o.linkAddr, err = a.link(); err != nil {
		return o, err
	}
	if o.ca, err = a.rng("--common-address", -1, 0, 254); err != nil {
		return o, err
	}
	if err := a.finish("server"); err != nil {
		return o, err
	}
	if net.ParseIP(o.bind) == nil {
		return o, usagef("--bind-address: '%s' is not an address", o.bind)
	}
	return o, nil
}

func runServer(a *args) (int, error) {
	o, err := parseServer(a)
	if err != nil {
		return exitUsage, err
	}
	f, err := loadFixture(o.fixture)
	if err != nil {
		logf("%v", err)
		return exitFixture, nil
	}
	ca := *f.Station.CommonAddress
	if o.ca >= 0 {
		ca = o.ca
	}
	// server.Config: a common address of zero means the link address.
	if ca == 0 && o.linkAddr != 0 {
		return exitUsage, usagef("go-iec60870-5 takes common address 0 for the link address: common address 0 needs --link-address 0")
	}
	st := newStation(f, uint8(o.linkAddr), uint8(ca))

	ln, err := net.Listen("tcp", net.JoinHostPort(o.bind, strconv.Itoa(o.port)))
	if err != nil {
		logf("cannot listen on %s:%d: %v", o.bind, o.port, err)
		return exitOperationFailed, nil
	}
	if err := os.MkdirAll(filepath.Dir(o.readyFile), 0o755); err == nil {
		err = os.WriteFile(o.readyFile, []byte("ready\n"), 0o644)
	}
	if err != nil {
		_ = ln.Close()
		logf("cannot write the ready file %s: %v", o.readyFile, err)
		return exitOperationFailed, nil
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	emit(object{
		"event": "ready", "adapter": adapter, "address": net.JoinHostPort(o.bind, strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)), "fixture": f.Name,
		"commonAddress": ca, "standard": standard, "mode": "unbalanced", "linkAddress": o.linkAddr,
	})

	done := make(chan struct{})
	go func() { defer close(done); st.serve(ln) }()
	select {
	case <-sig:
	case <-done:
	}
	_ = os.Remove(o.readyFile)
	_ = ln.Close()
	st.stop()
	<-done
	emit(object{"event": "stopped"})
	return exitOK, nil
}

// station is the protection equipment of the fixture. Its process state
// (the DPI of the indications, the generic values) lives as long as the
// process; every connection gets a fresh server.Server, so the link state
// and the class 1 queue start afresh with each (docs/FIXTURES.md, "Station
// behaviour (IEC 103)").
type station struct {
	f        *fixture
	linkAddr uint8
	ca       uint8
	fun      iec103.FunctionType

	mu      sync.Mutex
	dpi     map[funInf]iec103.DPI
	generic map[iec103.GIN]iec103.Value

	connMu sync.Mutex
	conn   net.Conn      // the line being served, nil when none
	free   chan struct{} // closed when conn ends
	closed bool
}

func newStation(f *fixture, linkAddr, ca uint8) *station {
	st := &station{
		f: f, linkAddr: linkAddr, ca: ca, fun: iec103.FunctionType(f.Station.Identification.Fun),
		dpi: map[funInf]iec103.DPI{}, generic: map[iec103.GIN]iec103.Value{},
	}
	for _, i := range f.Indications {
		st.dpi[funInf{i.Fun, i.Inf}] = iec103.DPI(i.DPI)
	}
	for _, g := range f.Generic {
		st.generic[g.gin()] = g.value
	}
	return st
}

func (st *station) config(onFrame func(bool, ft12.Frame)) server.Config {
	id := st.f.Station.Identification
	cfg := server.Config{
		Link:           link.Config{Timeout: 500 * time.Millisecond, OnFrame: onFrame},
		LinkAddr:       st.linkAddr,
		CommonAddr:     st.ca,
		Fun:            st.fun,
		Identification: iec103.IdentificationOf(uint8(id.Col), id.ASCII, st.f.software),
		Handler:        st,
		SyncDelay:      -1, // TCP, not a serial line: no transmission delay to add
		OnTimeSync: func(t time.Time, broadcast bool) {
			emit(object{"event": "clock-sync", "time": t.UTC().Format(timeLayout), "broadcast": broadcast})
		},
	}
	if len(st.f.Disturbances) > 0 {
		cfg.Disturbances = st
	}
	return cfg
}

// handover is how long a new connection waits for the line before it is
// closed as a second control system: the end of the previous connection
// may not have been read yet when the next one is accepted.
const handover = 300 * time.Millisecond

// serve accepts one connection at a time: a line has one controlling
// system, so another connection is closed.
func (st *station) serve(ln net.Listener) {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !st.claim(conn) {
				_ = conn.Close()
				return
			}
			st.line(conn, func() {
				st.connMu.Lock()
				st.conn = nil
				close(st.free)
				st.connMu.Unlock()
			})
		}()
	}
}

// claim makes conn the line being served, waiting up to handover for the
// previous one to end.
func (st *station) claim(conn net.Conn) bool {
	deadline := time.NewTimer(handover)
	defer deadline.Stop()
	for {
		st.connMu.Lock()
		if st.closed {
			st.connMu.Unlock()
			return false
		}
		if st.conn == nil {
			st.conn = conn
			st.free = make(chan struct{})
			st.connMu.Unlock()
			return true
		}
		free := st.free
		st.connMu.Unlock()
		select {
		case <-free:
		case <-deadline.C:
			return false
		}
	}
}

// stop closes the line being served.
func (st *station) stop() {
	st.connMu.Lock()
	st.closed = true
	if st.conn != nil {
		_ = st.conn.Close()
	}
	st.connMu.Unlock()
}

// drainGrace is how long a server keeps handling after its line closed.
const drainGrace = 100 * time.Millisecond

// line serves one connection until it closes, and calls release when the
// next connection may be served.
func (st *station) line(conn net.Conn, release func()) {
	peer := conn.RemoteAddr().String()
	emit(object{"event": "connection-opened", "peer": peer})
	defer emit(object{"event": "connection-closed", "peer": peer})
	w := &watched{Conn: conn, closed: make(chan struct{})}
	s, err := server.New(w, st.config(st.frame))
	if err != nil {
		logf("server: %v", err)
		_ = conn.Close()
		release()
		return
	}
	stop := make(chan struct{})
	fed := make(chan struct{})
	go func() { defer close(fed); st.feed(s, stop) }()
	<-w.closed
	release()
	// What arrived before the line closed is still handled: a broadcast
	// (SEND/NO REPLY) from a control system that closes right after it
	// has no confirmation to wait for. The server hands received ASDUs to
	// its handler goroutine and has no call to wait for that queue.
	time.Sleep(drainGrace)
	close(stop)
	<-fed
	_ = s.Close()
}

// frame reports the general commands the server refuses before they reach
// the handler (clause 7.2.6.4 and table 17: DCO 0 or 3, OFF where only ON
// is defined): the contract has a command event for every ASDU 20.
func (st *station) frame(tx bool, f ft12.Frame) {
	if tx || f.Kind != ft12.KindVariable || f.Addr != uint16(st.linkAddr) || len(f.Data) < 8 || iec103.TypeID(f.Data[0]) != iec103.TypeGeneralCommand {
		return
	}
	a, err := iec103.Decode(f.Data)
	if err != nil {
		return
	}
	b, ok := a.Body.(iec103.GeneralCommand)
	if !ok || (a.CommonAddr != st.ca && a.CommonAddr != iec103.CommonAddrGlobal) || iec103.CommandAllowed(a.Fun, a.Inf, b.DCO) {
		return
	}
	st.commandEvent(a, f.Data[6], b.RII, false)
}

func (st *station) commandEvent(a *iec103.ASDU, dco, rii uint8, executed bool) {
	outcome := "rejected"
	if executed {
		outcome = "executed"
	}
	emit(object{"event": "command", "fun": int(a.Fun), "inf": int(a.Inf), "dco": int(dco), "rii": int(rii), "outcome": outcome})
}

// HandleASDU is the station's part of the control direction (server.Handler).
func (st *station) HandleASDU(s *server.Server, a *iec103.ASDU) {
	st.mu.Lock()
	defer st.mu.Unlock()
	switch b := a.Body.(type) {
	case iec103.GIInit:
		// ASDU 1, cause 9, SIN the scan number (set by AnswerGI), for every
		// indication in the interrogation, in the fixture's order, then ASDU
		// 8 (and the list of recorded disturbances, by the server).
		emit(object{"event": "interrogation", "commonAddress": int(a.CommonAddr), "scn": int(b.SCN), "accepted": true})
		var data []*iec103.ASDU
		for _, i := range st.f.Indications {
			if !i.inGI() {
				continue
			}
			data = append(data, iec103.New(iec103.CauseGI, st.ca, iec103.FunctionType(i.Fun), iec103.InfoNumber(i.Inf),
				iec103.TimeTagged{DPI: st.dpi[funInf{i.Fun, i.Inf}], Time: s.Now()}))
		}
		if err := s.AnswerGI(a, data...); err != nil {
			logf("general interrogation: %v", err)
		}
	case iec103.GeneralCommand:
		// The server refused DCO 0 and 3 and OFF where table 17 defines
		// only ON. A command of the fixture sets the indication with the
		// same FUN and INF: acknowledged with cause 20, then the change of
		// state with cause 12 when the DPI changed (clause 7.4.4, figure
		// 7). Anything else is acknowledged with cause 21.
		k := funInf{int(a.Fun), int(a.Inf)}
		if !slices.Contains(st.f.Commands, k) {
			st.commandEvent(a, uint8(b.DCO), b.RII, false)
			_ = s.AckCommand(a, false)
			return
		}
		changed := st.dpi[k] != b.DCO
		st.dpi[k] = b.DCO
		st.commandEvent(a, uint8(b.DCO), b.RII, true)
		_ = s.AckCommand(a, true)
		if changed {
			_ = s.Event(iec103.CauseRemoteOperation, a.Fun, a.Inf, b.DCO)
		}
	case iec103.GenericCommand:
		st.genericRead(s, a, b)
	case iec103.GenericData:
		st.genericWrite(s, a, b)
	}
}

// genericRead answers a read of a single entry (ASDU 21, INF 244, clause
// 7.4.8.6.2): KOD 1 the actual value, KOD 10 the description, with cause
// 42; an entry the device lacks or another KOD with cause 43 and the GIN,
// the KOD and a generic reply code (1: GIN not defined, 2: data do not
// exist). The device answers no other generic command.
func (st *station) genericRead(s *server.Server, a *iec103.ASDU, b iec103.GenericCommand) {
	if a.Inf != iec103.InfReadEntry || a.Cause != iec103.CauseGenericRead {
		return
	}
	var sets, failed []iec103.DataSet
	for _, it := range b.Items {
		v, ok := st.generic[it.GIN]
		switch {
		case !ok:
			failed = append(failed, iec103.DataSet{GIN: it.GIN, KOD: it.KOD, Value: iec103.Reply(iec103.GRCInvalidGIN)})
		case it.KOD == iec103.KODActualValue:
			sets = append(sets, iec103.DataSet{GIN: it.GIN, KOD: it.KOD, Value: v})
		case it.KOD == iec103.KODDescription:
			sets = append(sets, iec103.DataSet{GIN: it.GIN, KOD: it.KOD, Value: iec103.ASCII(st.description(it.GIN))})
		default:
			failed = append(failed, iec103.DataSet{GIN: it.GIN, KOD: it.KOD, Value: iec103.Reply(iec103.GRCNotExist)})
		}
	}
	var err error
	if failed != nil {
		err = s.GenericAnswer(a, iec103.CauseGenericReadInvalid, failed...)
	} else {
		err = s.GenericAnswer(a, iec103.CauseGenericReadValid, sets...)
	}
	if err != nil {
		logf("generic read: %v", err)
	}
}

func (st *station) description(gin iec103.GIN) string {
	for _, g := range st.f.Generic {
		if g.gin() == gin {
			return g.Description
		}
	}
	return ""
}

// genericWrite answers a write with execution (ASDU 10, cause 40, INF 250,
// clause 7.4.8.6.3, figure 17): when every data set is KOD 1 to a writable
// entry with its data type and size and NUMBER 1, the values are set and
// cause 40 follows with no data sets; otherwise nothing is set and cause 41
// follows with the GIN, KOD and a reply code of each failed set (1: GIN not
// defined, 8: read only, 5: another type, size or KOD).
func (st *station) genericWrite(s *server.Server, a *iec103.ASDU, b iec103.GenericData) {
	if a.Inf != iec103.InfWriteExecute || a.Cause != iec103.CauseGenericWrite {
		return
	}
	var failed []iec103.DataSet
	for _, d := range b.Sets {
		grc := iec103.GRCAck
		var entry *genericEntry
		for i := range st.f.Generic {
			if st.f.Generic[i].gin() == d.GIN {
				entry = &st.f.Generic[i]
			}
		}
		switch {
		case entry == nil:
			grc = iec103.GRCInvalidGIN
		case !entry.Writable:
			grc = iec103.GRCReadOnly
		case d.KOD != iec103.KODActualValue || int(d.Value.Type) != entry.Type || int(d.Value.Size) != entry.Size || d.Value.Number != 1:
			grc = iec103.GRCOutOfRange
		}
		if grc != iec103.GRCAck {
			failed = append(failed, iec103.DataSet{GIN: d.GIN, KOD: d.KOD, Value: iec103.Reply(grc)})
		}
	}
	if failed != nil {
		if err := s.GenericAnswer(a, iec103.CauseGenericWriteNack, failed...); err != nil {
			logf("generic write: %v", err)
		}
		return
	}
	for _, d := range b.Sets {
		st.generic[d.GIN] = d.Value
	}
	if err := s.GenericAnswer(a, iec103.CauseGenericWriteAck); err != nil {
		logf("generic write: %v", err)
	}
}

// List, Record and Transmitted serve the fixture's recorded disturbances
// (server.DisturbanceSource); a record stays after its transfer.
func (st *station) List() []iec103.RecordedFault {
	var l []iec103.RecordedFault
	for i := range st.f.Disturbances {
		d := &st.f.Disturbances[i]
		l = append(l, iec103.RecordedFault{FAN: uint16(d.FAN), SOF: d.sof(), Time: cp.At(d.time)})
	}
	return l
}

// Record returns the record of fault fan.
func (st *station) Record(fan uint16) (*iec103.Disturbance, bool) {
	for i := range st.f.Disturbances {
		if d := &st.f.Disturbances[i]; d.FAN == int(fan) {
			return d.record(st.fun), true
		}
	}
	return nil, false
}

// Transmitted keeps the record: the fixture's records stay.
func (st *station) Transmitted(uint16, bool) {}

// feed keeps the next measurand of the fixture queued as class 2 data, one
// after the other, cyclically (cause 2).
func (st *station) feed(s *server.Server, stop <-chan struct{}) {
	if len(st.f.Measurands) == 0 {
		return
	}
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for i := 0; ; {
		if s.Pending(server.Class2) == 0 {
			m := st.f.Measurands[i%len(st.f.Measurands)]
			var vals []iec103.MEA
			for _, v := range m.Values {
				vals = append(vals, iec103.MEA{Raw: int16(v.Value), Overflow: v.OV, Error: v.ER})
			}
			a := iec103.New(iec103.CauseCyclic, st.ca, iec103.FunctionType(m.Fun), iec103.InfoNumber(m.Inf),
				iec103.Measurands{Set2: m.Type == 9, Values: vals})
			if s.Send(a, server.Class2) == nil {
				i++
			}
		}
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

// watched reports the end of a connection: the first failed read.
type watched struct {
	net.Conn
	once   sync.Once
	closed chan struct{}
}

func (w *watched) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if err != nil {
		w.once.Do(func() { close(w.closed) })
	}
	return n, err
}

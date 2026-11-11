// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	iec "github.com/cB-Sigma/iec60870"
	"github.com/cB-Sigma/iec60870/cs101"
)

// The controlling station of the contract: one bounded operation per
// invocation, on cB-Sigma's cs101.Link over the TCP byte stream. The
// operation sends its ASDUs itself and reads every answer, rather than
// cs101.Client's procedures, which answer for themselves what the contract
// wants to see.

// failure is a failed operation: it becomes the error object of the result.
type failure struct{ code, message string }

func (f *failure) Error() string { return f.message }

func failf(code, format string, a ...any) *failure {
	return &failure{code, strings.TrimSpace(sprintf(format, a...))}
}

// session is what the receive side shares with the operation.
type session struct {
	mu            sync.Mutex
	changed       chan struct{} // closed and replaced on every change
	asdus         []object
	confirmations []object
	closed        bool
	terminated    bool
	negativeCot   int
	readAnswered  bool
	expectType    iec.TypeID
	expectRead    bool
	expectIOA     int
}

func (s *session) update(f func()) {
	s.mu.Lock()
	f()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

// waitFor waits until cond holds, the line fails or the timeout passes.
func (s *session) waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		ok, closed, changed := cond(), s.closed, s.changed
		s.mu.Unlock()
		if ok || closed {
			return ok
		}
		select {
		case <-changed:
		case <-deadline.C:
			s.mu.Lock()
			defer s.mu.Unlock()
			return cond()
		}
	}
}

func (s *session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *session) onASDU(raw []byte) {
	if len(raw) < params.Header() {
		return
	}
	a, decodeErr := iec.DecodeASDU(raw, params, time.UTC)
	doc := asduJSON(raw, a, decodeErr)
	cot := doc["cot"].(int)
	typ := iec.TypeID(raw[0])
	s.update(func() {
		s.asdus = append(s.asdus, doc)
		switch {
		case s.expectType != 0 && typ == s.expectType:
			unknown := cot >= 44 && cot <= 47
			if cot == 7 || cot == 9 || unknown {
				negative := doc["negative"].(bool) || unknown
				s.confirmations = append(s.confirmations, object{"cot": cot, "negative": negative})
				if negative {
					s.negativeCot = cot
				}
			} else if cot == 10 {
				s.terminated = true
			}
		case s.expectRead && cot == 5:
			for _, o := range doc["objects"].([]object) {
				if o["ioa"] == s.expectIOA {
					s.readAnswered = true
				}
			}
		}
	})
}

func (s *session) awaitConfirmation(n int, timeout time.Duration) error {
	if !s.waitFor(func() bool { return len(s.confirmations) >= n }, timeout) {
		if s.isClosed() {
			return failf("connection-lost", "connection closed while waiting for confirmation %d", n)
		}
		return failf("timeout", "no confirmation %d from the station", n)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.negativeCot != 0 {
		return failf("negative-confirmation", "the station refused the request (cause %d)", s.negativeCot)
	}
	return nil
}

func (s *session) awaitTermination(timeout time.Duration) error {
	if !s.waitFor(func() bool { return s.terminated || s.negativeCot != 0 }, timeout) {
		if s.isClosed() {
			return failf("connection-lost", "connection closed while waiting for termination")
		}
		return failf("timeout", "no activation termination from the station")
	}
	return nil
}

// processCommand is a process command of the contract, parsed once and sent
// with either state of the select bit.
type processCommand struct {
	typ       iec.TypeID
	ioa       int
	qualifier int
	value     float64
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "true", "1", "on":
		return true, true
	case "false", "0", "off":
		return false, true
	}
	return false, false
}

func parseCommand(a *args) (*processCommand, error) {
	typeArg := a.str("--type", "")
	value := a.str("--value", "")
	ioa, err := a.num("--ioa", 0)
	if err != nil {
		return nil, err
	}
	qualifier, err := a.num("--qualifier", 0)
	if err != nil {
		return nil, err
	}
	typ, known := commandTypes[typeArg]
	if !known || ioa < 1 || value == "" {
		return nil, usagef("command: --type (C_SC_NA_1, C_DC_NA_1, C_RC_NA_1, C_SE_NA_1, C_SE_NB_1, " +
			"C_SE_NC_1), --ioa and --value are required")
	}
	c := &processCommand{typ: typ, ioa: ioa, qualifier: qualifier}
	ok := true
	switch typ {
	case iec.CScNa1:
		var b bool
		if b, ok = parseBool(value); b {
			c.value = 1
		}
	case iec.CDcNa1, iec.CRcNa1:
		n, err := strconv.Atoi(value)
		ok, c.value = err == nil && n >= 0 && n <= 3, float64(n)
	case iec.CSeNa1:
		n, err := strconv.Atoi(value)
		ok, c.value = err == nil && n >= -32768 && n <= 32767, float64(n)/32768
	case iec.CSeNb1:
		n, err := strconv.Atoi(value)
		ok, c.value = err == nil && n >= -32768 && n <= 32767, float64(n)
	default:
		f, err := strconv.ParseFloat(value, 32)
		ok, c.value = err == nil, float64(float32(f))
	}
	if !ok {
		return nil, usagef("command: --value '%s' is not valid for %s", value, typeArg)
	}
	return c, nil
}

func parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(timeLayout, s)
	return t, err == nil
}

func runClient(op string, a *args) (int, error) {
	host := a.str("--host", "")
	port, err := a.num("--port", defaultPort)
	if err != nil {
		return 0, err
	}
	oa, err := a.num("--originator-address", 0)
	if err != nil {
		return 0, err
	}
	timeoutMs, err := a.num("--timeout-ms", 5000)
	if err != nil {
		return 0, err
	}
	connectMs, err := a.num("--connect-timeout-ms", 5000)
	if err != nil {
		return 0, err
	}
	collectMs, err := a.num("--collect-ms", 0)
	if err != nil {
		return 0, err
	}
	lc, err := a.link()
	if err != nil {
		return 0, err
	}
	ca := lc.commonAddr
	if ca == 0 {
		ca = 1
	}
	if oa < 0 || oa > 255 {
		return 0, usagef("--originator-address: must be 0..255")
	}

	// Operation flags, parsed before connecting so that a usage error never
	// touches the network.
	var (
		qoi, qcc, ioa, holdMs, durationMs, maxASDU int
		clock                                      = time.Now()
		cmd                                        *processCommand
		mode                                       = "direct"
	)
	switch op {
	case "connect":
		holdMs, err = a.num("--hold-ms", 0)
	case "interrogate":
		qoi, err = a.num("--qoi", 20)
	case "counter-interrogate":
		qcc, err = a.num("--qcc", 5)
	case "read":
		if ioa, err = a.num("--ioa", 0); err == nil && ioa < 1 {
			err = usagef("read: --ioa is required")
		}
	case "clock-sync":
		if t := a.str("--time", ""); t != "" {
			var ok bool
			if clock, ok = parseTime(t); !ok {
				err = usagef("clock-sync: --time must look like 2026-01-02T03:04:05.678Z")
			}
		}
	case "test-command":
	case "command":
		mode = a.str("--mode", "direct")
		if mode != "direct" && mode != "select" && mode != "sbo" && mode != "cancel" {
			return 0, usagef("command: --mode must be direct, select, sbo or cancel")
		}
		cmd, err = parseCommand(a)
	case "monitor":
		if durationMs, err = a.num("--duration-ms", 1000); err == nil {
			maxASDU, err = a.num("--max-asdus", 0)
		}
	default:
		return 0, usagef("client: unknown operation %s", op)
	}
	if err != nil {
		return 0, err
	}
	if host == "" {
		return 0, usagef("client: --host is required")
	}
	if err := a.finish("client " + op); err != nil {
		return 0, err
	}
	if ioa >= 1<<(8*lc.ioaSize) || cmd != nil && cmd.ioa >= 1<<(8*lc.ioaSize) {
		return 0, usagef("--ioa: does not fit an information object address of %d octets", lc.ioaSize)
	}
	params = lc.params(oa)

	timeout := time.Duration(timeoutMs) * time.Millisecond
	s := &session{changed: make(chan struct{})}
	begin := time.Now()
	connected := false
	exitCode := exitOperationFailed
	addr := uint16(lc.address)

	var link *cs101.Link
	stop := make(chan struct{})
	var wg sync.WaitGroup

	operation := func() error {
		nc, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Duration(connectMs)*time.Millisecond)
		if err != nil {
			exitCode = exitConnectFailed
			return failf("connect-failed", "%v", err)
		}
		// The answer timeout of a request: the interval of 6.2.2 is far
		// shorter on a TCP stream than on 1200 bit/s; the retries cover a
		// slow peer.
		link = cs101.NewLink(cs101.NewPort(nc), lc.addrSize, lc.balanced, 500*time.Millisecond, 3)
		go func() {
			<-link.Done()
			s.update(func() { s.closed = true })
		}()

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err = link.Start(ctx, addr)
		cancel()
		if err != nil {
			exitCode = exitConnectFailed
			if s.isClosed() {
				return failf("connection-lost", "the line closed during the link start-up: %v", err)
			}
			return failf("link-init-timeout", "%v", err)
		}
		connected = true

		// The receive side: polling on an unbalanced line, the station's
		// own user data on a balanced one.
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lc.balanced {
				for {
					select {
					case <-stop:
						return
					case <-link.Done():
						return
					case b := <-link.ASDUs():
						s.onASDU(b)
					}
				}
			}
			t := time.NewTicker(50 * time.Millisecond)
			defer t.Stop()
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				err := link.Poll(ctx, addr, 16, s.onASDU)
				cancel()
				if err != nil && !s.isClosed() {
					logf("poll: %v", err)
				}
				select {
				case <-stop:
					return
				case <-link.Done():
					return
				case <-t.C:
				}
			}
		}()

		send := func(t iec.TypeID, c iec.Cause, o iec.Object) error {
			asdu := &iec.ASDU{Type: t, Cause: c, Orig: uint8(oa), CA: uint16(ca), Objects: []iec.Object{o}}
			raw, err := asdu.Encode(params, time.UTC)
			if err != nil {
				return failf("failed", "cannot encode %s: %v", t, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if err := link.Send(ctx, addr, raw); err != nil {
				if s.isClosed() {
					return failf("connection-lost", "%v", err)
				}
				return failf("timeout", "the station's link did not take the request: %v", err)
			}
			return nil
		}
		expect := func(t iec.TypeID) { s.update(func() { s.expectType = t }) }

		var opErr error
		switch op {
		case "connect":
			if holdMs > 0 {
				s.waitFor(func() bool { return false }, time.Duration(holdMs)*time.Millisecond)
			}
			if s.isClosed() {
				opErr = failf("connection-lost", "the line closed while it was held")
			}
		case "interrogate":
			expect(iec.CIcNa1)
			if opErr = send(iec.CIcNa1, iec.CauseActivation, iec.Object{Value: float64(qoi)}); opErr == nil {
				if opErr = s.awaitConfirmation(1, timeout); opErr == nil {
					opErr = s.awaitTermination(timeout)
				}
			}
		case "counter-interrogate":
			expect(iec.CCiNa1)
			if opErr = send(iec.CCiNa1, iec.CauseActivation, iec.Object{Value: float64(qcc)}); opErr == nil {
				if opErr = s.awaitConfirmation(1, timeout); opErr == nil {
					opErr = s.awaitTermination(timeout)
				}
			}
		case "read":
			s.update(func() { s.expectType, s.expectRead, s.expectIOA = iec.CRdNa1, true, ioa })
			if opErr = send(iec.CRdNa1, iec.CauseRequest, iec.Object{IOA: uint32(ioa)}); opErr == nil {
				if !s.waitFor(func() bool { return s.readAnswered || s.negativeCot != 0 }, timeout) {
					opErr = failf("timeout", "no answer to the read command")
					if s.isClosed() {
						opErr = failf("connection-lost", "connection closed while waiting for the answer")
					}
				} else if s.negativeCot != 0 {
					opErr = failf("negative-confirmation", "the station refused the request (cause %d)", s.negativeCot)
				}
			}
		case "clock-sync":
			expect(iec.CCsNa1)
			if opErr = send(iec.CCsNa1, iec.CauseActivation, iec.Object{Time: clock}); opErr == nil {
				opErr = s.awaitConfirmation(1, timeout)
			}
		case "test-command":
			expect(iec.CTsNa1)
			// FBP: the fixed test bit pattern 55AA (7.2.6.14).
			if opErr = send(iec.CTsNa1, iec.CauseActivation, iec.Object{Value: 0x55AA}); opErr == nil {
				opErr = s.awaitConfirmation(1, timeout)
			}
		case "command":
			expect(cmd.typ)
			n := 0
			o := iec.Object{IOA: uint32(cmd.ioa), Value: cmd.value, Qualifier: uint8(cmd.qualifier)}
			if mode != "direct" {
				sel := o
				sel.Select = true
				if opErr = send(cmd.typ, iec.CauseActivation, sel); opErr == nil {
					n++
					opErr = s.awaitConfirmation(n, timeout)
				}
				if opErr == nil && mode == "cancel" {
					if opErr = send(cmd.typ, iec.CauseDeactivation, sel); opErr == nil {
						n++
						opErr = s.awaitConfirmation(n, timeout)
					}
				}
			}
			if opErr == nil && (mode == "direct" || mode == "sbo") {
				if opErr = send(cmd.typ, iec.CauseActivation, o); opErr == nil {
					n++
					if opErr = s.awaitConfirmation(n, timeout); opErr == nil {
						opErr = s.awaitTermination(timeout)
					}
				}
			}
		default: // monitor
			s.waitFor(func() bool { return maxASDU > 0 && len(s.asdus) >= maxASDU }, time.Duration(durationMs)*time.Millisecond)
			if s.isClosed() {
				opErr = failf("connection-lost", "connection closed while monitoring")
			}
		}
		if opErr != nil {
			return opErr
		}
		if collectMs > 0 {
			s.waitFor(func() bool { return false }, time.Duration(collectMs)*time.Millisecond)
		}
		exitCode = exitOK
		return nil
	}

	err = operation()
	close(stop)
	if link != nil {
		link.Close()
	}
	wg.Wait()

	r := object{
		"schemaVersion": schemaVersion,
		"adapter":       adapter,
		"operation":     op,
		"ok":            err == nil,
		"error":         nil,
	}
	if err != nil {
		var f *failure
		if !errors.As(err, &f) {
			f = &failure{"failed", err.Error()}
		}
		r["error"] = object{"code": f.code, "message": f.message}
	}
	s.mu.Lock()
	r["connected"] = connected
	r["confirmations"] = append([]object{}, s.confirmations...)
	r["terminated"] = s.terminated
	r["asdus"] = append([]object{}, s.asdus...)
	s.mu.Unlock()
	r["elapsedMs"] = time.Since(begin).Milliseconds()
	emit(r)
	return exitCode, nil
}

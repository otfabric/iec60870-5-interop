// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101"
	"github.com/otfabric/go-iec60870-5/iec101/client"
	"github.com/otfabric/go-iec60870-5/link"
)

// The controlling station of the contract: one bounded operation per
// invocation, on go-iec60870-5's iec101/client over the TCP byte stream.
// The operation sends its request with Client.Send and observes every ASDU
// the station sends (Config.Handler), rather than the client's request
// procedures (Interrogate, Command, ...), which consume the mirrors of the
// request that the result document lists.

// failure is a failed operation: it becomes the error object of the result.
type failure struct{ code, message string }

func (f *failure) Error() string { return f.message }

func failf(code, format string, a ...any) *failure {
	return &failure{code, strings.TrimSpace(fmt.Sprintf(format, a...))}
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
	expectType    asdu.TypeID
	expectRead    bool
	expectIOA     int
}

func newSession() *session { return &session{changed: make(chan struct{})} }

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

// confirms reports whether a cause makes a mirror of the request a
// confirmation (CONTAINER_CONTRACT.md, result document), and whether it is
// negative whatever the P/N bit says.
func confirms(cot int) (confirmation, negative bool) {
	switch {
	case cot == 7 || cot == 9:
		return true, false
	case cot == 17 || cot >= 44 && cot <= 47:
		return true, true
	}
	return false, false
}

// onASDU is the client's handler: every ASDU the station sends.
func (s *session) onASDU(a *asdu.ASDU) {
	doc := asduJSON(a)
	cot := int(a.Cause)
	s.update(func() {
		s.asdus = append(s.asdus, doc)
		switch {
		case s.expectType != 0 && a.Type == s.expectType:
			if conf, neg := confirms(cot); conf {
				negative := a.Negative || neg
				s.confirmations = append(s.confirmations, object{"cot": cot, "negative": negative})
				if negative {
					s.negativeCot = cot
				}
			} else if cot == 10 {
				s.terminated = true
			}
		case s.expectRead && cot == 5:
			for _, o := range a.Objects {
				if int(o.Address()) == s.expectIOA {
					s.readAnswered = true
				}
			}
		}
	})
}

func (s *session) awaitConfirmation(n int, timeout time.Duration) error {
	if !s.waitFor(func() bool { return len(s.confirmations) >= n || s.negativeCot != 0 }, timeout) {
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
	typ       asdu.TypeID
	ioa       int
	qualifier int
	value     float64
	on        bool
}

// object is the information object of the command.
func (c *processCommand) object(sel bool) asdu.InformationObject {
	ioa := asdu.IOA(c.ioa)
	switch c.typ {
	case asdu.C_SC_NA_1:
		return asdu.SingleCommand{IOA: ioa, Value: c.on, Qualifier: asdu.CommandQualifier(c.qualifier), Select: sel}
	case asdu.C_DC_NA_1:
		return asdu.DoubleCommand{IOA: ioa, Value: asdu.DoubleState(c.value), Qualifier: asdu.CommandQualifier(c.qualifier), Select: sel}
	case asdu.C_RC_NA_1:
		return asdu.StepCommand{IOA: ioa, Value: asdu.StepDirection(c.value), Qualifier: asdu.CommandQualifier(c.qualifier), Select: sel}
	case asdu.C_SE_NA_1:
		return asdu.SetpointNormalized{IOA: ioa, Value: asdu.Normalized(int16(c.value)), Qualifier: uint8(c.qualifier), Select: sel}
	case asdu.C_SE_NB_1:
		return asdu.SetpointScaled{IOA: ioa, Value: int16(c.value), Qualifier: uint8(c.qualifier), Select: sel}
	default:
		return asdu.SetpointFloat{IOA: ioa, Value: float32(c.value), Qualifier: uint8(c.qualifier), Select: sel}
	}
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
	maxQualifier := 127 // QL of a set point
	ok := true
	switch typ {
	case asdu.C_SC_NA_1:
		c.on, ok = parseBool(value)
		maxQualifier = 31 // QU
	case asdu.C_DC_NA_1, asdu.C_RC_NA_1:
		n, err := strconv.Atoi(value)
		ok, c.value = err == nil && n >= 0 && n <= 3, float64(n)
		maxQualifier = 31
	case asdu.C_SE_NA_1, asdu.C_SE_NB_1:
		n, err := strconv.Atoi(value)
		ok, c.value = err == nil && n >= -32768 && n <= 32767, float64(n)
	default:
		f, err := strconv.ParseFloat(value, 32)
		ok, c.value = err == nil, float64(float32(f))
	}
	if !ok {
		return nil, usagef("command: --value '%s' is not valid for %s", value, typeArg)
	}
	if qualifier < 0 || qualifier > maxQualifier {
		return nil, usagef("command: --qualifier must be 0..%d for %s", maxQualifier, typeArg)
	}
	return c, nil
}

// clientFlags are the parsed flags of a client operation.
type clientFlags struct {
	op                               string
	host                             string
	port, oa, ca                     int
	timeout, connectTimeout, collect time.Duration
	lc                               linkConfig
	qoi, qcc, ioa                    int
	hold, duration                   time.Duration
	maxASDUs                         int
	clock                            time.Time
	cmd                              *processCommand
	mode                             string
}

// parseClient reads the flags of a client operation; a usage error never
// touches the network.
func parseClient(op string, a *args) (*clientFlags, error) {
	f := &clientFlags{op: op, clock: time.Now(), mode: "direct"}
	f.host = a.str("--host", "")
	var err error
	ms := func(name string, def int, dst *time.Duration) {
		if err != nil {
			return
		}
		var n int
		if n, err = a.num(name, def); err == nil {
			if n < 0 {
				err = usagef("%s: must not be negative", name)
			}
			*dst = time.Duration(n) * time.Millisecond
		}
	}
	if f.port, err = a.num("--port", defaultPort); err != nil {
		return nil, err
	}
	if f.oa, err = a.num("--originator-address", 0); err != nil {
		return nil, err
	}
	ms("--timeout-ms", 5000, &f.timeout)
	ms("--connect-timeout-ms", 5000, &f.connectTimeout)
	ms("--collect-ms", 0, &f.collect)
	if err != nil {
		return nil, err
	}
	if f.lc, err = a.link(); err != nil {
		return nil, err
	}
	f.ca = f.lc.commonAddr
	if f.ca == 0 {
		f.ca = 1
	}
	if f.oa < 0 || f.oa > 255 {
		return nil, usagef("--originator-address: must be 0..255")
	}
	if f.port < 1 || f.port > 65535 {
		return nil, usagef("--port: %d is not a TCP port", f.port)
	}

	switch op {
	case "connect":
		ms("--hold-ms", 0, &f.hold)
	case "interrogate":
		if f.qoi, err = a.num("--qoi", 20); err == nil && (f.qoi < 0 || f.qoi > 255) {
			err = usagef("interrogate: --qoi must be 0..255")
		}
	case "counter-interrogate":
		if f.qcc, err = a.num("--qcc", 5); err == nil && (f.qcc < 0 || f.qcc > 255) {
			err = usagef("counter-interrogate: --qcc must be 0..255")
		}
	case "read":
		if f.ioa, err = a.num("--ioa", 0); err == nil && f.ioa < 1 {
			err = usagef("read: --ioa is required")
		}
	case "clock-sync":
		if t := a.str("--time", ""); t != "" {
			var ok bool
			if f.clock, ok = parseTime(t); !ok {
				err = usagef("clock-sync: --time must look like 2026-01-02T03:04:05.678Z")
			}
		}
	case "test-command":
	case "command":
		f.mode = a.str("--mode", "direct")
		if f.mode != "direct" && f.mode != "select" && f.mode != "sbo" && f.mode != "cancel" {
			return nil, usagef("command: --mode must be direct, select, sbo or cancel")
		}
		if f.cmd, err = parseCommand(a); err == nil {
			f.ioa = f.cmd.ioa
		}
	case "monitor":
		ms("--duration-ms", 1000, &f.duration)
		if err == nil {
			if f.maxASDUs, err = a.num("--max-asdus", 0); err == nil && f.maxASDUs < 0 {
				err = usagef("monitor: --max-asdus must not be negative")
			}
		}
	default:
		return nil, usagef("client: unknown operation %s", op)
	}
	if err != nil {
		return nil, err
	}
	if f.host == "" {
		return nil, usagef("client: --host is required")
	}
	if err := a.finish("client " + op); err != nil {
		return nil, err
	}
	if f.ioa > f.lc.maxIOA() {
		return nil, usagef("--ioa: %d does not fit an information object address of %d octets", f.ioa, f.lc.ioaSize)
	}
	return f, nil
}

func runClient(op string, a *args) (int, error) {
	f, err := parseClient(op, a)
	if err != nil {
		return 0, err
	}
	s := newSession()
	begin := time.Now()
	connected := false
	exitCode := exitOperationFailed

	var c *client.Client
	operation := func() error {
		nc, err := net.DialTimeout("tcp", net.JoinHostPort(f.host, strconv.Itoa(f.port)), f.connectTimeout)
		if err != nil {
			exitCode = exitConnectFailed
			return failf("connect-failed", "%v", err)
		}
		c, err = client.New(nc, client.Config{
			Params:   f.lc.params(),
			LinkAddr: uint16(f.lc.address),
			OwnAddr:  uint16(f.lc.remote),
			// Unbalanced: class 2 polls while the station has nothing.
			PollInterval: 50 * time.Millisecond,
			// A link start-up that failed is tried again soon: the whole
			// start-up is bounded by --timeout-ms.
			RetryInterval: 500 * time.Millisecond,
			Handler:       s.onASDU,
			Logger:        stderrLogger{},
		})
		if err != nil {
			_ = nc.Close()
			return failf("failed", "%v", err)
		}
		go func() {
			<-c.Done()
			s.update(func() { s.closed = true })
		}()

		ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
		err = c.WaitUp(ctx)
		cancel()
		if err != nil {
			exitCode = exitConnectFailed
			if s.isClosed() || c.Err() != nil {
				return failf("connection-lost", "the line closed during the link start-up: %v", err)
			}
			return failf("link-init-timeout", "%v", err)
		}
		connected = true

		send := func(t asdu.TypeID, cause asdu.Cause, o asdu.InformationObject) error {
			req := &asdu.ASDU{Type: t, Cause: cause, Originator: uint8(f.oa), CommonAddr: asdu.CommonAddr(f.ca),
				Objects: []asdu.InformationObject{o}}
			ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
			defer cancel()
			err := c.Send(ctx, req)
			switch {
			case err == nil:
				return nil
			case s.isClosed() || errors.Is(err, link.ErrClosed):
				return failf("connection-lost", "%v", err)
			case errors.Is(err, context.DeadlineExceeded), errors.Is(err, link.ErrTimeout),
				errors.Is(err, iec101.ErrLinkDown):
				return failf("timeout", "the station's link did not take the request: %v", err)
			}
			return failf("failed", "cannot send %s: %v", t, err)
		}
		expect := func(t asdu.TypeID) { s.update(func() { s.expectType = t }) }

		var opErr error
		switch f.op {
		case "connect":
			if f.hold > 0 {
				s.waitFor(func() bool { return false }, f.hold)
			}
			if s.isClosed() {
				opErr = failf("connection-lost", "the line closed while it was held")
			}
		case "interrogate":
			expect(asdu.C_IC_NA_1)
			if opErr = send(asdu.C_IC_NA_1, asdu.CauseActivation, asdu.Interrogation{Qualifier: asdu.QOI(f.qoi)}); opErr == nil {
				if opErr = s.awaitConfirmation(1, f.timeout); opErr == nil {
					opErr = s.awaitTermination(f.timeout)
				}
			}
		case "counter-interrogate":
			expect(asdu.C_CI_NA_1)
			qcc := asdu.CounterInterrogation{Request: uint8(f.qcc & 0x3f), Freeze: uint8(f.qcc >> 6)}
			if opErr = send(asdu.C_CI_NA_1, asdu.CauseActivation, qcc); opErr == nil {
				if opErr = s.awaitConfirmation(1, f.timeout); opErr == nil {
					opErr = s.awaitTermination(f.timeout)
				}
			}
		case "read":
			s.update(func() { s.expectType, s.expectRead, s.expectIOA = asdu.C_RD_NA_1, true, f.ioa })
			if opErr = send(asdu.C_RD_NA_1, asdu.CauseRequest, asdu.Read{IOA: asdu.IOA(f.ioa)}); opErr == nil {
				if !s.waitFor(func() bool { return s.readAnswered || s.negativeCot != 0 }, f.timeout) {
					opErr = failf("timeout", "no answer to the read command")
					if s.isClosed() {
						opErr = failf("connection-lost", "connection closed while waiting for the answer")
					}
				} else if s.negativeCot != 0 {
					opErr = failf("negative-confirmation", "the station refused the request (cause %d)", s.negativeCot)
				}
			}
		case "clock-sync":
			expect(asdu.C_CS_NA_1)
			if opErr = send(asdu.C_CS_NA_1, asdu.CauseActivation, asdu.ClockSync{Time: asdu.At(f.clock.UTC())}); opErr == nil {
				opErr = s.awaitConfirmation(1, f.timeout)
			}
		case "test-command":
			expect(asdu.C_TS_NA_1)
			// FBP: the fixed test bit pattern 55AA (7.2.6.14).
			if opErr = send(asdu.C_TS_NA_1, asdu.CauseActivation, asdu.TestCommand{Counter: asdu.TestPattern}); opErr == nil {
				opErr = s.awaitConfirmation(1, f.timeout)
			}
		case "command":
			opErr = runCommand(f, s, send, expect)
		default: // monitor
			s.waitFor(func() bool { return f.maxASDUs > 0 && len(s.asdus) >= f.maxASDUs }, f.duration)
			if s.isClosed() {
				opErr = failf("connection-lost", "connection closed while monitoring")
			}
		}
		if opErr != nil {
			return opErr
		}
		if f.collect > 0 {
			s.waitFor(func() bool { return false }, f.collect)
		}
		exitCode = exitOK
		return nil
	}

	err = operation()
	if c != nil {
		_ = c.Close()
	}

	r := object{
		"schemaVersion": schemaVersion,
		"adapter":       adapter,
		"operation":     op,
		"ok":            err == nil,
		"error":         nil,
	}
	if err != nil {
		var fl *failure
		if !errors.As(err, &fl) {
			fl = &failure{"failed", err.Error()}
		}
		r["error"] = object{"code": fl.code, "message": fl.message}
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

// runCommand runs the sequence of a command mode (CONTAINER_CONTRACT.md,
// command).
func runCommand(f *clientFlags, s *session, send func(asdu.TypeID, asdu.Cause, asdu.InformationObject) error,
	expect func(asdu.TypeID)) error {
	cmd := f.cmd
	expect(cmd.typ)
	n := 0
	if f.mode != "direct" {
		if err := send(cmd.typ, asdu.CauseActivation, cmd.object(true)); err != nil {
			return err
		}
		n++
		if err := s.awaitConfirmation(n, f.timeout); err != nil {
			return err
		}
		if f.mode == "cancel" {
			if err := send(cmd.typ, asdu.CauseDeactivation, cmd.object(true)); err != nil {
				return err
			}
			n++
			return s.awaitConfirmation(n, f.timeout)
		}
		if f.mode == "select" {
			return nil
		}
	}
	if err := send(cmd.typ, asdu.CauseActivation, cmd.object(false)); err != nil {
		return err
	}
	n++
	if err := s.awaitConfirmation(n, f.timeout); err != nil {
		return err
	}
	return s.awaitTermination(f.timeout)
}

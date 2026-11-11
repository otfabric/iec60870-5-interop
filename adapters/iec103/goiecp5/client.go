// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/riclolsen/go-iecp5/cs103"
)

// stderrLog sends cs103's log to stderr.
type stderrLog struct{}

func (stderrLog) Critical(format string, v ...any) { logf("C: "+format, v...) }
func (stderrLog) Error(format string, v ...any)    { logf("E: "+format, v...) }
func (stderrLog) Warn(format string, v ...any)     { logf("W: "+format, v...) }
func (stderrLog) Debug(format string, v ...any)    { logf("D: "+format, v...) }

// handler hands every ASDU cs103 decoded to the operation, in order.
type handler struct{ ch chan *cs103.ASDU }

func (h *handler) ASDUHandlerAll(a *cs103.ASDU) error {
	h.ch <- a
	return nil
}
func (h *handler) TimeTaggedHandler(*cs103.ASDU, cs103.TimeTaggedInfo) error         { return nil }
func (h *handler) MeasurandsHandler(*cs103.ASDU, cs103.MeasurandsInfo) error         { return nil }
func (h *handler) IdentificationHandler(*cs103.ASDU, cs103.IdentificationInfo) error { return nil }
func (h *handler) GITerminationHandler(*cs103.ASDU, byte) error                      { return nil }
func (h *handler) ASDUHandler(*cs103.ASDU) error                                     { return nil }

type result struct {
	op            string
	ok            bool
	errCode       string
	errMsg        string
	connected     bool
	confirmations []object
	terminated    bool
	asdus         []object
	start         time.Time
}

func (r *result) fail(code, msg string) { r.ok, r.errCode, r.errMsg = false, code, msg }

func (r *result) emit() {
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
	emit(object{
		"schemaVersion": schemaVersion, "adapter": adapter, "operation": r.op,
		"ok": r.ok, "error": e, "connected": r.connected,
		"confirmations": r.confirmations, "terminated": r.terminated,
		"asdus": r.asdus, "elapsedMs": time.Since(r.start).Milliseconds(),
	})
}

var operations = map[string]bool{"connect": true, "interrogate": true, "clock-sync": true, "command": true, "monitor": true}

// opFlags are the flags of one operation.
type opFlags struct {
	holdMs, scn, fun, inf, dco, rii, durationMs, maxAsdus int
	time                                                  time.Time
}

func parseDCO(s string) (int, error) {
	switch strings.ToLower(s) {
	case "":
		return 0, usagef("command: --dco is required")
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

func runClient(op string, a *args) (int, error) {
	if !operations[op] {
		return exitUsage, usagef("client: unknown or unsupported operation '%s'", op)
	}
	host := a.str("--host", "")
	port, err := a.rng("--port", defaultPort, 1, 65535)
	if err != nil {
		return exitUsage, err
	}
	ca, err := a.rng("--common-address", 1, 0, 255)
	if err != nil {
		return exitUsage, err
	}
	timeoutMs, err := a.rng("--timeout-ms", 5000, 1, 3600000)
	if err != nil {
		return exitUsage, err
	}
	connectMs, err := a.rng("--connect-timeout-ms", 5000, 1, 3600000)
	if err != nil {
		return exitUsage, err
	}
	collectMs, err := a.rng("--collect-ms", 0, 0, 3600000)
	if err != nil {
		return exitUsage, err
	}
	linkAddr, err := a.link()
	if err != nil {
		return exitUsage, err
	}
	var f opFlags
	switch op {
	case "connect":
		f.holdMs, err = a.rng("--hold-ms", 0, 0, 3600000)
	case "interrogate":
		f.scn, err = a.rng("--scn", 0, 0, 255)
	case "clock-sync":
		if a.flag("--broadcast") {
			return exitUsage, usagef("clock-sync --broadcast: cs103 sends user data to known stations only, never SEND/NO REPLY to address 255")
		}
		f.time, err = parseTime(a.str("--time", ""))
	case "command":
		if f.fun, err = a.rng("--fun", -1, 0, 255); err == nil && f.fun < 0 {
			err = usagef("command: --fun is required")
		}
		if err == nil {
			if f.inf, err = a.rng("--inf", -1, 0, 255); err == nil && f.inf < 0 {
				err = usagef("command: --inf is required")
			}
		}
		if err == nil {
			f.dco, err = parseDCO(a.str("--dco", ""))
		}
		if err == nil {
			f.rii, err = a.rng("--rii", 1, 0, 255)
		}
	case "monitor":
		if f.durationMs, err = a.rng("--duration-ms", 1000, 0, 3600000); err == nil {
			f.maxAsdus, err = a.rng("--max-asdus", 0, 0, 1000000)
		}
	}
	if err != nil {
		return exitUsage, err
	}
	if err := a.finish("client " + op); err != nil {
		return exitUsage, err
	}
	if host == "" {
		return exitUsage, usagef("client: --host is required")
	}

	r := &result{op: op, start: time.Now()}
	code := execute(r, op, f, host, port, ca, linkAddr, timeoutMs, connectMs, collectMs)
	r.emit()
	return code, nil
}

func execute(r *result, op string, f opFlags, host string, port, ca, linkAddr, timeoutMs, connectMs, collectMs int) int {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	cfg := cs103.DefaultConfig()
	cfg.Transport = cs103.TransportTCPClient
	cfg.TCP = cs103.TCPConfig{Address: net.JoinHostPort(host, strconv.Itoa(port)), ConnectTimeout: time.Duration(connectMs) * time.Millisecond}
	cfg.LinkAddress = byte(linkAddr)
	cfg.AutoInit = false
	cfg.TimeoutResponseT1 = 2 * time.Second
	cfg.TimeoutRepeatT2 = 1 * time.Second
	cfg.TimeoutSendLinkMsg = 20 * time.Millisecond
	opt := cs103.NewOption()
	if err := opt.SetConfig(cfg); err != nil {
		r.fail("failed", "configuration: "+err.Error())
		return exitOperationFailed
	}
	opt.SetAutoReconnect(false)

	h := &handler{ch: make(chan *cs103.ASDU, 4096)}
	connErr := make(chan error, 1)
	lost := make(chan error, 1)
	active := make(chan struct{}, 8)
	c := cs103.NewClient(h, opt)
	// cs103's default logger writes to stdout, which carries the result only.
	c.SetLogProvider(stderrLog{})
	c.LogMode(true)
	c.SetConnectErrorHandler(func(_ *cs103.Client, err error) {
		select {
		case connErr <- err:
		default:
		}
	})
	c.SetConnectionLostHandler(func(_ *cs103.Client, err error) {
		select {
		case lost <- err:
		default:
		}
	})
	c.SetOnDeviceActiveHandler(func(*cs103.Client, byte) {
		select {
		case active <- struct{}{}:
		default:
		}
	})
	if err := c.Start(); err != nil {
		r.fail("failed", err.Error())
		return exitOperationFailed
	}
	defer func() { _ = c.Close() }()

	record := func(a *cs103.ASDU) object {
		d := asduDoc(a)
		r.asdus = append(r.asdus, d)
		return d
	}

	// ---- link start-up: request status of link, reset of the CU ----
	deadline := time.NewTimer(time.Duration(connectMs)*time.Millisecond + timeout)
	defer deadline.Stop()
startup:
	for {
		select {
		case err := <-connErr:
			r.fail("connect-failed", err.Error())
			return exitConnectFailed
		case err := <-lost:
			if err == nil || errors.Is(err, cs103.ErrTimeoutT1) {
				r.fail("link-init-timeout", "the station did not confirm the reset of the communication unit")
			} else {
				r.fail("connection-lost", "the connection closed during the link start-up: "+err.Error())
			}
			return exitConnectFailed
		case <-active:
			r.connected = true
			break startup
		case a := <-h.ch:
			record(a)
		case <-deadline.C:
			if c.IsConnected() {
				r.fail("link-init-timeout", "the station did not confirm the reset of the communication unit")
			} else {
				r.fail("connect-failed", "no TCP connection")
			}
			return exitConnectFailed
		}
	}

	// The class 1 data the reset queued (the identification) is fetched
	// before the request: wait until nothing but cyclic data arrives for a
	// moment.
	quiet := time.NewTimer(300 * time.Millisecond)
	limit := time.NewTimer(timeout)
settle:
	for {
		select {
		case a := <-h.ch:
			record(a)
			if a.Coa != cs103.CauseCyclic {
				stopReset(quiet, 300*time.Millisecond)
			}
		case <-quiet.C:
			break settle
		case <-limit.C:
			break settle
		case err := <-lost:
			r.fail("connection-lost", "connection closed: "+errString(err))
			return exitOperationFailed
		}
	}
	limit.Stop()

	// ---- the operation ----
	var done func(d object) (finished bool)
	opTimeout := timeout
	switch op {
	case "connect":
		done = nil
		opTimeout = time.Duration(f.holdMs) * time.Millisecond
	case "monitor":
		n := 0
		done = func(object) bool { n++; return f.maxAsdus > 0 && n >= f.maxAsdus }
		opTimeout = time.Duration(f.durationMs) * time.Millisecond
	case "interrogate":
		if err := c.SendTo(cs103.NewGeneralInterrogationASDU(byte(ca), byte(f.scn)), byte(linkAddr)); err != nil {
			r.fail("failed", err.Error())
			return exitOperationFailed
		}
		done = func(d object) bool {
			if d["typeId"] == 8 && objField(d, "scn") == f.scn {
				r.terminated = true
				return true
			}
			return false
		}
	case "clock-sync":
		if err := c.SendTo(cs103.NewTimeSyncASDU(byte(ca), f.time), byte(linkAddr)); err != nil {
			r.fail("failed", err.Error())
			return exitOperationFailed
		}
		done = func(d object) bool {
			if d["typeId"] == 6 && d["cot"] == 8 {
				r.confirmations = append(r.confirmations, object{"cot": 8, "negative": false})
				return true
			}
			return false
		}
	case "command":
		if err := c.SendTo(cs103.NewGeneralCommandASDU(byte(ca), byte(f.fun), byte(f.inf), cs103.DCO(f.dco), byte(f.rii)), byte(linkAddr)); err != nil {
			r.fail("failed", err.Error())
			return exitOperationFailed
		}
		done = func(d object) bool {
			t, cot := d["typeId"], d["cot"]
			if (t == 1 || t == 2) && (cot == 20 || cot == 21) && objField(d, "fun") == f.fun &&
				objField(d, "inf") == f.inf && objField(d, "sin") == f.rii {
				r.confirmations = append(r.confirmations, object{"cot": cot, "negative": cot == 21})
				return true
			}
			return false
		}
	}

	opTimer := time.NewTimer(opTimeout)
	defer opTimer.Stop()
	finished := false
wait:
	for !finished {
		select {
		case a := <-h.ch:
			d := record(a)
			if done != nil && done(d) {
				finished = true
			}
		case <-opTimer.C:
			break wait
		case err := <-lost:
			r.fail("connection-lost", "connection closed: "+errString(err))
			return exitOperationFailed
		}
	}

	switch op {
	case "connect", "monitor":
		if !c.IsLinkActive() {
			r.fail("connection-lost", "the link is no longer active")
			return exitOperationFailed
		}
	default:
		if !finished {
			r.fail("timeout", "the station did not answer in time")
			return exitOperationFailed
		}
		if op == "command" && r.confirmations[0]["negative"] == true {
			r.fail("negative-confirmation", "the station refused the command (cause 21)")
			drain(h, record, collectMs)
			return exitOperationFailed
		}
	}
	drain(h, record, collectMs)
	r.ok = true
	return exitOK
}

// drain keeps receiving for --collect-ms after the operation.
func drain(h *handler, record func(*cs103.ASDU) object, collectMs int) {
	if collectMs <= 0 {
		return
	}
	t := time.NewTimer(time.Duration(collectMs) * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case a := <-h.ch:
			record(a)
		case <-t.C:
			return
		}
	}
}

func stopReset(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

func errString(err error) string {
	if err == nil {
		return "closed"
	}
	return err.Error()
}

// objField is an integer field of the first object of an ASDU document, -1
// when there is none.
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

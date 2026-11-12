// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/iec101/server"
)

// maxObjects is the number of objects per ASDU of an interrogation answer:
// small enough for every point type and field size in an FT 1.2 frame.
const maxObjects = 20

// lineGrace is how long a new connection waits for the one being served to
// end before it is refused.
const lineGrace = 250 * time.Millisecond

// station is the controlled station of the contract, driven by a fixture.
// Its process state (point values, selections) lives as long as the
// process; every connection gets a fresh go-iec60870-5 server, so the link
// starts afresh on each (CONTAINER_CONTRACT.md, transport).
type station struct {
	mu sync.Mutex // guards the point values and selections of fx
	fx *fixture
	ca asdu.CommonAddr
	lc linkConfig
}

func event(name string) object { return object{"event": name} }

// runServer runs the station until SIGTERM or SIGINT, or until stop is
// closed when it is not nil (the tests).
func runServer(a *args, stop <-chan struct{}) (int, error) {
	fixturePath := a.str("--fixture", defaultFixture)
	bind := a.str("--bind-address", "0.0.0.0")
	readyFile := a.str("--ready-file", defaultReadyFile)
	port, err := a.num("--port", defaultPort)
	if err != nil {
		return 0, err
	}
	lc, err := a.link()
	if err != nil {
		return 0, err
	}
	if err := a.finish("server"); err != nil {
		return 0, err
	}
	if port < 0 || port > 65535 {
		return 0, usagef("--port: %d is not a TCP port", port)
	}

	fx, err := loadFixture(fixturePath)
	if err != nil {
		logf("fixture %s: %v", fixturePath, err)
		return exitFixtureInvalid, nil
	}
	if lc.commonAddr != 0 {
		fx.Station.CommonAddr = lc.commonAddr
	}
	if err := fitsFields(fx, lc); err != nil {
		logf("fixture %s: %v", fixturePath, err)
		return exitFixtureInvalid, nil
	}
	_ = os.Remove(readyFile)
	st := &station{fx: fx, ca: asdu.CommonAddr(fx.Station.CommonAddr), lc: lc}

	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		logf("cannot listen on %s:%d: %v", bind, port, err)
		return exitOperationFailed, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// line is the one line of the station: its token is taken while a
	// connection is served.
	line := make(chan struct{}, 1)
	line <- struct{}{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			// A controlling station that closed its connection and opens the
			// next one at once may find the station still taking the old one
			// down: it gets the line when that ends within lineGrace.
			t := time.NewTimer(lineGrace)
			select {
			case <-line:
				t.Stop()
			case <-t.C:
				// One line, one controlling station: a second connection
				// is closed.
				logf("refusing %s: a connection is already being served", nc.RemoteAddr())
				_ = nc.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				st.serve(ctx, nc)
				line <- struct{}{}
			}()
		}
	}()

	if err := os.WriteFile(readyFile, nil, 0o644); err != nil {
		logf("cannot write the ready file %s: %v", readyFile, err)
		_ = ln.Close()
		cancel()
		wg.Wait()
		return exitOperationFailed, nil
	}
	ready := event("ready")
	ready["adapter"], ready["address"] = adapter, ln.Addr().String()
	ready["fixture"], ready["commonAddress"] = fx.Name, fx.Station.CommonAddr
	ready["standard"], ready["mode"], ready["linkAddress"] = standard, lc.modeName(), lc.address
	ready["linkAddressSize"], ready["cotSize"], ready["caSize"], ready["ioaSize"] = lc.addrSize, lc.cotSize, lc.caSize, lc.ioaSize
	emit(ready)

	if stop == nil {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		defer signal.Stop(sig)
		<-sig
	} else {
		<-stop
	}

	_ = os.Remove(readyFile)
	_ = ln.Close()
	cancel()
	wg.Wait()
	emit(event("stopped"))
	return exitOK, nil
}

// serve runs a go-iec60870-5 controlled station on one TCP connection until
// it ends.
func (s *station) serve(ctx context.Context, nc net.Conn) {
	peer := nc.RemoteAddr().String()
	opened := event("connection-opened")
	opened["peer"] = peer
	emit(opened)
	defer func() {
		closed := event("connection-closed")
		closed["peer"] = peer
		emit(closed)
	}()

	srv, err := server.New(nc, server.Config{
		Params:     s.lc.params(),
		LinkAddr:   uint16(s.lc.address),
		Remote:     uint16(s.lc.remote),
		CommonAddr: s.ca,
		Handler:    s,
		// The contract's stations send no end of initialization: the
		// self-tests expect only the answers to their requests.
		NoEndOfInit: true,
		// Balanced: the station initializes its own direction again after
		// a failure; a short pause on a TCP stream.
		RetryInterval: time.Second,
		Logger:        stderrLogger{},
	})
	if err != nil {
		logf("line %s: %v", peer, err)
		_ = nc.Close()
		return
	}
	select {
	case <-srv.Done():
		if err := srv.Err(); err != nil && ctx.Err() == nil {
			logf("line %s ended: %v", peer, err)
		}
	case <-ctx.Done():
	}
	_ = srv.Close() // closes nc
}

// HandleASDU serves one ASDU of the controlling station (server.Handler).
// The server calls it one ASDU at a time.
func (s *station) HandleASDU(srv *server.Server, a *asdu.ASDU) {
	reject := func(cause asdu.Cause) { _ = srv.Reject(a, cause) }
	// reply mirrors the request with the station's common address.
	reply := func(cause asdu.Cause, negative bool) {
		r := a.Reply(cause, negative)
		r.CommonAddr = s.ca
		_ = srv.Send(r)
	}
	switch a.Type {
	case asdu.C_IC_NA_1, asdu.C_CI_NA_1, asdu.C_RD_NA_1, asdu.C_CS_NA_1, asdu.C_TS_NA_1,
		asdu.C_SC_NA_1, asdu.C_DC_NA_1, asdu.C_RC_NA_1, asdu.C_SE_NA_1, asdu.C_SE_NB_1, asdu.C_SE_NC_1:
	default:
		// Includes the CP56Time2a command types and C_TS_TA_1, which
		// IEC 101 does not have.
		reject(asdu.CauseUnknownType)
		return
	}
	if a.CommonAddr != s.ca && !s.lc.asduParams().IsBroadcast(a.CommonAddr) {
		reject(asdu.CauseUnknownCommonAddr)
		return
	}
	want := asdu.CauseActivation
	if a.Type == asdu.C_RD_NA_1 {
		want = asdu.CauseRequest
	}
	if len(a.Objects) != 1 || a.Cause != want && !(a.Type.IsProcessCommand() && a.Cause == asdu.CauseDeactivation) {
		reject(asdu.CauseUnknownCause)
		return
	}

	switch o := a.First().(type) {
	case asdu.Interrogation:
		qoi := int(o.Qualifier)
		group := qoi - 20
		accepted := qoi == 20 || group >= 1 && group <= 16 && s.fx.hasGroup(group)
		e := event("interrogation")
		e["commonAddress"], e["qoi"], e["accepted"] = int(a.CommonAddr), qoi, accepted
		emit(e)
		reply(asdu.CauseActivationCon, !accepted)
		if accepted {
			s.sendPoints(srv, asdu.Cause(qoi), a.Originator, func(p *point) bool {
				if group == 0 {
					return p.typ != asdu.M_IT_NA_1
				}
				return slices.Contains(p.Groups, group)
			})
			reply(asdu.CauseActivationTerm, false)
		}
	case asdu.CounterInterrogation:
		// Only "general request counter" with "read" (no freeze, no reset).
		qcc := int(o.Request) | int(o.Freeze)<<6
		accepted := o.Request == asdu.CounterGeneral && o.Freeze == asdu.FreezeRead
		e := event("counter-interrogation")
		e["commonAddress"], e["qcc"], e["accepted"] = int(a.CommonAddr), qcc, accepted
		emit(e)
		reply(asdu.CauseActivationCon, !accepted)
		if accepted {
			s.sendPoints(srv, asdu.CauseCounterGeneral, a.Originator, func(p *point) bool { return p.typ == asdu.M_IT_NA_1 })
			reply(asdu.CauseActivationTerm, false)
		}
	case asdu.Read:
		var answer *asdu.ASDU
		s.mu.Lock()
		if p := s.fx.point(int(o.IOA)); p != nil {
			answer = asdu.New(asdu.CauseRequest, s.ca, p.obj)
			answer.Originator = a.Originator
		}
		s.mu.Unlock()
		e := event("read")
		e["ioa"], e["accepted"] = int(o.IOA), answer != nil
		emit(e)
		if answer == nil {
			reject(asdu.CauseUnknownIOA)
			return
		}
		s.send(srv, answer)
	case asdu.ClockSync:
		e := event("clock-sync")
		e["time"] = formatTime(o.Time.Time)
		emit(e)
		reply(asdu.CauseActivationCon, false)
	case asdu.TestCommand:
		// No event for the test command (CONTAINER_CONTRACT.md).
		reply(asdu.CauseActivationCon, false)
	default:
		s.command(srv, a, reply)
	}
}

// send queues a monitor-direction ASDU: class 1 data that the controlling
// station fetches when it sees ACD (unbalanced), or the station's own
// message (balanced).
func (s *station) send(srv *server.Server, a *asdu.ASDU) {
	if err := srv.Send(a); err != nil {
		logf("cannot send %s: %v", a, err)
	}
}

// sendPoints sends the points selected, in ascending address order,
// consecutive points of one type sharing an ASDU (SQ = 0).
func (s *station) sendPoints(srv *server.Server, cause asdu.Cause, oa uint8, selected func(*point) bool) {
	var out []*asdu.ASDU
	var batch []asdu.InformationObject
	flush := func() {
		if len(batch) > 0 {
			a := asdu.New(cause, s.ca, batch...)
			a.Originator = oa
			out = append(out, a)
			batch = nil
		}
	}
	s.mu.Lock()
	var typ asdu.TypeID
	for _, p := range s.fx.Points {
		if !selected(p) {
			continue
		}
		if len(batch) > 0 && (p.typ != typ || len(batch) >= maxObjects) {
			flush()
		}
		typ = p.typ
		batch = append(batch, p.obj)
	}
	flush()
	s.mu.Unlock()
	for _, a := range out {
		s.send(srv, a)
	}
}

// stamped is the point object with a time tag, for the report after a
// command.
func stamped(o asdu.InformationObject, t asdu.Timestamp) asdu.InformationObject {
	switch p := o.(type) {
	case asdu.SinglePoint:
		p.Time = t
		return p
	case asdu.DoublePoint:
		p.Time = t
		return p
	case asdu.StepPosition:
		p.Time = t
		return p
	case asdu.Bitstring32:
		p.Time = t
		return p
	case asdu.MeasuredNormalized:
		p.Time = t
		return p
	case asdu.MeasuredScaled:
		p.Time = t
		return p
	case asdu.MeasuredFloat:
		p.Time = t
		return p
	case asdu.IntegratedTotal:
		p.Time = t
		return p
	}
	return o
}

// command serves a process command, following FIXTURES.md (process
// commands), first match wins.
func (s *station) command(srv *server.Server, a *asdu.ASDU, reply func(asdu.Cause, bool)) {
	obj := a.First()
	ioa := int(obj.Address())
	var (
		value any
		sel   bool
		valid = true
		apply func(*point) (asdu.InformationObject, bool) // the target's next value; false at a limit
	)
	switch c := obj.(type) {
	case asdu.SingleCommand:
		value, sel = c.Value, c.Select
		apply = func(p *point) (asdu.InformationObject, bool) {
			n := p.obj.(asdu.SinglePoint)
			n.Value = c.Value
			return n, true
		}
	case asdu.DoubleCommand:
		value, sel = int(c.Value), c.Select
		valid = c.Value == asdu.DoubleOff || c.Value == asdu.DoubleOn
		apply = func(p *point) (asdu.InformationObject, bool) {
			n := p.obj.(asdu.DoublePoint)
			n.Value = c.Value
			return n, true
		}
	case asdu.StepCommand:
		value, sel = int(c.Value), c.Select
		valid = c.Value == asdu.StepLower || c.Value == asdu.StepHigher
		apply = func(p *point) (asdu.InformationObject, bool) {
			n := p.obj.(asdu.StepPosition)
			if c.Value == asdu.StepLower {
				if n.Value <= -64 {
					return nil, false
				}
				n.Value--
			} else {
				if n.Value >= 63 {
					return nil, false
				}
				n.Value++
			}
			return n, true
		}
	case asdu.SetpointNormalized:
		value, sel = int(c.Value), c.Select
		apply = func(p *point) (asdu.InformationObject, bool) {
			n := p.obj.(asdu.MeasuredNormalized)
			n.Value = c.Value
			return n, true
		}
	case asdu.SetpointScaled:
		value, sel = int(c.Value), c.Select
		apply = func(p *point) (asdu.InformationObject, bool) {
			n := p.obj.(asdu.MeasuredScaled)
			n.Value = c.Value
			return n, true
		}
	case asdu.SetpointFloat:
		value, sel = float64(c.Value), c.Select
		apply = func(p *point) (asdu.InformationObject, bool) {
			n := p.obj.(asdu.MeasuredFloat)
			n.Value = c.Value
			return n, true
		}
	default:
		_ = srv.Reject(a, asdu.CauseUnknownType)
		return
	}

	e := event("command")
	e["type"], e["ioa"], e["cot"] = a.Type.String(), ioa, int(a.Cause)

	var (
		outcome string
		report  *asdu.ASDU
	)
	s.mu.Lock()
	cmd := s.fx.command(ioa)
	if cmd == nil || cmd.typ != a.Type {
		s.mu.Unlock()
		e["outcome"] = "unknown-ioa"
		emit(e)
		_ = srv.Reject(a, asdu.CauseUnknownIOA)
		return
	}
	e["value"], e["select"] = value, sel
	switch {
	case a.Cause == asdu.CauseDeactivation:
		cmd.selected = false
		outcome = "deactivated"
	case !valid:
		outcome = "rejected"
	case sel:
		cmd.selected = true
		outcome = "selected"
	case cmd.SelectRequired && !cmd.selected:
		outcome = "rejected"
	default:
		cmd.selected = false
		next, ok := apply(cmd.target)
		if !ok {
			outcome = "rejected" // the step position is at its limit
			break
		}
		cmd.target.obj = next
		report = asdu.New(asdu.Cause(*cmd.ReportCause), s.ca, stamped(next, asdu.Now()))
		report.Type = timeTagged[cmd.target.typ]
		outcome = "executed"
	}
	s.mu.Unlock()
	e["outcome"] = outcome
	emit(e)

	switch outcome {
	case "deactivated":
		reply(asdu.CauseDeactivationCon, false)
	case "rejected":
		reply(asdu.CauseActivationCon, true)
	case "selected":
		reply(asdu.CauseActivationCon, false)
	default:
		// Confirmation, then the new state of the target, then termination.
		reply(asdu.CauseActivationCon, false)
		s.send(srv, report)
		reply(asdu.CauseActivationTerm, false)
	}
}

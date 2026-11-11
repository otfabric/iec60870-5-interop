// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	iec "github.com/cB-Sigma/iec60870"
	"github.com/cB-Sigma/iec60870/cs101"
)

// Objects per ASDU when answering an interrogation; small enough for every
// type in an FT 1.2 frame.
const maxObjects = 20

// station is the controlled station of the contract, driven by a fixture.
type station struct {
	mu sync.Mutex // guards the point values and selections of fx
	fx *fixture
	ca uint16
	lc linkConfig
}

// line is the one connection of the station: an IEC 101 secondary (or
// balanced station B) on the byte stream of a TCP connection.
type line struct {
	sec  *cs101.Secondary
	peer string
}

func event(name string) object { return object{"event": name} }

func runServer(a *args) (int, error) {
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
	if port < 1 || port > 65535 {
		return 0, usagef("--port: %d is not a TCP port", port)
	}
	params = lc.params(0)

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
	st := &station{fx: fx, ca: uint16(fx.Station.CommonAddr), lc: lc}

	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		logf("cannot listen on %s:%d: %v", bind, port, err)
		return exitOperationFailed, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu      sync.Mutex
		current net.Conn // the one connection being served
		wg      sync.WaitGroup
	)
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			busy := current != nil
			if !busy {
				current = nc
			}
			mu.Unlock()
			if busy {
				// One line, one controlling station: a second connection is
				// closed at once.
				logf("refusing %s: a connection is already being served", nc.RemoteAddr())
				_ = nc.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				st.serve(ctx, nc)
				mu.Lock()
				current = nil
				mu.Unlock()
			}()
		}
	}()

	if err := os.WriteFile(readyFile, nil, 0o644); err != nil {
		logf("cannot write the ready file %s: %v", readyFile, err)
		_ = ln.Close()
		return exitOperationFailed, nil
	}
	ready := event("ready")
	ready["adapter"], ready["address"] = adapter, ln.Addr().String()
	ready["fixture"], ready["commonAddress"] = fx.Name, fx.Station.CommonAddr
	ready["standard"], ready["mode"], ready["linkAddress"] = standard, lc.modeName(), lc.address
	emit(ready)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	_ = os.Remove(readyFile)
	_ = ln.Close()
	cancel()
	mu.Lock()
	if current != nil {
		_ = current.Close()
	}
	mu.Unlock()
	wg.Wait()
	emit(event("stopped"))
	return exitOK, nil
}

// fitsFields checks that every address of the fixture fits the field sizes.
func fitsFields(fx *fixture, lc linkConfig) error {
	maxIOA := 1<<(8*lc.ioaSize) - 1
	for _, p := range fx.Points {
		if p.IOA > maxIOA {
			return errorf("point %d does not fit an information object address of %d octets", p.IOA, lc.ioaSize)
		}
	}
	for _, c := range fx.Commands {
		if c.IOA > maxIOA {
			return errorf("command %d does not fit an information object address of %d octets", c.IOA, lc.ioaSize)
		}
	}
	if fx.Station.CommonAddr >= 1<<(8*lc.caSize)-1 {
		return errorf("common address %d does not fit a common address of %d octets", fx.Station.CommonAddr, lc.caSize)
	}
	return nil
}

func errorf(format string, a ...any) error { return errors.New(sprintf(format, a...)) }

// serve runs the station on one TCP connection until it ends.
func (s *station) serve(ctx context.Context, nc net.Conn) {
	l := &line{peer: nc.RemoteAddr().String()}
	opened := event("connection-opened")
	opened["peer"] = l.peer
	emit(opened)
	defer func() {
		closed := event("connection-closed")
		closed["peer"] = l.peer
		emit(closed)
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	port := cs101.NewPort(nc)
	l.sec = cs101.NewSecondary(port, cs101.SecondaryConfig{
		LinkAddress: uint16(s.lc.address),
		AddrSize:    s.lc.addrSize,
		Balanced:    s.lc.balanced,
		// Unbalanced: a controlling station that stops polling for this long
		// has gone; the closed connection usually says so first.
		LinkTimeout: 10 * time.Minute,
		Timeout:     500 * time.Millisecond,
		Retries:     3,
		TestPeriod:  10 * time.Second,
		Name:        l.peer,
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for raw := range l.sec.ASDUs() {
			s.handle(ctx, l, raw)
		}
	}()
	err := l.sec.Run(ctx)
	if err != nil && ctx.Err() == nil {
		logf("line %s ended: %v", l.peer, err)
	}
	cancel()
	l.sec.Close()
	_ = nc.Close()
	<-done
}

// send queues an ASDU in class 1: in unbalanced mode the controlling
// station fetches it when it sees ACD, in balanced mode station B's primary
// sends it.
func (l *line) send(ctx context.Context, raw []byte) {
	if err := l.sec.Send(ctx, raw); err != nil && !errors.Is(err, context.Canceled) {
		logf("send to %s failed: %v", l.peer, err)
	}
}

func (l *line) sendASDU(ctx context.Context, a *iec.ASDU) {
	raw, err := a.Encode(params, time.UTC)
	if err != nil {
		logf("cannot encode %s: %v", a.Type, err)
		return
	}
	l.send(ctx, raw)
}

// pointASDU is an ASDU with the given points, all of one type.
func (s *station) pointASDU(pts []*point, timed bool, cause iec.Cause, oa uint8) *iec.ASDU {
	typ := pts[0].typ
	if timed {
		typ = timeTagged[typ]
	}
	a := &iec.ASDU{Type: typ, Cause: cause, Orig: oa, CA: s.ca}
	now := time.Now()
	for _, p := range pts {
		o := pointObject(p)
		if timed {
			o.Time = now
		}
		a.Objects = append(a.Objects, o)
	}
	return a
}

// sendPoints sends the points selected, in ascending address order,
// consecutive points of one type sharing an ASDU.
func (s *station) sendPoints(ctx context.Context, l *line, cause iec.Cause, oa uint8, selected func(*point) bool) {
	var out []*iec.ASDU
	var batch []*point
	s.mu.Lock()
	for _, p := range s.fx.Points {
		if !selected(p) {
			continue
		}
		if len(batch) > 0 && (p.typ != batch[0].typ || len(batch) >= maxObjects) {
			out = append(out, s.pointASDU(batch, false, cause, oa))
			batch = nil
		}
		batch = append(batch, p)
	}
	if len(batch) > 0 {
		out = append(out, s.pointASDU(batch, false, cause, oa))
	}
	s.mu.Unlock()
	for _, a := range out {
		l.sendASDU(ctx, a)
	}
}

// handle serves one ASDU of the controlling station.
func (s *station) handle(ctx context.Context, l *line, raw []byte) {
	if len(raw) < params.Header() {
		return
	}
	reject := func(cause iec.Cause) { l.send(ctx, mirror(raw, cause, true, headerCA(raw))) }
	reply := func(cause iec.Cause, negative bool) { l.send(ctx, mirror(raw, cause, negative, s.ca)) }
	a, err := iec.DecodeASDU(raw, params, time.UTC)
	if errors.Is(err, iec.ErrUnsupportedType) {
		reject(iec.CauseUnknownType)
		return
	}
	if err != nil {
		logf("dropping an ASDU that cannot be decoded: %v", err)
		return
	}
	if a.CA != s.ca && a.CA != s.lc.broadcastCA() {
		reject(iec.CauseUnknownCA)
		return
	}
	switch a.Type {
	case iec.CIcNa1:
		if a.Cause != iec.CauseActivation || len(a.Objects) != 1 {
			reject(iec.CauseUnknownCause)
			return
		}
		qoi := int(a.Objects[0].Value)
		group := qoi - 20
		accepted := qoi == 20 || group >= 1 && group <= 16 && s.fx.hasGroup(group)
		e := event("interrogation")
		e["commonAddress"], e["qoi"], e["accepted"] = int(a.CA), qoi, accepted
		emit(e)
		reply(iec.CauseActCon, !accepted)
		if accepted {
			s.sendPoints(ctx, l, iec.Cause(qoi), a.Orig, func(p *point) bool {
				if group == 0 {
					return p.typ != iec.MItNa1
				}
				return slices.Contains(p.Groups, group)
			})
			reply(iec.CauseActTerm, false)
		}
	case iec.CCiNa1:
		if a.Cause != iec.CauseActivation || len(a.Objects) != 1 {
			reject(iec.CauseUnknownCause)
			return
		}
		// Only "general request counter" with "read" (no freeze, no reset).
		qcc := int(a.Objects[0].Value)
		accepted := qcc == 5
		e := event("counter-interrogation")
		e["commonAddress"], e["qcc"], e["accepted"] = int(a.CA), qcc, accepted
		emit(e)
		reply(iec.CauseActCon, !accepted)
		if accepted {
			s.sendPoints(ctx, l, iec.CauseCounterGen, a.Orig, func(p *point) bool { return p.typ == iec.MItNa1 })
			reply(iec.CauseActTerm, false)
		}
	case iec.CRdNa1:
		if a.Cause != iec.CauseRequest || len(a.Objects) != 1 {
			reject(iec.CauseUnknownCause)
			return
		}
		ioa := int(a.Objects[0].IOA)
		var answer *iec.ASDU
		s.mu.Lock()
		if p := s.fx.point(ioa); p != nil {
			answer = s.pointASDU([]*point{p}, false, iec.CauseRequest, a.Orig)
		}
		s.mu.Unlock()
		e := event("read")
		e["ioa"], e["accepted"] = ioa, answer != nil
		emit(e)
		if answer == nil {
			reject(iec.CauseUnknownObject)
			return
		}
		l.sendASDU(ctx, answer)
	case iec.CCsNa1:
		if a.Cause != iec.CauseActivation || len(a.Objects) != 1 {
			reject(iec.CauseUnknownCause)
			return
		}
		e := event("clock-sync")
		e["time"] = formatTime(a.Objects[0].Time)
		emit(e)
		reply(iec.CauseActCon, false)
	case iec.CTsNa1:
		if a.Cause != iec.CauseActivation {
			reject(iec.CauseUnknownCause)
			return
		}
		reply(iec.CauseActCon, false)
	case iec.CScNa1, iec.CDcNa1, iec.CRcNa1, iec.CSeNa1, iec.CSeNb1, iec.CSeNc1:
		if a.Cause != iec.CauseActivation && a.Cause != iec.CauseDeactivation || len(a.Objects) != 1 {
			reject(iec.CauseUnknownCause)
			return
		}
		s.command(ctx, l, raw, a)
	default:
		// Includes the CP56Time2a command types, which IEC 101 does not have.
		reject(iec.CauseUnknownType)
	}
}

func (s *station) command(ctx context.Context, l *line, raw []byte, a *iec.ASDU) {
	o := a.Objects[0]
	base := a.Type
	value := o.Value
	var eventValue any
	valid := true
	switch base {
	case iec.CScNa1:
		eventValue = o.Value != 0
	case iec.CDcNa1, iec.CRcNa1:
		eventValue = int(o.Value)
		valid = o.Value == 1 || o.Value == 2
	case iec.CSeNa1:
		value = float64(rawNormalised(o.Value))
		eventValue = int(value)
	case iec.CSeNb1:
		eventValue = int(o.Value)
	default: // C_SE_NC_1
		eventValue = o.Value
	}

	e := event("command")
	e["type"], e["ioa"], e["cot"] = a.Type.String(), int(o.IOA), int(a.Cause)
	reply := func(cause iec.Cause, negative bool) { l.send(ctx, mirror(raw, cause, negative, s.ca)) }

	var outcome string
	var report *iec.ASDU
	s.mu.Lock()
	cmd := s.fx.command(int(o.IOA))
	if cmd == nil || cmd.typ != base {
		s.mu.Unlock()
		e["outcome"] = "unknown-ioa"
		emit(e)
		l.send(ctx, mirror(raw, iec.CauseUnknownObject, true, a.CA))
		return
	}
	e["value"], e["select"] = eventValue, o.Select
	switch {
	case a.Cause == iec.CauseDeactivation:
		cmd.selected = false
		outcome = "deactivated"
	case !valid:
		outcome = "rejected"
	case o.Select:
		cmd.selected = true
		outcome = "selected"
	case cmd.SelectRequired && !cmd.selected:
		outcome = "rejected"
	default:
		cmd.selected = false
		t := cmd.target
		next := value
		if base == iec.CRcNa1 {
			next = t.value - 1
			if value == 2 {
				next = t.value + 1
			}
		}
		if base == iec.CRcNa1 && (next < -64 || next > 63) {
			outcome = "rejected" // the step position is at its limit
		} else {
			t.value = next
			report = s.pointASDU([]*point{t}, true, iec.Cause(*cmd.ReportCause), 0)
			outcome = "executed"
		}
	}
	s.mu.Unlock()
	e["outcome"] = outcome
	emit(e)

	switch outcome {
	case "deactivated":
		reply(iec.CauseDeactCon, false)
	case "rejected":
		reply(iec.CauseActCon, true)
	default:
		reply(iec.CauseActCon, false)
		if report != nil {
			// Confirmation, then the new state of the target, then termination.
			l.sendASDU(ctx, report)
			reply(iec.CauseActTerm, false)
		}
	}
}

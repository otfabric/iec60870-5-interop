// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/go-iec60870-5/iec102/server"
)

// station is the integrated total station of a fixture (docs/FIXTURES.md,
// "Station behaviour (IEC 102)"). The library's server.Server runs the link
// and serves C_RD_NA_2, C_TI_NA_2, C_CI_NT_2 and C_CI_NU_2 from a
// server.Store; ree.Session answers C_AC_NA_2, C_FS_NA_2 and C_CS_TA_2; the
// tariff requests C_TA_VC_2 and C_TA_VM_2, which the library has no store
// for, are answered here with the library's ree.TariffInfo bodies. The
// store and the clock live as long as the process; every connection gets a
// fresh server (link state, class 2 queue) and session.
type station struct {
	fx     *fixture
	ca     uint16
	params iec102.Params
	store  *server.Store

	mu    sync.Mutex
	clock time.Time // stands still until set-time
}

func newStation(fx *fixture, o linkOpts) *station {
	st := &station{fx: fx, ca: fx.Station.CommonAddress, store: server.NewStore(), clock: fx.clock()}
	if o.caGiven {
		st.ca = o.ca
	}
	st.params = o.params(stationProfile{})
	for _, l := range fx.Totals {
		typ := iec102.M_IT_TG_2
		if l.Kind == "incremental" {
			typ = iec102.M_IT_TK_2
		}
		for _, p := range l.Periods {
			body := iec102.IntegratedTotals{T: typ, Time: mustTimeA(p.End)}
			for _, t := range p.Totals {
				body.Totals = append(body.Totals, iec102.IntegratedTotal{Addr: t.Address, Value: t.Value, Quality: iec102.Quality(t.Quality)})
			}
			st.store.AddPeriod(l.Record, body)
		}
	}
	return st
}

// mustTimeA converts a validated time a of the fixture.
func mustTimeA(s string) iec102.TimeA {
	t, _ := time.Parse(layoutA, s)
	return iec102.TimeAOf(t, time.UTC)
}

func (st *station) now() time.Time {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.clock
}

// setTime sets the clock to the wall-clock fields of a C_CS_TA_2. The
// library's server keeps the clock as a time.Time in the station's location
// (UTC): the SU bit of the time set is not kept, and M_TI_TA_2 reports SU 0.
func (st *station) setTime(t iec102.TimeB) {
	tt, ok := t.Time(time.UTC)
	if !ok {
		return
	}
	st.mu.Lock()
	st.clock = tt
	st.mu.Unlock()
}

// stationProfile decodes the requests the fixture's station answers and
// leaves every other standard type undecoded (iec102.Raw), so that it
// reaches the handler and is refused with cause 14 as the fixture says: the
// library's server would also serve the other read commands C_CI_NA_2 ..
// C_CI_NS_2 and C_SP_NA_2 / C_SP_NB_2 from its store, which the contract's
// station does not have. The REE types are those of ree.Profile.
type stationProfile struct{}

func (stationProfile) DecodeBody(a *iec102.ASDU, data []byte, p iec102.Params) (iec102.Body, error) {
	switch a.Type {
	case iec102.C_RD_NA_2, iec102.C_TI_NA_2, iec102.C_CI_NT_2, iec102.C_CI_NU_2:
		return nil, nil // the standard codec
	}
	if !a.Type.Private() {
		return iec102.Raw{Data: bytes.Clone(data)}, nil
	}
	return ree.Profile.DecodeBody(a, data, p)
}

// HandleASDU answers C_TA_VC_2 with the fixture's current records and
// C_TA_VM_2 with its stored records whose end lies within the range: the
// confirmation, one record per M_TA_VC_2 / M_TA_VM_2, the termination;
// cause 16 for a register without tariff information, 18 when no record
// matches. Anything else is refused with cause 14.
func (st *station) HandleASDU(s *server.Server, a *iec102.ASDU) {
	q, ok := a.Body.(ree.Request)
	if !ok || (q.T != ree.C_TA_VC_2 && q.T != ree.C_TA_VM_2) {
		_ = s.Mirror(a, iec102.CauseUnknownType)
		return
	}
	var recs []tariffRecord
	known := false
	for _, t := range st.fx.Tariffs {
		if t.Register != a.Record {
			continue
		}
		known = true
		if q.T == ree.C_TA_VC_2 {
			recs = t.Current
			continue
		}
		lo, hi := textA(q.From), textA(q.To)
		for _, r := range t.Stored {
			if r.PeriodEnd >= lo && r.PeriodEnd <= hi {
				recs = append(recs, r)
			}
		}
	}
	switch {
	case !known:
		_ = s.Mirror(a, iec102.CauseUnknownRecordAddr)
		return
	case len(recs) == 0:
		_ = s.Mirror(a, iec102.CauseNoPeriod)
		return
	}
	answer := ree.M_TA_VC_2
	if q.T == ree.C_TA_VM_2 {
		answer = ree.M_TA_VM_2
	}
	_ = s.Mirror(a, iec102.CauseActivationCon)
	for _, r := range recs {
		_ = s.Send(iec102.New(iec102.CauseRequest, a.CommonAddr, a.Record, ree.TariffInfo{T: answer, Records: []ree.TariffRecord{tariffOf(r)}}))
	}
	_ = s.Mirror(a, iec102.CauseActivationTerm)
}

func tariffOf(r tariffRecord) ree.TariffRecord {
	return ree.TariffRecord{
		Addr: r.Address, ActiveAbs: r.ActiveAbs, ActiveInc: r.ActiveInc, ActiveQual: r.ActiveQuality,
		ReactiveIndAbs: r.ReactiveIndAbs, ReactiveIndInc: r.ReactiveIndInc, ReactiveIndQual: r.ReactiveIndQuality,
		ReactiveCapAbs: r.ReactiveCapAbs, ReactiveCapInc: r.ReactiveCapInc, ReactiveCapQual: r.ReactiveCapQuality,
		MaxPower: r.MaxPower, MaxPowerTime: mustTimeA(r.MaxPowerTime), MaxPowerQual: r.MaxPowerQuality,
		ExcessPower: r.ExcessPower, ExcessPowerQual: r.ExcessPowerQuality,
		Start: mustTimeA(r.PeriodStart), End: mustTimeA(r.PeriodEnd),
	}
}

// config is the library configuration of one connection.
func (st *station) config(o linkOpts, ev *events) server.Config {
	p := st.fx.Station.Product
	cfg := server.Config{
		LinkAddrSize: o.linkAddrSize,
		LinkAddr:     o.linkAddr,
		CommonAddr:   st.ca,
		Params:       st.params,
		ProductSpec:  iec102.ProductSpec{Standard: p.Standard, Manufacturer: p.Manufacturer, Product: p.Product},
		Store:        st.store,
		Clock:        st.now,
		Handler:      &ree.Session{Key: st.fx.Station.AccessKey, OnSetTime: st.setTime, Next: st},
	}
	cfg.Link.OnFrame = ev.onFrame
	return cfg
}

// events turns what the station sends on one connection into the server
// events of the contract ("IEC 102 server"). The library has no hook for
// the requests it serves itself, so the events are read from the answers as
// they go out (link.Config.OnFrame): an event comes when the controlling
// station fetches the answer (the confirmation, the refusal, the
// termination), with the outcome the station really sent.
type events struct {
	st   *station
	emit func(object)

	last    ft12.Frame // the previous frame sent: a repetition is the same frame again
	pending *pendingRead
}

// pendingRead is a confirmed read whose termination has not gone out yet.
type pendingRead struct {
	typ     iec102.TypeID
	record  uint8
	periods map[string]bool // read-totals: the integration periods sent (by their end)
	records int             // read-tariff: the records sent
}

func sameFrame(a, b ft12.Frame) bool {
	return a.Kind == b.Kind && a.Control == b.Control && a.Addr == b.Addr && bytes.Equal(a.Data, b.Data)
}

func (e *events) onFrame(tx bool, f ft12.Frame) {
	if !tx {
		return
	}
	repeated := sameFrame(f, e.last)
	e.last = f
	e.last.Data = bytes.Clone(f.Data)
	if repeated || f.Kind != ft12.KindVariable {
		return
	}
	p := e.st.params
	p.Profile = ree.Profile // the answers: integrated totals, tariff records, the mirrors
	a, err := iec102.Decode(f.Data, p)
	if err != nil {
		return
	}
	e.answer(a)
}

func (e *events) answer(a *iec102.ASDU) {
	code := int(a.Cause.Code())
	refused := a.Cause.Refusal()
	positive := !refused && a.Cause.Code() == iec102.CauseActivationCon
	ev := func(name string, fields ...field) { e.emit(append(object{{"event", name}}, fields...)) }
	if refused && a.CommonAddr != e.st.ca {
		ev("refused", field{"typeId", int(a.Type)}, field{"commonAddress", int(a.CommonAddr)}, field{"cot", code})
		return
	}
	switch a.Type {
	case ree.C_AC_NA_2:
		ev("login", field{"accepted", positive})
	case ree.C_FS_NA_2:
		if positive {
			ev("logout")
		}
	case ree.C_CS_TA_2:
		if b, ok := a.Body.(ree.SetClock); ok && positive {
			ev("set-time", field{"time", textB(b.Time)}, field{"su", b.Time.SummerTime})
		} else if refused {
			ev("refused", field{"typeId", int(a.Type)}, field{"commonAddress", int(a.CommonAddr)}, field{"cot", code})
		}
	case iec102.C_CI_NT_2, iec102.C_CI_NU_2, ree.C_TA_VC_2, ree.C_TA_VM_2:
		e.read(a, code, refused, positive)
	case iec102.M_TI_TA_2:
		if b, ok := a.Body.(iec102.SystemTime); ok {
			ev("read-time", field{"time", textB(b.Time)})
		}
	case iec102.P_MP_NA_2:
		ev("read-product")
	case ree.M_TA_VC_2, ree.M_TA_VM_2:
		if e.pending != nil {
			e.pending.records += a.VSQ.Number()
		}
	default:
		if _, ok := a.Type.IntegratedTotalsType(); ok {
			// M_IT_TA_2 .. M_IT_TM_2: a period of the pending read.
			if b, ok := a.Body.(iec102.IntegratedTotals); ok && e.pending != nil && e.pending.periods != nil {
				e.pending.periods[textA(b.Time)] = true
			}
			return
		}
		if refused {
			ev("refused", field{"typeId", int(a.Type)}, field{"commonAddress", int(a.CommonAddr)}, field{"cot", code})
		}
	}
}

// read follows a read of totals or tariff information: the refusal, or the
// confirmation, the data and the termination.
func (e *events) read(a *iec102.ASDU, code int, refused, positive bool) {
	totals := a.Type == iec102.C_CI_NT_2 || a.Type == iec102.C_CI_NU_2
	name, key := "read-tariff", "register"
	if totals {
		name, key = "read-totals", "record"
	}
	switch {
	case refused:
		e.pending = nil
		e.emit(object{{"event", name}, {"typeId", int(a.Type)}, {key, int(a.Record)}, {"cot", code}})
	case positive:
		e.pending = &pendingRead{typ: a.Type, record: a.Record}
		if totals {
			e.pending.periods = map[string]bool{}
		}
	case a.Cause.Code() == iec102.CauseActivationTerm && e.pending != nil && e.pending.typ == a.Type:
		p := e.pending
		e.pending = nil
		if totals {
			e.emit(object{{"event", name}, {"typeId", int(a.Type)}, {key, int(p.record)}, {"cot", int(iec102.CauseActivationCon)}, {"periods", len(p.periods)}})
		} else {
			e.emit(object{{"event", name}, {"typeId", int(a.Type)}, {key, int(p.record)}, {"cot", int(iec102.CauseActivationCon)}, {"records", p.records}})
		}
	}
}

// runServer serves the fixture until ctx ends (SIGTERM, SIGINT).
func runServer(ctx context.Context, o serverOpts, stdout, stderr io.Writer) int {
	fx, err := loadFixture(o.fixture)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "[otfabric] fixture %s: %v\n", o.fixture, err)
		return exitFixture
	}
	if !o.link.caGiven && o.link.caSize == 1 && fx.Station.CommonAddress > 255 {
		_, _ = fmt.Fprintf(stderr, "[otfabric] fixture %s: common address %d does not fit one octet\n", o.fixture, fx.Station.CommonAddress)
		return exitFixture
	}
	st := newStation(fx, o.link)
	emit := func(v object) { writeJSONLine(stdout, v) }

	ln, err := net.Listen("tcp", net.JoinHostPort(o.bind, strconv.Itoa(o.port)))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "[otfabric] listen: %v\n", err)
		return exitFailed
	}
	if err := os.WriteFile(o.readyFile, nil, 0o644); err != nil {
		_ = ln.Close()
		_, _ = fmt.Fprintf(stderr, "[otfabric] ready file: %v\n", err)
		return exitFailed
	}
	emit(object{
		{"event", "ready"}, {"adapter", adapterName}, {"address", ln.Addr().String()}, {"fixture", fx.Name},
		{"commonAddress", int(st.ca)}, {"standard", standardName}, {"mode", "unbalanced"}, {"linkAddress", int(o.link.linkAddr)},
	})

	var (
		mu      sync.Mutex
		current *server.Server // the connection being served: one line, one controlling station
		busy    bool
		wg      sync.WaitGroup
	)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			continue
		}
		mu.Lock()
		if busy {
			mu.Unlock()
			_ = conn.Close() // another one while a connection is served: closed at once
			continue
		}
		busy = true
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			peer := conn.RemoteAddr().String()
			emit(object{{"event", "connection-opened"}, {"peer", peer}})
			ev := &events{st: st, emit: emit}
			s, err := server.New(conn, st.config(o.link, ev))
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "[otfabric] station: %v\n", err)
				_ = conn.Close()
			} else {
				mu.Lock()
				current = s
				mu.Unlock()
				select {
				case <-s.Done():
				case <-ctx.Done():
				}
				_ = s.Close()
			}
			emit(object{{"event", "connection-closed"}, {"peer", peer}})
			mu.Lock()
			current, busy = nil, false
			mu.Unlock()
		}()
	}
	_ = os.Remove(o.readyFile)
	mu.Lock()
	if current != nil {
		_ = current.Close()
	}
	mu.Unlock()
	wg.Wait()
	emit(object{{"event", "stopped"}})
	return exitOK
}

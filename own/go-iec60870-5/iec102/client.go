// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
	"github.com/otfabric/go-iec60870-5/link"
)

// failure is an error of the result document.
type failure struct {
	code string
	msg  string
	exit int
}

func (f *failure) Error() string { return f.code + ": " + f.msg }

// confirmation is an entry of the result's confirmations.
type confirmation struct {
	Cot      int  `json:"cot"`
	Negative bool `json:"negative"`
}

// result is the client result document ("IEC 102 client": that of IEC 101
// with session for --profile ree).
type result struct {
	SchemaVersion string         `json:"schemaVersion"`
	Adapter       string         `json:"adapter"`
	Operation     string         `json:"operation"`
	OK            bool           `json:"ok"`
	Error         *resultError   `json:"error"`
	Connected     bool           `json:"connected"`
	Confirmations []confirmation `json:"confirmations"`
	Terminated    bool           `json:"terminated"`
	Session       *session       `json:"session,omitempty"`
	ASDUs         []object       `json:"asdus"`
	ElapsedMs     float64        `json:"elapsedMs"`
}

type resultError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type session struct {
	Login  bool `json:"login"`
	Logout bool `json:"logout"`
}

// controller runs one operation with the library's client.
type controller struct {
	o      clientOpts
	c      *client.Client
	params iec102.Params

	mu    sync.Mutex
	asdus []*iec102.ASDU // every ASDU received, in order
}

func (k *controller) keep(as ...*iec102.ASDU) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.asdus = append(k.asdus, as...)
}

// request sends a with the library's client.Request (the procedures of
// iec102/client and iec102/ree) and keeps what arrived. Each request waits
// at most --timeout-ms.
func (k *controller) request(a *iec102.ASDU, until client.Until) ([]*iec102.ASDU, error) {
	ctx, cancel := context.WithTimeout(context.Background(), k.o.timeout)
	defer cancel()
	out, err := k.c.Request(ctx, a, until)
	k.keep(out...)
	return out, err
}

// classify turns an error of a request into the failure of the contract.
func classify(err error) *failure {
	var refused *iec102.RefusedError
	switch {
	case errors.As(err, &refused):
		return &failure{"negative-confirmation", err.Error(), exitFailed}
	case errors.Is(err, link.ErrClosed), errors.Is(err, iec102.ErrClosed), errors.Is(err, io.EOF):
		return &failure{"connection-lost", err.Error(), exitFailed}
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, link.ErrTimeout):
		return &failure{"timeout", err.Error(), exitFailed}
	}
	return &failure{"failed", err.Error(), exitFailed}
}

// confirmations are the mirrors of req that confirm (cause 7) or refuse it,
// and whether its activation termination arrived.
func confirmations(out []*iec102.ASDU, req *iec102.ASDU) ([]confirmation, bool) {
	confs := []confirmation{}
	term := false
	for _, a := range out {
		if !a.Mirrors(req) {
			continue
		}
		switch {
		case a.Cause.Refusal() || a.Cause.Code() == iec102.CauseActivationCon:
			confs = append(confs, confirmation{Cot: int(a.Cause.Code()), Negative: a.Cause.Refusal()})
		case a.Cause.Code() == iec102.CauseActivationTerm:
			term = true
		}
	}
	return confs, term
}

func runClient(o clientOpts, stdout, stderr io.Writer) int {
	t0 := time.Now()
	doc := result{SchemaVersion: schemaVersion, Adapter: adapterName, Operation: o.op, Confirmations: []confirmation{}}
	if o.link.profile == profileREE {
		doc.Session = &session{}
	}
	k := &controller{o: o, params: o.link.params(ree.Profile)}
	f := k.run(&doc)
	k.mu.Lock()
	doc.ASDUs = make([]object, 0, len(k.asdus))
	for _, a := range k.asdus {
		doc.ASDUs = append(doc.ASDUs, asduDoc(a, k.params))
	}
	k.mu.Unlock()
	doc.ElapsedMs = math.Round(float64(time.Since(t0).Microseconds())/100) / 10
	code := exitOK
	if f == nil {
		doc.OK = true
	} else {
		doc.Error = &resultError{Code: f.code, Message: f.msg}
		code = f.exit
		_, _ = fmt.Fprintf(stderr, "[otfabric] %s: %s\n", f.code, f.msg)
	}
	writeJSONLine(stdout, doc)
	return code
}

// run connects, starts the link (and the REE session), runs the operation
// and ends the session.
func (k *controller) run(doc *result) *failure {
	o := k.o
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(o.host, strconv.Itoa(o.port)), o.connectTimeout)
	if err != nil {
		return &failure{"connect-failed", err.Error(), exitConnect}
	}
	cfg := client.Config{
		LinkAddrSize: o.link.linkAddrSize,
		LinkAddr:     o.link.linkAddr,
		CommonAddr:   o.link.ca,
		Params:       k.params,
		// The ASDUs of another common address: no request waits for them.
		Handler: func(a *iec102.ASDU) { k.keep(a) },
	}
	cfg.Link.Timeout = min(link.DefaultTimeout, o.timeout)
	c, err := client.New(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return &failure{"failed", err.Error(), exitFailed}
	}
	defer func() { _ = c.Close() }()
	k.c = c

	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	err = c.Init(ctx)
	cancel()
	if err != nil {
		if errors.Is(err, link.ErrClosed) || errors.Is(err, io.EOF) {
			return &failure{"connection-lost", "the connection closed during the link start-up: " + err.Error(), exitConnect}
		}
		return &failure{"link-init-timeout", "the link start-up was not confirmed: " + err.Error(), exitConnect}
	}
	doc.Connected = true

	if doc.Session != nil {
		if f := k.login(); f != nil {
			return f
		}
		doc.Session.Login = true
	}
	f := k.operation(doc)
	if doc.Session != nil && (f == nil || f.code != "connection-lost") {
		lf := k.logout()
		doc.Session.Logout = lf == nil
		if f == nil {
			f = lf
		}
	}
	return f
}

// login starts the REE session: C_AC_NA_2 with cause 6, record address 0
// and the access key (as ree.Login sends it), confirmed with cause 7.
func (k *controller) login() *failure {
	req := iec102.New(iec102.CauseActivation, k.o.link.ca, 0, ree.AccessKey{Key: k.o.link.key})
	_, err := k.request(req, client.UntilConfirmation)
	if err == nil {
		return nil
	}
	if f := classify(err); f.code == "connection-lost" {
		return f
	}
	return &failure{"login-refused", "the station refused the access key or did not confirm it: " + err.Error(), exitFailed}
}

// logout ends the REE session: C_FS_NA_2 with cause 6 (as ree.Logout),
// confirmed with cause 7.
func (k *controller) logout() *failure {
	req := iec102.New(iec102.CauseActivation, k.o.link.ca, 0, ree.Request{T: ree.C_FS_NA_2})
	if _, err := k.request(req, client.UntilConfirmation); err != nil {
		f := classify(err)
		f.msg = "end of session: " + f.msg
		return f
	}
	return nil
}

// operation runs the operation and fills confirmations and terminated.
func (k *controller) operation(doc *result) *failure {
	o, ca := k.o, k.o.link.ca
	var (
		req    *iec102.ASDU
		until  client.Until
		answer iec102.TypeID // the reply of a read, or 0
	)
	switch o.op {
	case opConnect:
		return nil
	case opReadTime:
		req, until, answer = iec102.New(iec102.CauseRequest, ca, 0, iec102.Read{T: iec102.C_TI_NA_2}), client.UntilReply, iec102.M_TI_TA_2
	case opReadProduct:
		req, until, answer = iec102.New(iec102.CauseRequest, ca, 0, iec102.Read{T: iec102.C_RD_NA_2}), client.UntilReply, iec102.P_MP_NA_2
	case opReadTotals:
		t := iec102.C_CI_NT_2
		if o.kind == "incremental" {
			t = iec102.C_CI_NU_2
		}
		q := iec102.Read{T: t, First: o.first, Last: o.last, From: o.from, To: o.to}
		req, until = iec102.New(iec102.CauseActivation, ca, o.record, q), client.UntilTermination
	case opSetTime:
		req, until = iec102.New(iec102.CauseActivation, ca, 0, ree.SetClock{Time: o.time}), client.UntilConfirmation
	case opReadTariff:
		q := ree.Request{T: ree.C_TA_VC_2}
		if o.ranged {
			q = ree.Request{T: ree.C_TA_VM_2, From: o.from, To: o.to}
		}
		req, until = iec102.New(iec102.CauseActivation, ca, o.record, q), client.UntilTermination
	default:
		return &failure{"failed", "operation " + o.op + " not implemented", exitFailed}
	}
	out, err := k.request(req, until)
	doc.Confirmations, doc.Terminated = confirmations(out, req)
	if err != nil {
		return classify(err)
	}
	positive := len(doc.Confirmations) > 0 && !doc.Confirmations[len(doc.Confirmations)-1].Negative
	switch {
	case answer != 0:
		last := out[len(out)-1]
		if last.Type != answer || last.Cause.Code() != iec102.CauseRequest {
			return &failure{"failed", fmt.Sprintf("%s answered by %s", req.Type, last), exitFailed}
		}
	case !positive:
		return &failure{"failed", "no confirmation with cause 7", exitFailed}
	case until == client.UntilTermination && !doc.Terminated:
		return &failure{"failed", "no activation termination", exitFailed}
	}
	return nil
}

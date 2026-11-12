// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
	iecclient "github.com/otfabric/go-iec60870-5/iec102/client"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
)

// testFixture is a copy of fixtures/iec102/baseline/fixture.json of the
// harness: the module is built without the rest of the repository.
const testFixture = "testdata/fixture.json"

func TestUsageErrors(t *testing.T) {
	tr := []string{"--from", "2026-01-01T00:00", "--to", "2026-01-01T05:00"}
	ree := []string{"--profile", "ree", "--access-key", "7"}
	cat := func(a ...[]string) []string {
		var out []string
		for _, x := range a {
			out = append(out, x...)
		}
		return out
	}
	cl := func(op string, extra ...string) []string {
		return append([]string{"client", op, "--host", "nowhere"}, extra...)
	}
	cases := map[string][]string{
		"no command":           {},
		"unknown command":      {"frobnicate"},
		"client without host":  {"client", "read-time"},
		"no operation":         {"client", "--host", "x"},
		"unknown operation":    cl("frobnicate"),
		"IEC 104 operation":    cl("interrogate"),
		"unknown flag":         cl("read-time", "--frobnicate", "1"),
		"flag without value":   cl("read-time", "--timeout-ms"),
		"flag twice":           cl("read-time", "--port", "1", "--port", "2"),
		"cot size":             cl("read-time", "--cot-size", "1"),
		"ioa size":             cl("read-time", "--ioa-size", "2"),
		"originator":           cl("read-time", "--originator-address", "1"),
		"remote link address":  cl("read-time", "--remote-link-address", "2"),
		"collect":              cl("read-time", "--collect-ms", "2"),
		"balanced":             cl("read-time", "--link-mode", "balanced"),
		"broadcast link addr":  cl("read-time", "--link-address", "65535"),
		"link addr one octet":  cl("read-time", "--link-address-size", "1", "--link-address", "255"),
		"link addr size 3":     cl("read-time", "--link-address-size", "3"),
		"link addr size 0":     cl("read-time", "--link-address-size", "0"),
		"ca size 3":            cl("read-time", "--ca-size", "3"),
		"ca beyond one octet":  cl("read-time", "--ca-size", "1", "--common-address", "256"),
		"unknown profile":      cl("read-time", "--profile", "iberdrola"),
		"ree without key":      cl("connect", "--profile", "ree"),
		"key without ree":      cl("connect", "--access-key", "7"),
		"key beyond 4 octets":  cl("connect", "--profile", "ree", "--access-key", "4294967296"),
		"totals without from":  cl("read-totals", "--to", "2026-01-01T05:00"),
		"totals from after to": cl("read-totals", "--from", "2026-01-02T00:00", "--to", "2026-01-01T00:00"),
		"totals no time a":     cl("read-totals", "--from", "2026-01-01", "--to", "2026-01-01T05:00"),
		"totals bad month":     cl("read-totals", "--from", "2026-13-01T00:00", "--to", "2026-12-01T05:00"),
		"totals year 2100":     cl("read-totals", "--from", "2100-01-01T00:00", "--to", "2100-01-01T05:00"),
		"totals unknown kind":  cat(cl("read-totals", "--kind", "cumulative"), tr),
		"totals first > last":  cat(cl("read-totals", "--first", "5", "--last", "2"), tr),
		"totals record 256":    cat(cl("read-totals", "--record", "256"), tr),
		"totals first 0":       cat(cl("read-totals", "--first", "0"), tr),
		"set-time standard":    cl("set-time", "--time", "2026-01-15T10:20:30.500"),
		"set-time no time":     cat(cl("set-time"), ree),
		"set-time no ms":       cat(cl("set-time", "--time", "2026-01-15T10:20"), ree),
		"tariff standard":      cl("read-tariff"),
		"tariff from only":     cat(cl("read-tariff", "--from", "2025-11-01T00:00"), ree),
		"tariff from after to": cat(cl("read-tariff", "--from", "2025-12-01T00:00", "--to", "2025-11-01T00:00"), ree),
		"time flag of read":    cl("read-time", "--time", "2026-01-15T10:20:30.500"),
		"server unknown flag":  {"server", "--frobnicate", "1"},
		"server access key":    {"server", "--access-key", "7"},
		"server cot size":      {"server", "--cot-size", "1"},
		"server balanced":      {"server", "--link-mode", "balanced"},
		"server bad profile":   {"server", "--profile", "x"},
		"server link size 3":   {"server", "--link-address-size", "3"},
		"capabilities args":    {"print-capabilities", "x"},
		"print-fixture name":   {"print-fixture", "../etc"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if got := run(args, &out, &errb); got != exitUsage {
				t.Fatalf("run(%q) = %d, want %d; stdout %q", args, got, exitUsage, out.String())
			}
			if out.Len() != 0 {
				t.Errorf("stdout on a usage error: %q", out.String())
			}
		})
	}
}

func TestParseClient(t *testing.T) {
	o, err := parseClient([]string{"read-totals", "--host", "h", "--kind", "incremental", "--record", "12",
		"--first", "2", "--last", "3", "--from", "2026-01-01T00:00", "--to", "2026-01-01T05:00",
		"--link-address-size", "1", "--link-address", "254", "--ca-size", "1", "--common-address", "255"})
	if err != nil {
		t.Fatal(err)
	}
	if o.kind != "incremental" || o.record != 12 || o.first != 2 || o.last != 3 || textA(o.from) != "2026-01-01T00:00" ||
		textA(o.to) != "2026-01-01T05:00" || o.link.linkAddrSize != 1 || o.link.linkAddr != 254 || o.link.caSize != 1 || o.link.ca != 255 {
		t.Errorf("parsed %+v", o)
	}
	if o.from.SummerTime || o.from.Invalid {
		t.Errorf("time a with flags: %+v", o.from)
	}
	o, err = parseClient([]string{"set-time", "--host", "h", "--profile", "ree", "--access-key", "4294967295", "--time", "2026-02-03T04:05:06.789"})
	if err != nil {
		t.Fatal(err)
	}
	if textB(o.time) != "2026-02-03T04:05:06.789" || o.link.key != 4294967295 || o.link.linkAddrSize != 2 || o.link.caSize != 2 {
		t.Errorf("parsed %+v", o)
	}
}

func TestCapabilities(t *testing.T) {
	var out bytes.Buffer
	if run([]string{"print-capabilities"}, &out, &out) != exitOK {
		t.Fatal(out.String())
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("not one line: %q", out.String())
	}
	var c struct {
		Adapter  string          `json:"adapter"`
		Standard string          `json:"standard"`
		Features map[string]bool `json:"features"`
		Roles    map[string]bool `json:"roles"`
		Upstream struct {
			IndependentEngine bool `json:"independentEngine"`
		} `json:"upstream"`
		ClientOperations []string `json:"clientOperations"`
	}
	if err := json.Unmarshal(out.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.Adapter != "otfabric" || c.Standard != "iec60870-5-102" || !c.Roles["server"] || !c.Roles["client"] ||
		c.Upstream.IndependentEngine || len(c.ClientOperations) != 6 {
		t.Errorf("capabilities %+v", c)
	}
	for _, f := range []string{"integratedTotals", "readTime", "setTime", "productIdentification", "tariffInformation", "reeSession", "clockSync", "unbalancedMode"} {
		if !c.Features[f] {
			t.Errorf("feature %s not true", f)
		}
	}
	if c.Features["balancedMode"] || c.Features["multipleConnections"] {
		t.Error("balancedMode or multipleConnections true")
	}
}

func TestFixtureValidation(t *testing.T) {
	if _, err := loadFixture(testFixture); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	base, err := os.ReadFile(testFixture)
	if err != nil {
		t.Fatal(err)
	}
	edit := func(f func(m map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(base, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		b, _ := json.Marshal(m)
		return b
	}
	at := func(m map[string]any, path ...any) map[string]any {
		var v any = m
		for _, p := range path {
			switch k := p.(type) {
			case string:
				v = v.(map[string]any)[k]
			case int:
				v = v.([]any)[k]
			}
		}
		return v.(map[string]any)
	}
	bad := map[string][]byte{
		"not json":         []byte("{"),
		"unknown member":   edit(func(m map[string]any) { m["points"] = []any{} }),
		"no station":       edit(func(m map[string]any) { delete(m, "station") }),
		"no access key":    edit(func(m map[string]any) { delete(at(m, "station"), "accessKey") }),
		"schema version":   edit(func(m map[string]any) { m["schemaVersion"] = "2.0" }),
		"kind":             edit(func(m map[string]any) { at(m, "totals", 0)["kind"] = "cumulative" }),
		"record 256":       edit(func(m map[string]any) { at(m, "totals", 0)["record"] = 256 }),
		"clock":            edit(func(m map[string]any) { at(m, "station")["clock"] = "2026-01-15T10:20" }),
		"end":              edit(func(m map[string]any) { at(m, "totals", 0, "periods", 0)["end"] = "2026-01-01" }),
		"end order":        edit(func(m map[string]any) { at(m, "totals", 0, "periods", 1)["end"] = "2025-01-01T00:00" }),
		"address 0":        edit(func(m map[string]any) { at(m, "totals", 0, "periods", 0, "totals", 0)["address"] = 0 }),
		"duplicate addr":   edit(func(m map[string]any) { at(m, "totals", 0, "periods", 0, "totals", 1)["address"] = 1 }),
		"no value":         edit(func(m map[string]any) { delete(at(m, "totals", 0, "periods", 0, "totals", 0), "value") }),
		"value beyond u32": edit(func(m map[string]any) { at(m, "totals", 0, "periods", 0, "totals", 0)["value"] = 4294967296 }),
		"second list":      edit(func(m map[string]any) { at(m, "totals", 1)["kind"] = "absolute" }),
		"register":         edit(func(m map[string]any) { at(m, "tariffs", 0)["register"] = 133 }),
		"record order":     edit(func(m map[string]any) { at(m, "tariffs", 0, "current", 0)["periodStart"] = "2027-01-01T00:00" }),
		"no period end":    edit(func(m map[string]any) { delete(at(m, "tariffs", 0, "stored", 0), "periodEnd") }),
		"ca 65535":         edit(func(m map[string]any) { at(m, "station")["commonAddress"] = 65535 }),
	}
	dir := t.TempDir()
	for name, data := range bad {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadFixture(p); err == nil {
				t.Error("accepted")
			}
			var out, errb bytes.Buffer
			o := serverOpts{fixture: p, bind: "127.0.0.1", port: 0, readyFile: filepath.Join(dir, "ready"), link: linkOpts{linkAddrSize: 2, caSize: 2, linkAddr: 1}}
			if got := runServer(context.Background(), o, &out, &errb); got != exitFixture {
				t.Errorf("server exit %d, want %d", got, exitFixture)
			}
		})
	}
}

// syncBuffer is the stdout of a server under test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) events() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.b.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// testServer starts the station of the test fixture on a free port.
func testServer(t *testing.T, args ...string) (port int, out *syncBuffer, stop func() int) {
	t.Helper()
	dir := t.TempDir()
	o, err := parseServer(append([]string{"--fixture", testFixture, "--bind-address", "127.0.0.1", "--port", "0",
		"--ready-file", filepath.Join(dir, "ready")}, args...))
	if err != nil {
		t.Fatal(err)
	}
	out = &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	code := make(chan int, 1)
	go func() { code <- runServer(ctx, o, out, os.Stderr) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(o.readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("server not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ev := out.events()
	if len(ev) == 0 || ev[0]["event"] != "ready" {
		t.Fatalf("first event %v", ev)
	}
	_, p, _ := net.SplitHostPort(ev[0]["address"].(string))
	port, _ = strconv.Atoi(p)
	stopped := false
	stop = func() int {
		if stopped {
			return exitOK
		}
		stopped = true
		cancel()
		select {
		case c := <-code:
			return c
		case <-time.After(5 * time.Second):
			t.Fatal("server did not stop")
			return -1
		}
	}
	t.Cleanup(func() { stop() })
	return port, out, stop
}

// doc is the part of a result document the tests look at.
type doc struct {
	OK    bool `json:"ok"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
	Connected     bool           `json:"connected"`
	Confirmations []confirmation `json:"confirmations"`
	Terminated    bool           `json:"terminated"`
	Session       *session       `json:"session"`
	ASDUs         []struct {
		Type          *string          `json:"type"`
		TypeID        int              `json:"typeId"`
		Cot           int              `json:"cot"`
		Negative      bool             `json:"negative"`
		CommonAddress int              `json:"commonAddress"`
		Record        int              `json:"record"`
		Count         int              `json:"count"`
		Time          string           `json:"time"`
		Objects       []map[string]any `json:"objects"`
	} `json:"asdus"`
}

func (d doc) code() string {
	if d.Error == nil {
		return "-"
	}
	return d.Error.Code
}

// runOp runs one operation against port and returns its document and exit
// code. It waits until the station serves no connection (one at a time).
func runOp(t *testing.T, port int, out *syncBuffer, args ...string) (doc, int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		opened, closed := 0, 0
		for _, e := range out.events() {
			switch e["event"] {
			case "connection-opened":
				opened++
			case "connection-closed":
				closed++
			}
		}
		if opened == closed || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	full := []string{args[0], "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	if !slices.Contains(args, "--timeout-ms") {
		full = append(full, "--timeout-ms", "3000")
	}
	o, err := parseClient(append(full, args[1:]...))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	code := runClient(o, &b, os.Stderr)
	if strings.Count(b.String(), "\n") != 1 {
		t.Fatalf("result is not one line: %q", b.String())
	}
	var d doc
	if err := json.Unmarshal(b.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d, code
}

func TestClientAgainstServer(t *testing.T) {
	port, out, stop := testServer(t)
	ree := []string{"--profile", "ree", "--access-key", "7"}
	conf := func(cot int, neg bool) []confirmation { return []confirmation{{Cot: cot, Negative: neg}} }
	same := func(a, b []confirmation) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	d, code := runOp(t, port, out, "connect")
	if code != 0 || !d.OK || !d.Connected || d.Session != nil || len(d.ASDUs) != 0 {
		t.Errorf("connect: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, append([]string{"connect"}, ree...)...)
	if code != 0 || d.Session == nil || !d.Session.Login || !d.Session.Logout || len(d.ASDUs) != 2 ||
		d.ASDUs[0].TypeID != 183 || d.ASDUs[0].Cot != 7 || d.ASDUs[1].TypeID != 187 || d.ASDUs[1].Cot != 7 {
		t.Errorf("connect ree: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, "connect", "--profile", "ree", "--access-key", "8")
	if code != 1 || d.code() != "login-refused" || d.Session.Login || len(d.ASDUs) != 1 || !d.ASDUs[0].Negative {
		t.Errorf("wrong key: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, "read-product")
	if code != 0 || len(d.ASDUs) != 1 || d.ASDUs[0].TypeID != 71 || d.ASDUs[0].Cot != 5 ||
		d.ASDUs[0].Objects[0]["product"] != float64(20261115) || d.ASDUs[0].Objects[0]["manufacturer"] != float64(33) {
		t.Errorf("read-product: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, append([]string{"read-time"}, ree...)...)
	if code != 0 || len(d.ASDUs) != 3 || d.ASDUs[1].TypeID != 72 || d.ASDUs[1].Objects[0]["time"] != "2026-01-15T10:20:30.500" ||
		d.ASDUs[1].Objects[0]["su"] != false || len(d.Confirmations) != 0 {
		t.Errorf("read-time: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, "read-totals", "--from", "2026-01-01T00:00", "--to", "2026-01-01T05:00")
	if code != 0 || !same(d.Confirmations, conf(7, false)) || !d.Terminated || len(d.ASDUs) != 5 ||
		d.ASDUs[1].TypeID != 8 || d.ASDUs[1].Time != "2026-01-01T01:00" || d.ASDUs[1].Count != 6 || d.ASDUs[1].Record != 11 ||
		d.ASDUs[2].Objects[0]["quality"] != float64(16) || d.ASDUs[3].Objects[5]["quality"] != float64(128) ||
		d.ASDUs[0].Objects[0]["from"] != "2026-01-01T00:00" || d.ASDUs[4].Cot != 10 {
		t.Errorf("read-totals: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, "read-totals", "--kind", "incremental", "--first", "1", "--last", "3",
		"--from", "2026-01-01T02:00", "--to", "2026-01-01T03:00")
	if code != 0 || len(d.ASDUs) != 4 || d.ASDUs[1].TypeID != 11 || d.ASDUs[1].Count != 3 || d.ASDUs[2].Time != "2026-01-01T03:00" {
		t.Errorf("read-totals subset: exit %d %+v", code, d)
	}
	for _, r := range []struct {
		cot  int
		args []string
	}{
		{18, []string{"--from", "2027-01-01T00:00", "--to", "2027-01-01T05:00"}},
		{16, []string{"--record", "12", "--from", "2026-01-01T00:00", "--to", "2026-01-01T05:00"}},
		{17, []string{"--first", "7", "--last", "8", "--from", "2026-01-01T00:00", "--to", "2026-01-01T05:00"}},
		{16, []string{"--common-address", "2", "--from", "2026-01-01T00:00", "--to", "2026-01-01T05:00"}},
	} {
		d, code = runOp(t, port, out, append([]string{"read-totals"}, r.args...)...)
		if code != 1 || d.code() != "negative-confirmation" || d.Terminated || !same(d.Confirmations, conf(r.cot, true)) || len(d.ASDUs) != 1 {
			t.Errorf("read-totals %v: exit %d %+v", r.args, code, d)
		}
	}
	d, code = runOp(t, port, out, append([]string{"read-tariff"}, ree...)...)
	if code != 0 || !d.Terminated || len(d.ASDUs) != 6 || d.ASDUs[2].TypeID != 135 || d.ASDUs[2].Count != 1 ||
		d.ASDUs[2].Objects[0]["maxPowerTime"] != "2026-01-12T20:15" || d.ASDUs[3].Objects[0]["address"] != float64(2) {
		t.Errorf("read-tariff: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, append([]string{"read-tariff", "--from", "2025-11-15T00:00", "--to", "2026-01-01T00:00"}, ree...)...)
	if code != 0 || len(d.ASDUs) != 6 || d.ASDUs[2].TypeID != 136 || d.ASDUs[3].Objects[0]["activeQuality"] != float64(128) ||
		d.ASDUs[1].Objects[0]["to"] != "2026-01-01T00:00" {
		t.Errorf("read-tariff stored: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, append([]string{"read-tariff", "--register", "135"}, ree...)...)
	if code != 1 || d.code() != "negative-confirmation" || !same(d.Confirmations, conf(16, true)) || !d.Session.Logout {
		t.Errorf("read-tariff refused: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, append([]string{"set-time", "--time", "2026-02-03T04:05:06.789"}, ree...)...)
	if code != 0 || !same(d.Confirmations, conf(7, false)) || d.ASDUs[1].TypeID != 181 || d.ASDUs[1].Objects[0]["time"] != "2026-02-03T04:05:06.789" {
		t.Errorf("set-time: exit %d %+v", code, d)
	}
	d, code = runOp(t, port, out, "read-time")
	if code != 0 || d.ASDUs[0].Objects[0]["time"] != "2026-02-03T04:05:06.789" {
		t.Errorf("read-time after set-time: exit %d %+v", code, d)
	}
	// Wrong link address: the station does not answer.
	d, code = runOp(t, port, out, "read-time", "--link-address", "9", "--timeout-ms", "500")
	if code != 3 || d.code() != "link-init-timeout" || d.Connected {
		t.Errorf("link start-up failure: exit %d %+v", code, d)
	}

	if c := stop(); c != exitOK {
		t.Errorf("stop: exit %d", c)
	}
	ev := out.events()
	if ev[len(ev)-1]["event"] != "stopped" {
		t.Errorf("last event %v", ev[len(ev)-1])
	}
	want := []string{
		`{"accepted":true,"event":"login"}`,
		`{"event":"logout"}`,
		`{"accepted":false,"event":"login"}`,
		`{"event":"read-product"}`,
		`{"event":"read-time","time":"2026-01-15T10:20:30.500"}`,
		`{"cot":7,"event":"read-totals","periods":3,"record":11,"typeId":122}`,
		`{"cot":7,"event":"read-totals","periods":2,"record":11,"typeId":123}`,
		`{"cot":18,"event":"read-totals","record":11,"typeId":122}`,
		`{"cot":16,"event":"read-totals","record":12,"typeId":122}`,
		`{"cot":17,"event":"read-totals","record":11,"typeId":122}`,
		`{"commonAddress":2,"cot":16,"event":"refused","typeId":122}`,
		`{"cot":7,"event":"read-tariff","records":2,"register":134,"typeId":133}`,
		`{"cot":7,"event":"read-tariff","records":2,"register":134,"typeId":134}`,
		`{"cot":16,"event":"read-tariff","register":135,"typeId":133}`,
		`{"event":"set-time","su":false,"time":"2026-02-03T04:05:06.789"}`,
		`{"event":"read-time","time":"2026-02-03T04:05:06.789"}`,
	}
	var got []string
	opened, closed := 0, 0
	for _, e := range ev {
		switch e["event"] {
		case "connection-opened":
			opened++
		case "connection-closed":
			closed++
		case "ready", "stopped", "logout", "login", "read-product", "read-time", "read-totals", "read-tariff", "set-time", "refused":
			if e["event"] != "ready" && e["event"] != "stopped" {
				b, _ := json.Marshal(e)
				got = append(got, string(b))
			}
		default:
			t.Errorf("unknown event %v", e)
		}
	}
	// The sessions of the REE operations add login and logout around
	// them; compare the events in order without those.
	var plain []string
	for _, g := range got {
		if g != `{"accepted":true,"event":"login"}` && g != `{"event":"logout"}` {
			plain = append(plain, g)
		}
	}
	var wantPlain []string
	for _, w := range want {
		if w != `{"accepted":true,"event":"login"}` && w != `{"event":"logout"}` {
			wantPlain = append(wantPlain, w)
		}
	}
	if strings.Join(plain, "\n") != strings.Join(wantPlain, "\n") {
		t.Errorf("events\n got %s\nwant %s", strings.Join(plain, "\n     "), strings.Join(wantPlain, "\n     "))
	}
	if opened != 17 || closed != 17 {
		t.Errorf("%d connections opened, %d closed, want 17", opened, closed)
	}
}

func TestOneOctetSizes(t *testing.T) {
	sizes := []string{"--link-address-size", "1", "--ca-size", "1", "--link-address", "3"}
	port, out, _ := testServer(t, sizes...)
	d, code := runOp(t, port, out, append([]string{"read-totals", "--from", "2026-01-01T00:00", "--to", "2026-01-01T01:00"}, sizes...)...)
	if code != 0 || len(d.ASDUs) != 3 || d.ASDUs[1].Count != 6 {
		t.Errorf("one-octet sizes: exit %d %+v", code, d)
	}
	// A client with two-octet fields is not understood.
	d, code = runOp(t, port, out, "read-time", "--link-address", "3", "--timeout-ms", "500")
	if code != 3 || d.code() != "link-init-timeout" {
		t.Errorf("mismatched sizes: exit %d %+v", code, d)
	}
}

func TestStationRefusesOtherTypes(t *testing.T) {
	port, out, _ := testServer(t)
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	p := iec102.Params{Location: time.UTC, Profile: ree.Profile}
	c, err := iecclient.New(conn, iecclient.Config{LinkAddr: 1, CommonAddr: 1, Params: p})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// C_CI_NI_2 (oldest operational period), which the library's server
	// would serve from its store, C_SP_NA_2 and an REE request the station
	// does not have (C_RM_NA_2): the fixture's station refuses each with 14.
	for _, body := range []iec102.Body{
		iec102.Read{T: iec102.C_CI_NI_2}, iec102.Read{T: iec102.C_SP_NA_2}, ree.Request{T: ree.C_RM_NA_2},
	} {
		_, err := c.Request(ctx, iec102.New(iec102.CauseActivation, 1, 11, body), iecclient.UntilTermination)
		var refused *iec102.RefusedError
		if !errors.As(err, &refused) || refused.Cause().Code() != iec102.CauseUnknownType {
			t.Errorf("%s: %v, want a refusal with cause 14", body.Type(), err)
		}
	}
	_ = c.Close()
	deadline := time.Now().Add(2 * time.Second)
	var refusedEvents int
	for time.Now().Before(deadline) {
		refusedEvents = 0
		for _, e := range out.events() {
			if e["event"] == "refused" && e["cot"] == float64(14) && e["commonAddress"] == float64(1) {
				refusedEvents++
			}
		}
		if refusedEvents == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if refusedEvents != 3 {
		t.Errorf("%d refused events, want 3: %v", refusedEvents, out.events())
	}
}

func TestSecondConnectionClosed(t *testing.T) {
	port, out, _ := testServer(t)
	first, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	deadline := time.Now().Add(2 * time.Second)
	for len(out.events()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	second, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, err := second.Read(make([]byte, 1)); err == nil {
		t.Errorf("second connection read %d octets, want it closed", n)
	}
}

func TestConnectFailed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	o, err := parseClient([]string{"read-time", "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--connect-timeout-ms", "500"})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if code := runClient(o, &b, os.Stderr); code != exitConnect {
		t.Errorf("exit %d", code)
	}
	var d doc
	if err := json.Unmarshal(b.Bytes(), &d); err != nil || d.code() != "connect-failed" || d.Connected {
		t.Errorf("document %s: %v", b.String(), err)
	}
}

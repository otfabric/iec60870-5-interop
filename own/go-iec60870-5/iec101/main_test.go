// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
)

// baseline is fixtures/iec101/baseline/fixture.json of the harness (the
// image build has no checkout of the fixtures, so the test carries it).
const baseline = `{
  "schemaVersion": "1.0",
  "name": "baseline",
  "station": { "commonAddress": 1 },
  "points": [
    { "ioa": 100, "type": "M_SP_NA_1", "value": true },
    { "ioa": 101, "type": "M_DP_NA_1", "value": 2 },
    { "ioa": 102, "type": "M_ST_NA_1", "value": 5 },
    { "ioa": 103, "type": "M_BO_NA_1", "value": 2863311530 },
    { "ioa": 110, "type": "M_SP_NA_1", "value": false, "quality": ["NT", "IV"] },
    { "ioa": 111, "type": "M_SP_NA_1", "value": true, "quality": ["BL", "SB"] },
    { "ioa": 200, "type": "M_ME_NA_1", "value": 16384 },
    { "ioa": 201, "type": "M_ME_NB_1", "value": 1234 },
    { "ioa": 202, "type": "M_ME_NC_1", "value": 230.5 },
    { "ioa": 210, "type": "M_ME_NB_1", "value": -32768, "quality": ["OV"] },
    { "ioa": 211, "type": "M_ME_NB_1", "value": -1 },
    { "ioa": 300, "type": "M_IT_NA_1", "value": 123456, "sequence": 1 }
  ],
  "commands": [
    { "ioa": 500, "type": "C_SC_NA_1", "target": 100, "reportCause": 11 },
    { "ioa": 501, "type": "C_DC_NA_1", "target": 101, "reportCause": 3 },
    { "ioa": 502, "type": "C_SE_NC_1", "target": 202, "reportCause": 11 },
    { "ioa": 503, "type": "C_SE_NA_1", "target": 200, "reportCause": 11 },
    { "ioa": 504, "type": "C_SE_NB_1", "target": 201, "reportCause": 11 },
    { "ioa": 505, "type": "C_RC_NA_1", "target": 102, "reportCause": 11 },
    { "ioa": 510, "type": "C_SC_NA_1", "target": 100, "reportCause": 11, "selectRequired": true }
  ]
}`

// output collects the JSON documents of the package.
type output struct{ buf bytes.Buffer }

func (o *output) Write(p []byte) (int, error) { return o.buf.Write(p) }

// docs returns the documents written so far.
func (o *output) docs(t *testing.T) []map[string]any {
	t.Helper()
	outMu.Lock()
	raw := o.buf.String()
	outMu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("stdout line %q is not JSON: %v", line, err)
		}
		out = append(out, d)
	}
	return out
}

func capture(t *testing.T) *output {
	t.Helper()
	o := &output{}
	outMu.Lock()
	prev := stdout
	stdout = o
	outMu.Unlock()
	t.Cleanup(func() {
		outMu.Lock()
		stdout = prev
		outMu.Unlock()
	})
	return o
}

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCapabilities(t *testing.T) {
	o := capture(t)
	if code, err := run([]string{"print-capabilities"}); code != exitOK || err != nil {
		t.Fatalf("exit %d, %v", code, err)
	}
	d := o.docs(t)
	if len(d) != 1 {
		t.Fatalf("%d documents", len(d))
	}
	c := d[0]
	if c["adapter"] != "otfabric" || c["standard"] != standard || c["protocol"] != standard || c["transport"] != "tcp-stream" {
		t.Errorf("capabilities %v", c)
	}
	up := c["upstream"].(map[string]any)
	if up["name"] != "otfabric/go-iec60870-5" || up["license"] != "MIT" || up["independentEngine"] != false {
		t.Errorf("upstream %v", up)
	}
	for _, f := range c["features"].(map[string]any) {
		if _, ok := f.(bool); !ok {
			t.Errorf("feature %v is not a boolean", f)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	capture(t)
	for _, argv := range [][]string{
		{},
		{"frobnicate"},
		{"client"},
		{"client", "interrogate"},
		{"client", "frobnicate", "--host", "x"},
		{"client", "file-get", "--host", "x", "--ioa", "1"},
		{"client", "interrogate", "--host", "x", "--frobnicate", "1"},
		{"client", "command", "--host", "x", "--type", "C_SC_NA_1", "--ioa", "500"},
		{"client", "command", "--host", "x", "--type", "C_SE_NB_1", "--ioa", "504", "--value", "99999"},
		{"client", "command", "--host", "x", "--type", "C_SC_NA_1", "--ioa", "500", "--value", "true", "--with-time"},
		{"client", "command", "--host", "x", "--type", "C_SC_NA_1", "--ioa", "500", "--value", "true", "--qualifier", "32"},
		{"client", "command", "--host", "x", "--type", "C_SE_NC_1", "--ioa", "502", "--value", "1", "--qualifier", "128"},
		{"client", "interrogate", "--host", "x", "--link-mode", "sideways"},
		{"client", "interrogate", "--host", "x", "--ca-size", "3"},
		{"client", "interrogate", "--host", "x", "--ioa-size", "4"},
		{"client", "interrogate", "--host", "x", "--cot-size", "3"},
		{"client", "interrogate", "--host", "x", "--link-mode", "unbalanced", "--remote-link-address", "2"},
		{"client", "interrogate", "--host", "x", "--link-address-size", "0"},
		{"client", "read", "--host", "x", "--ioa", "300", "--ioa-size", "1"},
		{"client", "interrogate", "--host", "x", "--common-address", "256"},
		{"server", "--frobnicate"},
		{"server", "--k", "4"},
		{"server", "--link-address", "255"},
		{"server", "--link-address-size", "3"},
		{"server", "--link-mode", "balanced", "--link-address-size", "2", "--remote-link-address", "65535"},
	} {
		if code := exitCode(run(argv)); code != exitUsage {
			t.Errorf("%v: exit %d, want 2", argv, code)
		}
	}
}

func TestFixtures(t *testing.T) {
	capture(t)
	fx, err := loadFixture(writeFixture(t, baseline))
	if err != nil {
		t.Fatal(err)
	}
	if p := fx.point(103); p.obj != (asdu.Bitstring32{IOA: 103, Value: 2863311530}) {
		t.Errorf("point 103: %#v", p.obj)
	}
	if err := fitsFields(fx, linkConfig{ioaSize: 1, caSize: 1}); err == nil {
		t.Error("addresses beyond one octet fit --ioa-size 1")
	}
	for _, bad := range []string{
		strings.Replace(baseline, `"target": 100, "reportCause": 11 },`, `"target": 300, "reportCause": 11 },`, 1),
		strings.Replace(baseline, `"value": 2 }`, `"value": 4 }`, 1),
		strings.Replace(baseline, `"quality": ["OV"]`, `"quality": ["CY"]`, 1),
		strings.Replace(baseline, `"schemaVersion": "1.0"`, `"schemaVersion": "2.0"`, 1),
		strings.Replace(baseline, `"reportCause": 3`, `"reportCause": 5`, 1),
		`{`,
	} {
		path := writeFixture(t, bad)
		if code := exitCode(runServer(&args{rest: []string{"--fixture", path, "--port", "0",
			"--ready-file", filepath.Join(t.TempDir(), "ready")}}, nil)); code != exitFixtureInvalid {
			t.Errorf("exit %d for an invalid fixture, want 4", code)
		}
	}
	if code := exitCode(runServer(&args{rest: []string{"--fixture", writeFixture(t, baseline), "--ioa-size", "1", "--port", "0"}}, nil)); code != exitFixtureInvalid {
		t.Errorf("exit %d for a fixture that does not fit --ioa-size 1, want 4", code)
	}
}

func TestPrintFixture(t *testing.T) {
	o := capture(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "baseline"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "baseline", "fixture.json"), []byte(baseline), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := fixturesDir
	fixturesDir = dir
	t.Cleanup(func() { fixturesDir = prev })
	if code := exitCode(run([]string{"print-fixture"})); code != exitOK || o.buf.String() != baseline {
		t.Errorf("print-fixture: exit %d", code)
	}
	for _, name := range []string{"nope", "../baseline"} {
		if code := exitCode(run([]string{"print-fixture", name})); code != exitOperationFailed {
			t.Errorf("print-fixture %s: exit %d, want 1", name, code)
		}
	}
}

// startServer runs the station in-process and returns its port.
func startServer(t *testing.T, o *output, flags ...string) string {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan int)
	argv := append([]string{"--fixture", writeFixture(t, baseline), "--port", "0", "--bind-address", "127.0.0.1",
		"--ready-file", filepath.Join(t.TempDir(), "ready")}, flags...)
	go func() { done <- exitCode(runServer(&args{rest: argv}, stop)) }()
	t.Cleanup(func() {
		close(stop)
		if code := <-done; code != exitOK {
			t.Errorf("server exit %d", code)
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range o.docs(t) {
			if d["event"] == "ready" {
				addr := d["address"].(string)
				return addr[strings.LastIndex(addr, ":")+1:]
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server not ready")
	return ""
}

// lastResult is the last client result document.
func lastResult(docs []map[string]any) map[string]any {
	var r map[string]any
	for _, d := range docs {
		if d["operation"] != nil {
			r = d
		}
	}
	return r
}

// summary renders a client result as tests/iec101/cases.tsv states it.
func summary(r map[string]any) string {
	code := "-"
	if e, ok := r["error"].(map[string]any); ok {
		code = e["code"].(string)
	}
	var conf, asdus []string
	for _, c := range r["confirmations"].([]any) {
		c := c.(map[string]any)
		s := strconv.Itoa(int(c["cot"].(float64)))
		if c["negative"].(bool) {
			s += "n"
		}
		conf = append(conf, s)
	}
	for _, a := range r["asdus"].([]any) {
		a := a.(map[string]any)
		typ, _ := a["type"].(string)
		if typ == "" {
			typ = fmt.Sprint(a["typeId"])
		}
		asdus = append(asdus, fmt.Sprintf("%s/%d", typ, int(a["cot"].(float64))))
	}
	return fmt.Sprintf("ok=%t error=%s conf=%s term=%t asdus=%s", r["ok"], code, strings.Join(conf, ","),
		r["terminated"], strings.Join(asdus, ","))
}

const gi = "C_IC_NA_1/7,M_SP_NA_1/20,M_DP_NA_1/20,M_ST_NA_1/20,M_BO_NA_1/20,M_SP_NA_1/20,M_ME_NA_1/20,M_ME_NB_1/20,M_ME_NC_1/20,M_ME_NB_1/20,C_IC_NA_1/10"

// TestLoopback runs this adapter's client against its server, in both link
// modes and with several field sizes: a subset of tests/iec101/cases.tsv.
func TestLoopback(t *testing.T) {
	type tc struct {
		exit int
		want string
		op   []string
	}
	cases := []tc{
		{0, "ok=true error=- conf= term=false asdus=", []string{"connect", "--hold-ms", "200"}},
		{0, "ok=true error=- conf=7 term=true asdus=" + gi, []string{"interrogate"}},
		{0, "ok=true error=- conf=7 term=true asdus=" + gi, []string{"interrogate", "--common-address", "255"}},
		{1, "ok=false error=negative-confirmation conf=46n term=false asdus=C_IC_NA_1/46", []string{"interrogate", "--common-address", "7"}},
		{1, "ok=false error=negative-confirmation conf=7n term=false asdus=C_IC_NA_1/7", []string{"interrogate", "--qoi", "21"}},
		{0, "ok=true error=- conf=7 term=true asdus=C_CI_NA_1/7,M_IT_NA_1/37,C_CI_NA_1/10", []string{"counter-interrogate"}},
		{1, "ok=false error=negative-confirmation conf=7n term=false asdus=C_CI_NA_1/7", []string{"counter-interrogate", "--qcc", "69"}},
		{0, "ok=true error=- conf= term=false asdus=M_ME_NC_1/5", []string{"read", "--ioa", "202"}},
		{1, "ok=false error=negative-confirmation conf=47n term=false asdus=C_RD_NA_1/47", []string{"read", "--ioa", "999"}},
		{0, "ok=true error=- conf=7 term=false asdus=C_CS_NA_1/7", []string{"clock-sync", "--time", "2026-10-09T15:35:12.345Z"}},
		{0, "ok=true error=- conf=7 term=false asdus=C_TS_NA_1/7", []string{"test-command"}},
		{1, "ok=false error=negative-confirmation conf=7n term=false asdus=C_DC_NA_1/7", []string{"command", "--type", "C_DC_NA_1", "--ioa", "501", "--value", "3"}},
		{1, "ok=false error=negative-confirmation conf=7n term=false asdus=C_SC_NA_1/7", []string{"command", "--type", "C_SC_NA_1", "--ioa", "510", "--value", "true"}},
		{1, "ok=false error=negative-confirmation conf=47n term=false asdus=C_DC_NA_1/47", []string{"command", "--type", "C_DC_NA_1", "--ioa", "500", "--value", "1"}},
		{0, "ok=true error=- conf=7 term=true asdus=C_SC_NA_1/7,M_SP_TB_1/11,C_SC_NA_1/10", []string{"command", "--type", "C_SC_NA_1", "--ioa", "500", "--value", "false", "--qualifier", "2"}},
		{0, "ok=true error=- conf=7,7 term=true asdus=C_DC_NA_1/7,C_DC_NA_1/7,M_DP_TB_1/3,C_DC_NA_1/10", []string{"command", "--type", "C_DC_NA_1", "--ioa", "501", "--value", "1", "--mode", "sbo"}},
		{0, "ok=true error=- conf=7 term=true asdus=C_RC_NA_1/7,M_ST_TB_1/11,C_RC_NA_1/10", []string{"command", "--type", "C_RC_NA_1", "--ioa", "505", "--value", "2"}},
		{0, "ok=true error=- conf=7 term=true asdus=C_SE_NA_1/7,M_ME_TD_1/11,C_SE_NA_1/10", []string{"command", "--type", "C_SE_NA_1", "--ioa", "503", "--value", "-12345"}},
		{0, "ok=true error=- conf=7,9 term=false asdus=C_SC_NA_1/7,C_SC_NA_1/9", []string{"command", "--type", "C_SC_NA_1", "--ioa", "510", "--value", "true", "--mode", "cancel"}},
		{0, "ok=true error=- conf=7 term=false asdus=C_SC_NA_1/7", []string{"command", "--type", "C_SC_NA_1", "--ioa", "510", "--value", "true", "--mode", "select"}},
		{0, "ok=true error=- conf= term=false asdus=", []string{"monitor", "--duration-ms", "200"}},
	}
	fieldSets := []struct {
		name  string
		modes []string
		flags []string
	}{
		{"defaults", []string{"unbalanced", "balanced"}, nil},
		{"2223", []string{"unbalanced", "balanced"}, []string{"--link-address-size", "2", "--cot-size", "2", "--ca-size", "2", "--ioa-size", "3", "--link-address", "300"}},
		{"1211", []string{"unbalanced"}, []string{"--link-address-size", "1", "--cot-size", "2", "--ca-size", "1", "--ioa-size", "2", "--link-address", "7"}},
		{"0112", []string{"balanced"}, []string{"--link-address-size", "0", "--link-address", "0"}},
		{"distinct", []string{"balanced"}, []string{"--link-address", "3", "--remote-link-address", "4"}},
	}
	for _, fs := range fieldSets {
		for _, mode := range fs.modes {
			t.Run(fs.name+"/"+mode, func(t *testing.T) {
				o := capture(t)
				link := append([]string{"--link-mode", mode}, fs.flags...)
				port := startServer(t, o, link...)
				for _, c := range cases {
					if fs.name != "defaults" && c.op[0] == "interrogate" && len(c.op) > 1 {
						continue // the common address cases are for one-octet fields
					}
					before := len(o.docs(t))
					argv := append(append([]string{"client"}, c.op...), "--host", "127.0.0.1", "--port", port,
						"--timeout-ms", "3000", "--originator-address", "9")
					argv = append(argv, link...)
					code := exitCode(run(argv))
					var res map[string]any
					for _, d := range o.docs(t)[before:] {
						if d["operation"] != nil {
							res = d
						}
					}
					if res == nil {
						t.Fatalf("%v: no result document", c.op)
					}
					if got := summary(res); code != c.exit || got != c.want {
						t.Errorf("%v: exit %d, %s\nwant exit %d, %s", c.op, code, got, c.exit, c.want)
					}
					if fs.name == "2223" && c.op[0] == "interrogate" {
						for _, a := range res["asdus"].([]any) {
							if a := a.(map[string]any); a["originator"] != 9.0 || a["commonAddress"] != 1.0 {
								t.Errorf("originator/common address of %v", a)
							}
						}
					}
				}
			})
		}
	}
}

func TestLinkStartupFailure(t *testing.T) {
	o := capture(t)
	port := startServer(t, o)
	code := exitCode(run([]string{"client", "interrogate", "--host", "127.0.0.1", "--port", port,
		"--link-address-size", "2", "--timeout-ms", "1500"}))
	res := lastResult(o.docs(t))
	if code != exitConnectFailed || res["connected"] != false || res["error"].(map[string]any)["code"] != "link-init-timeout" {
		t.Errorf("exit %d, %v", code, res)
	}
	code = exitCode(run([]string{"client", "interrogate", "--host", "127.0.0.1", "--port", "1", "--connect-timeout-ms", "500"}))
	if res := lastResult(o.docs(t)); code != exitConnectFailed || res["error"].(map[string]any)["code"] != "connect-failed" {
		t.Errorf("exit %d, %v", code, res)
	}
}

func TestServerEvents(t *testing.T) {
	o := capture(t)
	port := startServer(t, o)
	for _, op := range [][]string{
		{"interrogate"},
		{"command", "--type", "C_SC_NA_1", "--ioa", "500", "--value", "false"},
		{"command", "--type", "C_SC_NA_1", "--ioa", "999", "--value", "false"},
		{"clock-sync", "--time", "2026-10-09T15:35:12.345Z"},
		{"read", "--ioa", "300"},
	} {
		exitCode(run(append(append([]string{"client"}, op...), "--host", "127.0.0.1", "--port", port)))
	}
	time.Sleep(100 * time.Millisecond)
	want := []string{
		`"event":"ready"`,
		`"event":"connection-opened"`,
		`"accepted":true,"commonAddress":1,"event":"interrogation","qoi":20`,
		`"cot":6,"event":"command","ioa":500,"outcome":"executed","select":false,"type":"C_SC_NA_1","value":false`,
		`"cot":6,"event":"command","ioa":999,"outcome":"unknown-ioa","type":"C_SC_NA_1"`,
		`"event":"clock-sync","time":"2026-10-09T15:35:12.345Z"`,
		`"accepted":true,"event":"read","ioa":300`,
		`"event":"connection-closed"`,
	}
	outMu.Lock()
	all := o.buf.String()
	outMu.Unlock()
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("no event with %s in\n%s", w, all)
		}
	}
}

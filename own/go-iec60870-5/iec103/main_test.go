// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-iec60870-5/iec103"
)

// testdata/fixture.json is a copy of fixtures/iec103/baseline/fixture.json
// of the harness: the build context of the image has only this module.
func baseline(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// capture collects what emit writes while f runs.
type capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *capture) lines(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(c.buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not a JSON line: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func swapStdout(t *testing.T) *capture {
	t.Helper()
	c := &capture{}
	old := stdout
	stdout = c
	t.Cleanup(func() { stdout = old })
	return c
}

func TestCapabilities(t *testing.T) {
	c := capabilities()
	if c["standard"] != standard || c["adapter"] != adapter || c["transport"] != "tcp-stream" {
		t.Fatalf("capabilities: %v", c)
	}
	up := c["upstream"].(object)
	if up["name"] != "otfabric/go-iec60870-5" || up["license"] != "MIT" || up["independentEngine"] != false {
		t.Errorf("upstream: %v", up)
	}
	f := c["features"].(object)
	for _, name := range []string{"generalCommand", "identification", "measurands", "broadcastClockSync", "genericServices",
		"disturbanceList", "disturbanceData", "disturbanceDataServer", "unbalancedMode"} {
		if f[name] != true {
			t.Errorf("feature %s: %v", name, f[name])
		}
	}
	if f["balancedMode"] != false || f["multipleConnections"] != false {
		t.Errorf("features: %v", f)
	}
	raw, err := json.Marshal(c)
	if err != nil || bytes.Contains(raw, []byte("\n")) {
		t.Fatalf("encoding: %v", err)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, argv := range [][]string{
		{"frobnicate"},
		{},
		{"client"},
		{"client", "interrogate"},
		{"client", "frobnicate", "--host", "x"},
		{"client", "counter-interrogate", "--host", "x"},
		{"client", "read", "--host", "x", "--ioa", "1"},
		{"client", "interrogate", "--host", "x", "--frobnicate", "1"},
		{"client", "interrogate", "--host", "x", "--cot-size", "1"},
		{"client", "interrogate", "--host", "x", "--originator-address", "1"},
		{"client", "interrogate", "--host", "x", "--link-mode", "balanced"},
		{"client", "interrogate", "--host", "x", "--link-address", "255"},
		{"client", "interrogate", "--host", "x", "--scn", "256"},
		{"client", "interrogate", "--host", "x", "--scn"},
		{"client", "interrogate", "--host", "x", "--scn", "1", "--scn", "2"},
		{"client", "command", "--host", "x", "--fun", "160", "--inf", "16"},
		{"client", "command", "--host", "x", "--inf", "16", "--dco", "on"},
		{"client", "command", "--host", "x", "--fun", "160", "--dco", "on"},
		{"client", "command", "--host", "x", "--fun", "160", "--inf", "16", "--dco", "4"},
		{"client", "command", "--host", "x", "--fun", "160", "--inf", "16", "--dco", "on", "--type", "C_SC_NA_1"},
		{"client", "clock-sync", "--host", "x", "--time", "yesterday"},
		{"client", "generic-read", "--host", "x"},
		{"client", "generic-read", "--host", "x", "--gin", "1"},
		{"client", "generic-read", "--host", "x", "--gin", "1.256"},
		{"client", "generic-write", "--host", "x", "--gin", "1.2", "--type", "7", "--size", "4"},
		{"client", "generic-write", "--host", "x", "--gin", "1.2", "--type", "7", "--size", "2", "--value", "1"},
		{"client", "generic-write", "--host", "x", "--gin", "1.2", "--type", "3", "--size", "1", "--value", "256"},
		{"client", "generic-write", "--host", "x", "--gin", "1.2", "--type", "4", "--size", "1", "--value", "-129"},
		{"client", "generic-write", "--host", "x", "--gin", "1.2", "--type", "1", "--size", "3", "--value", "ab"},
		{"client", "generic-write", "--host", "x", "--gin", "1.2", "--type", "9", "--size", "1", "--value", "1"},
		{"client", "disturbance-transfer", "--host", "x"},
		{"client", "disturbance-list", "--host", "x", "--fun", "256"},
		{"client", "disturbance-list", "--host", "x", "--common-address", "0"},
		{"server", "--frobnicate"},
		{"server", "--k", "4"},
		{"server", "--link-mode", "balanced"},
		{"server", "--link-address", "255"},
		{"server", "--ioa-size", "2"},
		{"server", "--bind-address", "nowhere"},
		{"print-capabilities", "extra"},
	} {
		code, err := run(argv)
		var ue usageError
		if code != exitUsage || (err != nil && !errors.As(err, &ue)) {
			t.Errorf("%v: exit %d, %v", argv, code, err)
		}
	}
}

func TestParseClient(t *testing.T) {
	o, err := parseClient("command", &args{rest: strings.Fields("--host h --fun 160 --inf 16 --dco off --rii 7 --link-address 3 --common-address 3")})
	if err != nil || o.fun != 160 || o.inf != 16 || o.dco != 1 || o.rii != 7 || o.linkAddr != 3 || o.ca != 3 || o.port != defaultPort {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = parseClient("generic-write", &args{rest: strings.Fields("--host h --gin 1.2 --type 7 --size 4 --value 2.25")})
	if err != nil || o.gin != (iec103.GIN{Group: 1, Entry: 2}) || o.kod != 1 || o.value.Type != iec103.DataFloat32 {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = parseClient("generic-write", &args{rest: []string{"--host", "h", "--gin", "1.4", "--type", "1", "--size", "8", "--value", "FEEDER 2"}})
	if err != nil || o.value.Text() != "FEEDER 2" {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = parseClient("generic-write", &args{rest: strings.Fields("--host h --gin 1.5 --type 4 --size 2 --value -20")})
	if n, _ := o.value.Int(0); err != nil || n != -20 {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = parseClient("disturbance-transfer", &args{rest: strings.Fields("--host h --fan 7")})
	if err != nil || o.fan != 7 || o.svcFun != -1 {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = parseClient("clock-sync", &args{rest: strings.Fields("--host h --broadcast --time 2026-10-09T15:35:12.345Z")})
	if err != nil || !o.broadcast || o.time.Format(timeLayout) != "2026-10-09T15:35:12.345Z" {
		t.Fatalf("%+v %v", o, err)
	}
	for _, s := range []string{"0", "1", "2", "3", "on", "OFF"} {
		if _, err := parseDCO(s); err != nil {
			t.Errorf("--dco %s: %v", s, err)
		}
	}
}

func TestFixture(t *testing.T) {
	f, err := parseFixture(baseline(t))
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "baseline" || len(f.Generic) != 5 || len(f.Disturbances) != 1 || f.software != [4]byte{1, 2, 3, 4} {
		t.Fatalf("%+v", f)
	}
	rec := f.Disturbances[0].record(160)
	if rec.FAN != 7 || len(rec.Channels) != 2 || len(rec.Channels[0].Values) != 40 || rec.Start.UTC().Format("15:04:05.000") != "15:30:00.000" {
		t.Fatalf("%+v", rec)
	}
	if v := f.Generic[1].value; v.Type != iec103.DataFloat32 {
		t.Fatalf("%+v", v)
	}

	bad := []func(m map[string]any){
		func(m map[string]any) { m["schemaVersion"] = "2.0" },
		func(m map[string]any) { st(m)["commonAddress"] = 255 },
		func(m map[string]any) { ident(m)["ascii"] = "SHORT" },
		func(m map[string]any) { ident(m)["software"] = "0102" },
		func(m map[string]any) { ident(m)["fun"] = 0 },
		func(m map[string]any) {
			m["commands"] = append(m["commands"].([]any), map[string]any{"fun": 160, "inf": 99})
		},
		func(m map[string]any) {
			m["commands"] = append(m["commands"].([]any), map[string]any{"fun": 160, "inf": 16})
		},
		func(m map[string]any) {
			m["indications"] = append(m["indications"].([]any), map[string]any{"fun": 160, "inf": 16, "dpi": 1})
		},
		func(m map[string]any) { m["indications"].([]any)[0].(map[string]any)["dpi"] = 4 },
		func(m map[string]any) { meas(m)["values"].([]any)[0].(map[string]any)["value"] = 5000 },
		func(m map[string]any) { meas(m)["type"] = 4 },
		func(m map[string]any) { meas(m)["inf"] = 16 },
		func(m map[string]any) { gen(m)["type"] = 2 },
		func(m map[string]any) { gen(m)["value"] = 70000 },
		func(m map[string]any) { gen(m)["size"] = 3 },
		func(m map[string]any) { gen(m)["gin"] = map[string]any{"group": 1, "entry": 2} },
		func(m map[string]any) { m["generic"].([]any)[1].(map[string]any)["value"] = 0.1 },
		func(m map[string]any) { m["generic"].([]any)[3].(map[string]any)["value"] = "FEEDER" },
		func(m map[string]any) { dist(m)["start"] = "15:30" },
		func(m map[string]any) { dist(m)["time"] = "yesterday" },
		func(m map[string]any) {
			ch := dist(m)["channels"].([]any)
			ch[1].(map[string]any)["values"] = []any{1}
		},
		func(m map[string]any) { dist(m)["channels"].([]any)[1].(map[string]any)["acc"] = 1 },
		func(m map[string]any) { dist(m)["tags"].([]any)[3].(map[string]any)["pos"] = 1 },
		func(m map[string]any) { m["disturbances"] = append(m["disturbances"].([]any), dist(m)) },
	}
	for i, mutate := range bad {
		var m map[string]any
		if err := json.Unmarshal(baseline(t), &m); err != nil {
			t.Fatal(err)
		}
		mutate(m)
		raw, _ := json.Marshal(m)
		var fe fixtureError
		if _, err := parseFixture(raw); !errors.As(err, &fe) {
			t.Errorf("mutation %d: %v", i, err)
		}
	}
	if _, err := loadFixture("testdata/none.json"); err == nil {
		t.Error("missing fixture loaded")
	}
}

func st(m map[string]any) map[string]any    { return m["station"].(map[string]any) }
func ident(m map[string]any) map[string]any { return st(m)["identification"].(map[string]any) }
func meas(m map[string]any) map[string]any  { return m["measurands"].([]any)[0].(map[string]any) }
func gen(m map[string]any) map[string]any   { return m["generic"].([]any)[0].(map[string]any) }
func dist(m map[string]any) map[string]any  { return m["disturbances"].([]any)[0].(map[string]any) }

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestASDUDoc(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		// ASDU 1, cause 9, FUN 160 INF 16, DPI 2, 15:35:12.345 IV, SIN 5.
		{"01 81 09 01 A0 10 02 39 30 A3 0F 05",
			`{"commonAddress":1,"cot":9,"count":1,"negative":false,"objects":[{"dpi":2,"fun":160,"inf":16,"sin":5,"time32":"15:35:12.345","timeInvalid":true}],"originator":0,"sequence":true,"test":false,"type":"time-tagged-message","typeId":1}`},
		// ASDU 9 with two values: 100, and 4095 with OV.
		{"09 02 02 01 A0 94 20 03 F9 7F",
			`{"commonAddress":1,"cot":2,"count":2,"negative":false,"objects":[{"fun":160,"inf":148,"values":[{"er":false,"ov":false,"value":100},{"er":false,"ov":true,"value":4095}]}],"originator":0,"sequence":false,"test":false,"type":"measurands-ii","typeId":9}`},
		// ASDU 5: COL 2, "OTFAB103", 01020304.
		{"05 81 04 01 A0 03 02 4F 54 46 41 42 31 30 33 01 02 03 04",
			`{"commonAddress":1,"cot":4,"count":1,"negative":false,"objects":[{"ascii":"OTFAB103","col":2,"fun":160,"inf":3,"software":"01020304"}],"originator":0,"sequence":true,"test":false,"type":"identification","typeId":5}`},
		// ASDU 6, 2026-10-09T15:35:12.345Z.
		{"06 81 08 01 FF 00 39 30 23 0F 09 0A 1A",
			`{"commonAddress":1,"cot":8,"count":1,"negative":false,"objects":[{"fun":255,"inf":0,"time":"2026-10-09T15:35:12.345Z"}],"originator":0,"sequence":true,"test":false,"type":"time-synchronization","typeId":6}`},
		// ASDU 8, SCN 3.
		{"08 81 0A 01 FF 00 03",
			`{"commonAddress":1,"cot":10,"count":1,"negative":false,"objects":[{"fun":255,"inf":0,"scn":3}],"originator":0,"sequence":true,"test":false,"type":"general-interrogation-termination","typeId":8}`},
		// ASDU 20, DCO 2, RII 9.
		{"14 81 14 01 A0 11 02 09",
			`{"commonAddress":1,"cot":20,"count":1,"negative":false,"objects":[{"dco":2,"fun":160,"inf":17,"rii":9}],"originator":0,"sequence":true,"test":false,"type":"general-command","typeId":20}`},
		// ASDU 1 with the 12-octet elements of another stack: data.
		{"01 81 0B 01 A0 10 02 FF FF FF FF 00 00 00 00 00 00 00",
			`{"commonAddress":1,"cot":11,"count":1,"negative":false,"objects":[{"data":"02ffffffff00000000000000","fun":160,"inf":16}],"originator":0,"sequence":true,"test":false,"type":"time-tagged-message","typeId":1}`},
		// ASDU 10: data.
		{"0A 81 2A 01 FE F4 01 00 01 01 01 03 02 01 E8 03",
			`{"commonAddress":1,"cot":42,"count":1,"negative":false,"objects":[{"data":"01000101010302 01e803","fun":254,"inf":244}],"originator":0,"sequence":true,"test":false,"type":"generic-data","typeId":10}`},
		// A private type.
		{"C8 81 01 01 A0 10 01",
			`{"commonAddress":1,"cot":1,"count":1,"negative":false,"objects":[{"data":"01","fun":160,"inf":16}],"originator":0,"sequence":true,"test":false,"type":null,"typeId":200}`},
	} {
		got, _ := json.Marshal(asduDoc(mustHex(t, tc.raw)))
		want := strings.ReplaceAll(tc.want, " ", "")
		if string(got) != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.raw, got, want)
		}
	}
	if d := asduDoc([]byte{1}); d["typeId"] != 0 {
		t.Errorf("short: %v", d)
	}
}

func TestSetDoc(t *testing.T) {
	for _, tc := range []struct {
		v    iec103.Value
		want string
	}{
		{iec103.Uints(iec103.DataUint, 2, 1000), `{"gin":"1.1","kod":1,"number":1,"size":2,"type":3,"values":[1000]}`},
		{iec103.Ints(2, -20), `{"gin":"1.1","kod":1,"number":1,"size":2,"type":4,"values":[-20]}`},
		{iec103.Float32s(1.5), `{"gin":"1.1","kod":1,"number":1,"size":4,"type":7,"values":[1.5]}`},
		{iec103.ASCII("FEEDER 1"), `{"gin":"1.1","kod":1,"number":1,"size":8,"type":1,"values":["FEEDER 1"]}`},
		{iec103.Reply(iec103.GRCReadOnly), `{"gin":"1.1","kod":1,"number":1,"size":1,"type":22,"values":[8]}`},
		{iec103.Value{GDD: iec103.GDD{Type: iec103.DataDPI, Size: 1, Number: 1}, Data: []byte{2}}, `{"data":"02","gin":"1.1","kod":1,"number":1,"size":1,"type":9}`},
	} {
		got, _ := json.Marshal(setDoc(iec103.DataSet{GIN: iec103.GIN{Group: 1, Entry: 1}, KOD: 1, Value: tc.v}))
		if string(got) != tc.want {
			t.Errorf("got %s, want %s", got, tc.want)
		}
	}
}

func TestPrintFixture(t *testing.T) {
	c := swapStdout(t)
	old := fixtureRoot
	fixtureRoot = t.TempDir()
	t.Cleanup(func() { fixtureRoot = old })
	if err := os.MkdirAll(fixtureRoot+"/baseline", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixtureRoot+"/baseline/fixture.json", baseline(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := printFixture("baseline"); code != exitOK || !bytes.Equal(c.buf.Bytes(), baseline(t)) {
		t.Errorf("print-fixture baseline: exit %d", code)
	}
	if code := printFixture("nothing"); code != exitOperationFailed {
		t.Errorf("unknown fixture: exit %d", code)
	}
	if code := printFixture("../etc"); code != exitUsage {
		t.Errorf("path: exit %d", code)
	}
}

// startStation serves the baseline fixture on a loopback port.
func startStation(t *testing.T) (*station, int) {
	t.Helper()
	f, err := parseFixture(baseline(t))
	if err != nil {
		t.Fatal(err)
	}
	st := newStation(f, 1, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); st.serve(ln) }()
	t.Cleanup(func() { _ = ln.Close(); st.stop(); <-done })
	return st, ln.Addr().(*net.TCPAddr).Port
}

// op runs one client operation against port and returns its document and
// exit code.
func op(t *testing.T, port int, cmdline string) (map[string]any, int) {
	t.Helper()
	argv := append(strings.Fields(cmdline), "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	o, err := parseClient(argv[0], &args{rest: argv[1:]})
	if err != nil {
		t.Fatalf("%s: %v", cmdline, err)
	}
	r := &result{op: o.op, start: time.Now()}
	code := execute(r, o)
	raw, _ := json.Marshal(r.document())
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	return doc, code
}

// summary is SUMMARY_103 of the harness's scripts/lib.sh.
func summary(doc map[string]any) string {
	conf := []string{}
	for _, c := range doc["confirmations"].([]any) {
		m := c.(map[string]any)
		s := strconv.Itoa(int(m["cot"].(float64)))
		if m["negative"] == true {
			s += "n"
		}
		conf = append(conf, s)
	}
	svc := strings.HasPrefix(doc["operation"].(string), "generic-") || strings.HasPrefix(doc["operation"].(string), "disturbance-")
	asdus := []string{}
	for _, a := range doc["asdus"].([]any) {
		m := a.(map[string]any)
		if m["typeId"].(float64) == 5 || m["cot"].(float64) == 2 || svc {
			continue
		}
		asdus = append(asdus, m["type"].(string)+"/"+strconv.Itoa(int(m["cot"].(float64))))
	}
	code := "-"
	if e, ok := doc["error"].(map[string]any); ok {
		code = e["code"].(string)
	}
	return "ok=" + strconv.FormatBool(doc["ok"].(bool)) + " error=" + code + " conf=" + strings.Join(conf, ",") +
		" term=" + strconv.FormatBool(doc["terminated"].(bool)) + " asdus=" + strings.Join(asdus, ",")
}

// TestSelf runs the cases of the harness's tests/iec103/cases.tsv with
// this client against this server.
func TestSelf(t *testing.T) {
	events := swapStdout(t)
	_, port := startStation(t)
	gi := strings.Repeat("time-tagged-message/9,", 8) + "general-interrogation-termination/10"
	for _, tc := range []struct {
		cmd, want string
		exit      int
	}{
		{"connect", "ok=true error=- conf= term=false asdus=", 0},
		{"interrogate", "ok=true error=- conf= term=true asdus=" + gi, 0},
		{"interrogate --scn 200", "ok=true error=- conf= term=true asdus=" + gi, 0},
		{"clock-sync --time 2026-10-09T15:35:12.345Z", "ok=true error=- conf=8 term=false asdus=time-synchronization/8", 0},
		{"monitor --duration-ms 500", "ok=true error=- conf= term=false asdus=", 0},
		{"command --fun 160 --inf 18 --dco on --rii 4 --collect-ms 500", "ok=false error=negative-confirmation conf=21n term=false asdus=time-tagged-message/21", 1},
		{"command --fun 160 --inf 16 --dco 3 --rii 6 --collect-ms 500", "ok=false error=negative-confirmation conf=21n term=false asdus=time-tagged-message/21", 1},
		{"command --fun 160 --inf 16 --dco off --rii 42 --collect-ms 500", "ok=true error=- conf=20 term=false asdus=time-tagged-message/20,time-tagged-message/12", 0},
		{"command --fun 160 --inf 19 --dco on --rii 77 --collect-ms 500", "ok=true error=- conf=20 term=false asdus=time-tagged-message/20,time-tagged-message/12", 0},
		{"generic-read --gin 1.3", "ok=true error=- conf=42 term=false asdus=", 0},
		{"generic-read --gin 1.4 --kod 10", "ok=true error=- conf=42 term=false asdus=", 0},
		{"generic-read --gin 2.1", "ok=false error=negative-confirmation conf=43n term=false asdus=", 1},
		{"generic-write --gin 1.3 --type 3 --size 2 --value 450", "ok=true error=- conf=40 term=false asdus=", 0},
		{"generic-write --gin 1.1 --type 3 --size 2 --value 5", "ok=false error=negative-confirmation conf=41n term=false asdus=", 1},
		{"disturbance-list", "ok=true error=- conf= term=false asdus=", 0},
		{"disturbance-transfer --fan 7", "ok=true error=- conf= term=false asdus=", 0},
		{"disturbance-transfer --fan 8", "ok=false error=failed conf= term=false asdus=", 1},
		{"clock-sync --broadcast --time 2026-10-09T15:35:13.000Z", "ok=true error=- conf= term=false asdus=", 0},
	} {
		doc, code := op(t, port, tc.cmd)
		if got := summary(doc); got != tc.want || code != tc.exit {
			t.Errorf("%s:\n got exit %d %s\nwant exit %d %s", tc.cmd, code, got, tc.exit, tc.want)
		}
	}

	// Values.
	doc, _ := op(t, port, "generic-read --gin 1.3")
	if v := doc["generic"].(map[string]any)["sets"].([]any)[0].(map[string]any)["values"]; v.([]any)[0] != 450.0 {
		t.Errorf("1.3 after the write: %v", v)
	}
	doc, _ = op(t, port, "disturbance-list")
	if l, _ := json.Marshal(doc["disturbances"]); string(l) != `[{"fan":7,"sof":{"otev":false,"test":false,"tm":false,"tp":true},"time":"2026-10-09T15:30:00.012Z"}]` {
		t.Errorf("list %s", l)
	}
	doc, _ = op(t, port, "disturbance-transfer --fan 7")
	d := doc["disturbance"].(map[string]any)
	if d["noc"] != 2.0 || d["noe"] != 40.0 || d["interval"] != 1000.0 || d["start"] != "15:30:00.000" || len(d["tags"].([]any)) != 4 {
		t.Errorf("disturbance %v", d)
	}
	doc, _ = op(t, port, "connect --hold-ms 300")
	var id []any
	for _, a := range doc["asdus"].([]any) {
		if m := a.(map[string]any); m["typeId"] == 5.0 && m["cot"] == 4.0 {
			id = append(id, m["objects"])
		}
	}
	if raw, _ := json.Marshal(id); string(raw) != `[[{"ascii":"OTFAB103","col":2,"fun":160,"inf":3,"software":"01020304"}]]` {
		t.Errorf("identification %s", raw)
	}
	doc, _ = op(t, port, "monitor --duration-ms 1000")
	seen := map[float64]bool{}
	for _, a := range doc["asdus"].([]any) {
		if m := a.(map[string]any); m["cot"] == 2.0 {
			seen[m["objects"].([]any)[0].(map[string]any)["inf"].(float64)] = true
		}
	}
	if !seen[144] || !seen[145] || !seen[148] {
		t.Errorf("measurands %v", seen)
	}

	// A station that does not answer, and none at all.
	doc, code := op(t, port, "interrogate --link-address 9 --timeout-ms 1200")
	if code != exitConnectFailed || doc["error"].(map[string]any)["code"] != "link-init-timeout" || doc["connected"] != false {
		t.Errorf("other link address: exit %d %v", code, doc)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	free := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	doc, code = op(t, free, "connect --connect-timeout-ms 500")
	if code != exitConnectFailed || doc["error"].(map[string]any)["code"] != "connect-failed" {
		t.Errorf("no station: exit %d %v", code, doc)
	}

	time.Sleep(2 * drainGrace)
	want := map[string]bool{}
	for _, e := range events.lines(t) {
		switch e["event"] {
		case "interrogation":
			want["interrogation"] = want["interrogation"] || e["scn"] == 200.0
		case "clock-sync":
			want["clock-sync"] = want["clock-sync"] || (e["time"] == "2026-10-09T15:35:12.345Z" && e["broadcast"] == false)
			want["broadcast"] = want["broadcast"] || (e["time"] == "2026-10-09T15:35:13.000Z" && e["broadcast"] == true)
		case "command":
			want["executed"] = want["executed"] || (e["inf"] == 16.0 && e["dco"] == 1.0 && e["rii"] == 42.0 && e["outcome"] == "executed")
			want["rejected"] = want["rejected"] || (e["inf"] == 16.0 && e["dco"] == 3.0 && e["outcome"] == "rejected")
		}
	}
	for _, k := range []string{"interrogation", "clock-sync", "broadcast", "executed", "rejected"} {
		if !want[k] {
			t.Errorf("no %s event", k)
		}
	}
}

// TestOneLine: a second connection is closed while one is served.
func TestOneLine(t *testing.T) {
	swapStdout(t)
	_, port := startStation(t)
	held := make(chan struct{})
	go func() {
		defer close(held)
		op(t, port, "connect --hold-ms 1500")
	}()
	time.Sleep(700 * time.Millisecond)
	doc, code := op(t, port, "interrogate --timeout-ms 800")
	<-held
	if code == exitOK || doc["connected"] != false {
		t.Errorf("second connection: exit %d %v", code, doc)
	}
}

func TestRunServerFixtureErrors(t *testing.T) {
	swapStdout(t)
	if code, err := run([]string{"server", "--fixture", "testdata/none.json", "--port", "0"}); code != exitFixture || err != nil {
		t.Errorf("missing fixture: %d %v", code, err)
	}
	if code, err := run([]string{"server", "--fixture", "testdata/fixture.json", "--common-address", "0", "--port", "0"}); code != exitUsage || err == nil {
		t.Errorf("common address 0: %d %v", code, err)
	}
}

func TestRunServerBusyPort(t *testing.T) {
	swapStdout(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	code, _ := run([]string{"server", "--fixture", "testdata/fixture.json", "--bind-address", "127.0.0.1", "--port", port,
		"--ready-file", t.TempDir() + "/ready"})
	if code != exitOperationFailed {
		t.Errorf("busy port: exit %d", code)
	}
}

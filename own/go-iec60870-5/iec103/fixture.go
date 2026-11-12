// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/iec103"
)

// fixture is an IEC 103 fixture (docs/FIXTURES.md, "IEC 103").
type fixture struct {
	SchemaVersion string `json:"schemaVersion"`
	Name          string `json:"name"`
	Station       struct {
		CommonAddress  *int `json:"commonAddress"`
		Identification *struct {
			Fun      int    `json:"fun"`
			Col      int    `json:"col"`
			ASCII    string `json:"ascii"`
			Software string `json:"software"`
		} `json:"identification"`
	} `json:"station"`
	Indications  []indication   `json:"indications"`
	Measurands   []measurand    `json:"measurands"`
	Commands     []funInf       `json:"commands"`
	Generic      []genericEntry `json:"generic"`
	Disturbances []disturbance  `json:"disturbances"`

	software [4]byte
}

type indication struct {
	Fun int   `json:"fun"`
	Inf int   `json:"inf"`
	DPI int   `json:"dpi"`
	GI  *bool `json:"gi"`
}

func (i indication) inGI() bool { return i.GI == nil || *i.GI }

type funInf struct {
	Fun int `json:"fun"`
	Inf int `json:"inf"`
}

type measurand struct {
	Fun    int `json:"fun"`
	Inf    int `json:"inf"`
	Type   int `json:"type"`
	Values []struct {
		Value int  `json:"value"`
		OV    bool `json:"ov"`
		ER    bool `json:"er"`
	} `json:"values"`
}

type genericEntry struct {
	GIN struct {
		Group int `json:"group"`
		Entry int `json:"entry"`
	} `json:"gin"`
	Description string          `json:"description"`
	Type        int             `json:"type"`
	Size        int             `json:"size"`
	Value       json.RawMessage `json:"value"`
	Writable    bool            `json:"writable"`

	value iec103.Value // the GID of Value, as the data type and size lay it out
}

func (g genericEntry) gin() iec103.GIN {
	return iec103.GIN{Group: uint8(g.GIN.Group), Entry: uint8(g.GIN.Entry)}
}

type disturbance struct {
	FAN int `json:"fan"`
	SOF struct {
		TP   bool `json:"tp"`
		TM   bool `json:"tm"`
		Test bool `json:"test"`
		OTEV bool `json:"otev"`
	} `json:"sof"`
	Time     string `json:"time"`
	NOF      int    `json:"nof"`
	Interval int    `json:"interval"`
	Start    string `json:"start"`
	Tags     []struct {
		Pos *int `json:"pos"`
		Fun int  `json:"fun"`
		Inf int  `json:"inf"`
		DPI int  `json:"dpi"`
	} `json:"tags"`
	Channels []struct {
		ACC    int     `json:"acc"`
		RPV    float64 `json:"rpv"`
		RSV    float64 `json:"rsv"`
		RFA    float64 `json:"rfa"`
		Values []int   `json:"values"`
	} `json:"channels"`

	time  time.Time // the fault
	start time.Time // the first value: Start on the date of the fault
}

const timeLayout = "2006-01-02T15:04:05.000Z"

type fixtureError struct{ msg string }

func (e fixtureError) Error() string { return e.msg }

func fixturef(format string, a ...any) error { return fixtureError{fmt.Sprintf(format, a...)} }

func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func inRange(what string, n, lo, hi int) error {
	if n < lo || n > hi {
		return fixturef("%s: %d is not %d..%d", what, n, lo, hi)
	}
	return nil
}

// loadFixture reads and validates the fixture at path with the rules of
// docs/FIXTURES.md; any violation is a fixtureError (exit 4).
func loadFixture(path string) (*fixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fixturef("cannot read fixture %s: %v", path, err)
	}
	return parseFixture(raw)
}

func parseFixture(raw []byte) (*fixture, error) {
	var f fixture
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&f); err != nil {
		return nil, fixturef("invalid fixture: %v", err)
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *fixture) validate() error {
	if f.SchemaVersion != "1.0" {
		return fixturef("schemaVersion must be 1.0, got %q", f.SchemaVersion)
	}
	if f.Station.CommonAddress == nil {
		return fixturef("station.commonAddress is required")
	}
	if err := inRange("station.commonAddress", *f.Station.CommonAddress, 0, 254); err != nil {
		return err
	}
	id := f.Station.Identification
	if id == nil {
		return fixturef("station.identification is required")
	}
	if err := inRange("identification.fun", id.Fun, 1, 255); err != nil {
		return err
	}
	if err := inRange("identification.col", id.Col, 0, 255); err != nil {
		return err
	}
	if len(id.ASCII) != 8 || !printable(id.ASCII) {
		return fixturef("identification.ascii must be eight printable ASCII characters, got %q", id.ASCII)
	}
	sw, err := hex.DecodeString(id.Software)
	if err != nil || len(sw) != 4 {
		return fixturef("identification.software must be four octets in hex, got %q", id.Software)
	}
	copy(f.software[:], sw)

	seen := map[funInf]string{}
	if len(f.Indications) == 0 {
		return fixturef("indications: at least one is required")
	}
	for _, i := range f.Indications {
		if err := inRange("indication fun", i.Fun, 0, 254); err != nil {
			return err
		}
		if err := inRange("indication inf", i.Inf, 0, 255); err != nil {
			return err
		}
		if err := inRange(fmt.Sprintf("indication %d/%d dpi", i.Fun, i.Inf), i.DPI, 0, 3); err != nil {
			return err
		}
		k := funInf{i.Fun, i.Inf}
		if _, dup := seen[k]; dup {
			return fixturef("indication %d/%d: duplicate", i.Fun, i.Inf)
		}
		seen[k] = "indication"
	}
	for _, m := range f.Measurands {
		if err := inRange("measurand fun", m.Fun, 0, 254); err != nil {
			return err
		}
		if err := inRange("measurand inf", m.Inf, 0, 255); err != nil {
			return err
		}
		k := funInf{m.Fun, m.Inf}
		if _, dup := seen[k]; dup {
			return fixturef("measurand %d/%d: function type and information number already used", m.Fun, m.Inf)
		}
		seen[k] = "measurand"
		limit := 0
		switch m.Type {
		case 3:
			limit = iec103.MaxMeasurands1
		case 9:
			limit = iec103.MaxMeasurands2
		default:
			return fixturef("measurand %d/%d: type %d is not 3 or 9", m.Fun, m.Inf, m.Type)
		}
		if len(m.Values) < 1 || len(m.Values) > limit {
			return fixturef("measurand %d/%d: %d values, type %d takes 1..%d", m.Fun, m.Inf, len(m.Values), m.Type, limit)
		}
		for _, v := range m.Values {
			if err := inRange(fmt.Sprintf("measurand %d/%d value", m.Fun, m.Inf), v.Value, -4096, 4095); err != nil {
				return err
			}
		}
	}
	cmds := map[funInf]bool{}
	for _, c := range f.Commands {
		if seen[c] != "indication" {
			return fixturef("command %d/%d: no indication with this function type and information number", c.Fun, c.Inf)
		}
		if cmds[c] {
			return fixturef("command %d/%d: duplicate", c.Fun, c.Inf)
		}
		cmds[c] = true
	}
	gins := map[iec103.GIN]bool{}
	for i := range f.Generic {
		g := &f.Generic[i]
		if err := inRange("generic gin.group", g.GIN.Group, 1, 255); err != nil {
			return err
		}
		if err := inRange("generic gin.entry", g.GIN.Entry, 1, 255); err != nil {
			return err
		}
		if gins[g.gin()] {
			return fixturef("generic %d.%d: duplicate", g.GIN.Group, g.GIN.Entry)
		}
		gins[g.gin()] = true
		if g.Description == "" || len(g.Description) > 255 || !printable(g.Description) {
			return fixturef("generic %d.%d: the description must be 1..255 printable ASCII characters", g.GIN.Group, g.GIN.Entry)
		}
		v, err := genericValue(g.Type, g.Size, g.Value)
		if err != nil {
			return fixturef("generic %d.%d: %v", g.GIN.Group, g.GIN.Entry, err)
		}
		g.value = v
	}
	if len(f.Disturbances) > iec103.MaxDisturbances {
		return fixturef("disturbances: %d, at most %d", len(f.Disturbances), iec103.MaxDisturbances)
	}
	fans := map[int]bool{}
	for i := range f.Disturbances {
		if err := f.Disturbances[i].validate(); err != nil {
			return err
		}
		if fans[f.Disturbances[i].FAN] {
			return fixturef("disturbance FAN %d: duplicate", f.Disturbances[i].FAN)
		}
		fans[f.Disturbances[i].FAN] = true
	}
	return nil
}

// genericValue is the GID of a generic value of the given data type and
// size: 1 ASCII (size the number of characters), 3 unsigned and 4 signed
// integers (size 1, 2 or 4), 7 R32.23 (size 4, a number exact in single
// precision).
func genericValue(typ, size int, raw json.RawMessage) (iec103.Value, error) {
	switch typ {
	case 1:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return iec103.Value{}, fmt.Errorf("type 1 takes a string: %v", err)
		}
		if len(s) != size || size < 1 || size > 255 || !printable(s) {
			return iec103.Value{}, fmt.Errorf("type 1: %q is not %d printable ASCII characters", s, size)
		}
		return iec103.ASCII(s), nil
	case 3, 4:
		if size != 1 && size != 2 && size != 4 {
			return iec103.Value{}, fmt.Errorf("type %d: size %d is not 1, 2 or 4", typ, size)
		}
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil {
			return iec103.Value{}, fmt.Errorf("type %d takes an integer: %v", typ, err)
		}
		return intValue(typ, size, n)
	case 7:
		if size != 4 {
			return iec103.Value{}, fmt.Errorf("type 7: size %d is not 4", size)
		}
		var x float64
		if err := json.Unmarshal(raw, &x); err != nil {
			return iec103.Value{}, fmt.Errorf("type 7 takes a number: %v", err)
		}
		if float64(float32(x)) != x {
			return iec103.Value{}, fmt.Errorf("type 7: %v is not exact in single precision", x)
		}
		return iec103.Float32s(float32(x)), nil
	}
	return iec103.Value{}, fmt.Errorf("data type %d is not 1, 3, 4 or 7", typ)
}

// intValue is an integer of size octets: unsigned (type 3) or two's
// complement (type 4), refused when it does not fit.
func intValue(typ, size int, n int64) (iec103.Value, error) {
	bits := uint(8 * size)
	if typ == 3 {
		if n < 0 || n >= int64(1)<<bits {
			return iec103.Value{}, fmt.Errorf("%d does not fit an unsigned integer of %d octets", n, size)
		}
		return iec103.Uints(iec103.DataUint, size, uint64(n)), nil
	}
	if n < -(int64(1)<<(bits-1)) || n >= int64(1)<<(bits-1) {
		return iec103.Value{}, fmt.Errorf("%d does not fit an integer of %d octets", n, size)
	}
	return iec103.Ints(size, n), nil
}

func (d *disturbance) validate() error {
	what := fmt.Sprintf("disturbance FAN %d", d.FAN)
	if err := inRange(what+" fan", d.FAN, 0, math.MaxUint16); err != nil {
		return err
	}
	t, err := time.Parse(timeLayout, d.Time)
	if err != nil {
		return fixturef("%s: time %q is not like 2026-01-02T03:04:05.678Z", what, d.Time)
	}
	d.time = t
	tod, err := time.Parse("15:04:05.000", d.Start)
	if err != nil {
		return fixturef("%s: start %q is not HH:MM:SS.mmm", what, d.Start)
	}
	day := t.Truncate(24 * time.Hour)
	d.start = day.Add(time.Duration(tod.Hour())*time.Hour + time.Duration(tod.Minute())*time.Minute +
		time.Duration(tod.Second())*time.Second + time.Duration(tod.Nanosecond()))
	if err := inRange(what+" nof", d.NOF, 0, math.MaxUint16); err != nil {
		return err
	}
	if err := inRange(what+" interval", d.Interval, 0, math.MaxUint16); err != nil {
		return err
	}
	last := 0
	for i, tg := range d.Tags {
		if tg.Pos == nil {
			return fixturef("%s: tag %d has no pos", what, i)
		}
		if err := inRange(what+" tag pos", *tg.Pos, 0, math.MaxUint16); err != nil {
			return err
		}
		if *tg.Pos < last {
			return fixturef("%s: tags are not in ascending position", what)
		}
		last = *tg.Pos
		if err := inRange(what+" tag fun", tg.Fun, 0, 254); err != nil {
			return err
		}
		if err := inRange(what+" tag inf", tg.Inf, 0, 255); err != nil {
			return err
		}
		if err := inRange(what+" tag dpi", tg.DPI, 0, 3); err != nil {
			return err
		}
	}
	if len(d.Channels) > math.MaxUint8 {
		return fixturef("%s: %d channels, NOC is one octet", what, len(d.Channels))
	}
	lastACC := -1
	for _, c := range d.Channels {
		if err := inRange(what+" acc", c.ACC, 1, 255); err != nil {
			return err
		}
		if c.ACC <= lastACC {
			return fixturef("%s: channels are not in ascending ACC", what)
		}
		lastACC = c.ACC
		if len(c.Values) != len(d.Channels[0].Values) {
			return fixturef("%s: channel ACC %d has %d values, the first %d", what, c.ACC, len(c.Values), len(d.Channels[0].Values))
		}
		if len(c.Values) > math.MaxUint16 {
			return fixturef("%s: %d values, NOE is two octets", what, len(c.Values))
		}
		for _, v := range c.Values {
			if err := inRange(what+" value", v, math.MinInt16, math.MaxInt16); err != nil {
				return err
			}
		}
		for _, x := range []float64{c.RPV, c.RSV, c.RFA} {
			if math.IsNaN(x) || math.IsInf(float64(float32(x)), 0) {
				return fixturef("%s: channel ACC %d has a rating that is no single precision number", what, c.ACC)
			}
		}
	}
	return nil
}

// sof is the status of fault of the record (clause 7.2.6.24).
func (d *disturbance) sof() iec103.SOF {
	var s iec103.SOF
	if d.SOF.TP {
		s |= iec103.SOFTrip
	}
	if d.SOF.TM {
		s |= iec103.SOFTM
	}
	if d.SOF.Test {
		s |= iec103.SOFTest
	}
	if d.SOF.OTEV {
		s |= iec103.SOFOTEV
	}
	return s
}

// record is the record as the transfer of clause 7.4.7 carries it, with
// the function type of the device.
func (d *disturbance) record(fun iec103.FunctionType) *iec103.Disturbance {
	rec := &iec103.Disturbance{
		Fun: fun, TOV: iec103.TOVInstantaneous, FAN: uint16(d.FAN), NOF: uint16(d.NOF),
		Interval: uint16(d.Interval), Start: cp.At(d.start),
	}
	for _, t := range d.Tags {
		rec.Tags = append(rec.Tags, iec103.TagChange{Pos: uint16(*t.Pos),
			Tag: iec103.Tag{Fun: iec103.FunctionType(t.Fun), Inf: iec103.InfoNumber(t.Inf), DPI: iec103.DPI(t.DPI)}})
	}
	for _, c := range d.Channels {
		ch := iec103.Channel{ACC: uint8(c.ACC), RPV: float32(c.RPV), RSV: float32(c.RSV), RFA: float32(c.RFA)}
		for _, v := range c.Values {
			ch.Values = append(ch.Values, int16(v))
		}
		rec.Channels = append(rec.Channels, ch)
	}
	return rec
}

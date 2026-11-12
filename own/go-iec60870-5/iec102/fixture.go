// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"
)

// fixture is an IEC 102 fixture (docs/FIXTURES.md, "IEC 102"; schema
// fixtures/schema/iec60870-5-102-fixture.schema.json).
type fixture struct {
	SchemaVersion string `json:"schemaVersion"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Station       struct {
		CommonAddress uint16 `json:"commonAddress"`
		AccessKey     uint32 `json:"accessKey"`
		Clock         string `json:"clock"`
		Product       struct {
			Standard     uint8  `json:"standard"`
			Manufacturer uint8  `json:"manufacturer"`
			Product      uint32 `json:"product"`
		} `json:"product"`
	} `json:"station"`
	Totals  []totalsList `json:"totals"`
	Tariffs []tariffList `json:"tariffs"`
}

type totalsList struct {
	Record  uint8    `json:"record"`
	Kind    string   `json:"kind"`
	Periods []period `json:"periods"`
}

type period struct {
	End    string  `json:"end"`
	Totals []total `json:"totals"`
}

type total struct {
	Address uint8  `json:"address"`
	Value   uint32 `json:"value"`
	Quality uint8  `json:"quality"`
}

type tariffList struct {
	Register uint8          `json:"register"`
	Current  []tariffRecord `json:"current"`
	Stored   []tariffRecord `json:"stored"`
}

// tariffRecord is a record of M_TA_VC_2 / M_TA_VM_2 as the fixture and the
// ASDU documents write it.
type tariffRecord struct {
	Address            uint8  `json:"address"`
	ActiveAbs          uint32 `json:"activeAbs"`
	ActiveInc          uint32 `json:"activeInc"`
	ActiveQuality      uint8  `json:"activeQuality"`
	ReactiveIndAbs     uint32 `json:"reactiveIndAbs"`
	ReactiveIndInc     uint32 `json:"reactiveIndInc"`
	ReactiveIndQuality uint8  `json:"reactiveIndQuality"`
	ReactiveCapAbs     uint32 `json:"reactiveCapAbs"`
	ReactiveCapInc     uint32 `json:"reactiveCapInc"`
	ReactiveCapQuality uint8  `json:"reactiveCapQuality"`
	MaxPower           uint32 `json:"maxPower"`
	MaxPowerTime       string `json:"maxPowerTime"`
	MaxPowerQuality    uint8  `json:"maxPowerQuality"`
	ExcessPower        uint32 `json:"excessPower"`
	ExcessPowerQuality uint8  `json:"excessPowerQuality"`
	PeriodStart        string `json:"periodStart"`
	PeriodEnd          string `json:"periodEnd"`
}

// Required members, per object of the schema.
var (
	reqRoot    = []string{"schemaVersion", "name", "station", "totals"}
	reqStation = []string{"commonAddress", "accessKey", "clock", "product"}
	reqProduct = []string{"standard", "manufacturer", "product"}
	reqList    = []string{"record", "kind", "periods"}
	reqPeriod  = []string{"end", "totals"}
	reqTotal   = []string{"address", "value", "quality"}
	reqTariffs = []string{"register", "current", "stored"}
	reqRecord  = []string{"address", "activeAbs", "activeInc", "activeQuality", "reactiveIndAbs", "reactiveIndInc",
		"reactiveIndQuality", "reactiveCapAbs", "reactiveCapInc", "reactiveCapQuality", "maxPower",
		"maxPowerTime", "maxPowerQuality", "excessPower", "excessPowerQuality", "periodStart", "periodEnd"}
)

// loadFixture reads and validates a fixture: the schema (members, types and
// ranges) and the rules make validate adds (docs/FIXTURES.md, "IEC 102").
func loadFixture(path string) (*fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		return nil, err
	}
	if err := checkRequired(generic); err != nil {
		return nil, err
	}
	var f fixture
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, err
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func has(v any, keys []string, where string) error {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: not an object", where)
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("%s: %s is missing", where, k)
		}
	}
	return nil
}

func items(v any, key string) []any {
	m, _ := v.(map[string]any)
	a, _ := m[key].([]any)
	return a
}

func checkRequired(root map[string]any) error {
	errs := []error{has(root, reqRoot, "fixture")}
	if st, ok := root["station"]; ok {
		errs = append(errs, has(st, reqStation, "station"))
		if m, ok := st.(map[string]any); ok {
			if p, ok := m["product"]; ok {
				errs = append(errs, has(p, reqProduct, "station.product"))
			}
		}
	}
	for i, l := range items(root, "totals") {
		errs = append(errs, has(l, reqList, fmt.Sprintf("totals[%d]", i)))
		for j, p := range items(l, "periods") {
			errs = append(errs, has(p, reqPeriod, fmt.Sprintf("totals[%d].periods[%d]", i, j)))
			for k, t := range items(p, "totals") {
				errs = append(errs, has(t, reqTotal, fmt.Sprintf("totals[%d].periods[%d].totals[%d]", i, j, k)))
			}
		}
	}
	for i, t := range items(root, "tariffs") {
		errs = append(errs, has(t, reqTariffs, fmt.Sprintf("tariffs[%d]", i)))
		for _, key := range []string{"current", "stored"} {
			for j, r := range items(t, key) {
				errs = append(errs, has(r, reqRecord, fmt.Sprintf("tariffs[%d].%s[%d]", i, key, j)))
			}
		}
	}
	return errors.Join(errs...)
}

func (f *fixture) validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	timeA := func(where, s string) {
		if _, err := localTime(where, s, timeAText, layoutA); err != nil {
			bad("%v", err)
		}
	}
	if f.SchemaVersion != "1.0" {
		bad("schemaVersion %q, want \"1.0\"", f.SchemaVersion)
	}
	if !fixtureName.MatchString(f.Name) {
		bad("name %q", f.Name)
	}
	if f.Station.CommonAddress > 65534 {
		bad("station.commonAddress %d is beyond 65534", f.Station.CommonAddress)
	}
	if _, err := localTime("station.clock", f.Station.Clock, timeBText, layoutB); err != nil {
		bad("%v", err)
	}
	if len(f.Totals) == 0 {
		bad("totals: at least one list")
	}
	type key struct {
		record uint8
		kind   string
	}
	lists := map[key]bool{}
	for _, l := range f.Totals {
		where := fmt.Sprintf("totals %d/%s", l.Record, l.Kind)
		if l.Kind != "absolute" && l.Kind != "incremental" {
			bad("%s: kind must be absolute or incremental", where)
		}
		k := key{l.Record, l.Kind}
		if lists[k] {
			bad("%s: a second list of the same record address and kind", where)
		}
		lists[k] = true
		if len(l.Periods) == 0 {
			bad("%s: at least one period", where)
		}
		for i, p := range l.Periods {
			timeA(where+" end", p.End)
			if i > 0 && p.End <= l.Periods[i-1].End {
				bad("%s: period ends are not strictly ascending", where)
			}
			if len(p.Totals) == 0 || len(p.Totals) > 40 {
				bad("%s period %s: 1..40 totals", where, p.End)
			}
			var addrs []uint8
			for _, t := range p.Totals {
				if t.Address == 0 {
					bad("%s period %s: total address 0", where, p.End)
				}
				if slices.Contains(addrs, t.Address) {
					bad("%s period %s: duplicate total address %d", where, p.End, t.Address)
				}
				addrs = append(addrs, t.Address)
			}
		}
	}
	registers := map[uint8]bool{}
	for _, t := range f.Tariffs {
		where := fmt.Sprintf("tariff register %d", t.Register)
		if t.Register < 134 || t.Register > 139 {
			bad("%s: registers are 134..139", where)
		}
		if registers[t.Register] {
			bad("%s: duplicate", where)
		}
		registers[t.Register] = true
		for _, r := range slices.Concat(t.Current, t.Stored) {
			timeA(where+" maxPowerTime", r.MaxPowerTime)
			timeA(where+" periodStart", r.PeriodStart)
			timeA(where+" periodEnd", r.PeriodEnd)
			if r.PeriodStart > r.PeriodEnd {
				bad("%s: a record ends before it starts", where)
			}
		}
	}
	return errors.Join(errs...)
}

// clock is the station's clock of the fixture.
func (f *fixture) clock() time.Time {
	t, _ := time.Parse(layoutB, f.Station.Clock) // validated
	return t
}

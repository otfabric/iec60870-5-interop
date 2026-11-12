// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/otfabric/go-iec60870-5/asdu"
)

// The simulated station: see docs/FIXTURES.md. The rules mirror
// scripts/validate-fixtures.py.

type point struct {
	IOA       int      `json:"ioa"`
	Type      string   `json:"type"`
	RawValue  any      `json:"value"`
	Transient *bool    `json:"transient"`
	Sequence  *int     `json:"sequence"`
	Quality   []string `json:"quality"`
	Groups    []int    `json:"groups"`

	typ asdu.TypeID
	obj asdu.InformationObject // the current value, without time tag
}

type command struct {
	IOA            int    `json:"ioa"`
	Type           string `json:"type"`
	Target         int    `json:"target"`
	ReportCause    *int   `json:"reportCause"`
	SelectRequired bool   `json:"selectRequired"`

	typ      asdu.TypeID
	target   *point
	selected bool
}

type fileSpec struct {
	IOA         int `json:"ioa"`
	Name        int `json:"name"`
	Size        int `json:"size"`
	SectionSize int `json:"sectionSize"`
}

type uploadSpec struct {
	IOA     int `json:"ioa"`
	Name    int `json:"name"`
	MaxSize int `json:"maxSize"`
}

type fixture struct {
	SchemaVersion string `json:"schemaVersion"`
	Name          string `json:"name"`
	Station       struct {
		CommonAddr int `json:"commonAddress"`
	} `json:"station"`
	Points   []*point      `json:"points"`
	Commands []*command    `json:"commands"`
	Files    []*fileSpec   `json:"files"`
	Uploads  []*uploadSpec `json:"uploads"`
}

var pointTypes = map[string]asdu.TypeID{
	"M_SP_NA_1": asdu.M_SP_NA_1, "M_DP_NA_1": asdu.M_DP_NA_1, "M_ST_NA_1": asdu.M_ST_NA_1,
	"M_BO_NA_1": asdu.M_BO_NA_1, "M_ME_NA_1": asdu.M_ME_NA_1, "M_ME_NB_1": asdu.M_ME_NB_1,
	"M_ME_NC_1": asdu.M_ME_NC_1, "M_IT_NA_1": asdu.M_IT_NA_1,
}

var commandTypes = map[string]asdu.TypeID{
	"C_SC_NA_1": asdu.C_SC_NA_1, "C_DC_NA_1": asdu.C_DC_NA_1, "C_RC_NA_1": asdu.C_RC_NA_1,
	"C_SE_NA_1": asdu.C_SE_NA_1, "C_SE_NB_1": asdu.C_SE_NB_1, "C_SE_NC_1": asdu.C_SE_NC_1,
}

var commandTarget = map[asdu.TypeID]asdu.TypeID{
	asdu.C_SC_NA_1: asdu.M_SP_NA_1, asdu.C_DC_NA_1: asdu.M_DP_NA_1, asdu.C_RC_NA_1: asdu.M_ST_NA_1,
	asdu.C_SE_NA_1: asdu.M_ME_NA_1, asdu.C_SE_NB_1: asdu.M_ME_NB_1, asdu.C_SE_NC_1: asdu.M_ME_NC_1,
}

func (fx *fixture) point(ioa int) *point {
	for _, p := range fx.Points {
		if p.IOA == ioa {
			return p
		}
	}
	return nil
}

func (fx *fixture) command(ioa int) *command {
	for _, c := range fx.Commands {
		if c.IOA == ioa {
			return c
		}
	}
	return nil
}

// hasGroup reports whether a point of the fixture is in the interrogation group.
func (fx *fixture) hasGroup(group int) bool {
	return slices.ContainsFunc(fx.Points, func(p *point) bool { return slices.Contains(p.Groups, group) })
}

func integer(v any, lo, hi float64) (float64, bool) {
	f, ok := v.(float64)
	return f, ok && f == math.Trunc(f) && f >= lo && f <= hi
}

func allowedQuality(t asdu.TypeID) []string {
	switch t {
	case asdu.M_SP_NA_1, asdu.M_DP_NA_1:
		return []string{"BL", "SB", "NT", "IV"}
	case asdu.M_IT_NA_1:
		return []string{"CY", "CA", "IV"}
	default:
		return []string{"OV", "BL", "SB", "NT", "IV"}
	}
}

var qualityFlags = map[string]asdu.Quality{
	"OV": asdu.QualityOverflow, "BL": asdu.QualityBlocked, "SB": asdu.QualitySubstituted,
	"NT": asdu.QualityNotTopical, "IV": asdu.QualityInvalid,
}

// object builds the information object of a validated point.
func (p *point) object(v float64) asdu.InformationObject {
	ioa := asdu.IOA(p.IOA)
	var q asdu.Quality
	for _, f := range p.Quality {
		q |= qualityFlags[f]
	}
	switch p.typ {
	case asdu.M_SP_NA_1:
		return asdu.SinglePoint{IOA: ioa, Value: v != 0, Quality: q}
	case asdu.M_DP_NA_1:
		return asdu.DoublePoint{IOA: ioa, Value: asdu.DoubleState(v), Quality: q}
	case asdu.M_ST_NA_1:
		return asdu.StepPosition{IOA: ioa, Value: int8(v), Transient: p.Transient != nil && *p.Transient, Quality: q}
	case asdu.M_BO_NA_1:
		return asdu.Bitstring32{IOA: ioa, Value: uint32(v), Quality: q}
	case asdu.M_ME_NA_1:
		return asdu.MeasuredNormalized{IOA: ioa, Value: asdu.Normalized(int16(v)), Quality: q}
	case asdu.M_ME_NB_1:
		return asdu.MeasuredScaled{IOA: ioa, Value: int16(v), Quality: q}
	case asdu.M_ME_NC_1:
		return asdu.MeasuredFloat{IOA: ioa, Value: float32(v), Quality: q}
	default: // M_IT_NA_1
		seq := 0
		if p.Sequence != nil {
			seq = *p.Sequence
		}
		return asdu.IntegratedTotal{IOA: ioa, Value: int32(v), Sequence: uint8(seq),
			Carry: slices.Contains(p.Quality, "CY"), Adjusted: slices.Contains(p.Quality, "CA"),
			Invalid: slices.Contains(p.Quality, "IV")}
	}
}

func loadFixture(path string) (*fixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read fixture %s", path)
	}
	fx := &fixture{}
	if err := json.Unmarshal(raw, fx); err != nil {
		return nil, fmt.Errorf("fixture is not valid JSON: %v", err)
	}
	if fx.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("unsupported schemaVersion, want %s", schemaVersion)
	}
	if fx.Name == "" || fx.Station.CommonAddr < 1 || fx.Station.CommonAddr > 65534 || len(fx.Points) == 0 {
		return nil, fmt.Errorf("fixture needs name, station.commonAddress (1..65534) and points")
	}
	seen := map[int]bool{}
	for _, p := range fx.Points {
		if p == nil {
			return nil, fmt.Errorf("a point is null")
		}
		var ok bool
		if p.typ, ok = pointTypes[p.Type]; !ok || p.IOA < 1 || p.IOA > 0xFFFFFF {
			return nil, fmt.Errorf("point %d: needs ioa (1..16777215) and a supported type, got %q", p.IOA, p.Type)
		}
		if seen[p.IOA] {
			return nil, fmt.Errorf("point %d: duplicate information object address", p.IOA)
		}
		seen[p.IOA] = true
		var v float64
		valid := false
		switch p.typ {
		case asdu.M_SP_NA_1:
			var b bool
			if b, valid = p.RawValue.(bool); b {
				v = 1
			}
		case asdu.M_DP_NA_1:
			v, valid = integer(p.RawValue, 0, 3)
		case asdu.M_ST_NA_1:
			v, valid = integer(p.RawValue, -64, 63)
		case asdu.M_BO_NA_1:
			v, valid = integer(p.RawValue, 0, 4294967295)
		case asdu.M_ME_NA_1, asdu.M_ME_NB_1:
			v, valid = integer(p.RawValue, -32768, 32767)
		case asdu.M_ME_NC_1:
			v, valid = p.RawValue.(float64)
		case asdu.M_IT_NA_1:
			v, valid = integer(p.RawValue, -2147483648, 2147483647)
		}
		if !valid {
			return nil, fmt.Errorf("point %d: value is not valid for %s", p.IOA, p.Type)
		}
		if p.Transient != nil && p.typ != asdu.M_ST_NA_1 {
			return nil, fmt.Errorf("point %d: transient is a boolean of M_ST_NA_1 only", p.IOA)
		}
		if p.Sequence != nil && (p.typ != asdu.M_IT_NA_1 || *p.Sequence < 0 || *p.Sequence > 31) {
			return nil, fmt.Errorf("point %d: sequence is 0..31 of M_IT_NA_1 only", p.IOA)
		}
		for _, q := range p.Quality {
			if !slices.Contains(allowedQuality(p.typ), q) {
				return nil, fmt.Errorf("point %d: quality %q is not valid for %s (%s)", p.IOA, q, p.Type,
					strings.Join(allowedQuality(p.typ), " "))
			}
		}
		if len(p.Groups) > 0 && p.typ == asdu.M_IT_NA_1 {
			return nil, fmt.Errorf("point %d: an integrated total has no interrogation group", p.IOA)
		}
		for i, g := range p.Groups {
			if g < 1 || g > 16 || slices.Contains(p.Groups[:i], g) {
				return nil, fmt.Errorf("point %d: groups are unique and 1..16", p.IOA)
			}
		}
		p.obj = p.object(v)
	}
	sort.Slice(fx.Points, func(i, j int) bool { return fx.Points[i].IOA < fx.Points[j].IOA })

	for _, c := range fx.Commands {
		if c == nil {
			return nil, fmt.Errorf("a command is null")
		}
		var ok bool
		if c.typ, ok = commandTypes[c.Type]; !ok || c.IOA < 1 || c.IOA > 0xFFFFFF {
			return nil, fmt.Errorf("command %d: needs ioa, a supported type and target, got %q", c.IOA, c.Type)
		}
		if seen[c.IOA] {
			return nil, fmt.Errorf("command %d: duplicate information object address", c.IOA)
		}
		seen[c.IOA] = true
		c.target = fx.point(c.Target)
		if c.target == nil || c.target.typ != commandTarget[c.typ] {
			return nil, fmt.Errorf("command %d: target must be a point of the matching type", c.IOA)
		}
		if c.ReportCause == nil {
			cause := 11
			c.ReportCause = &cause
		} else if *c.ReportCause != 3 && *c.ReportCause != 11 {
			return nil, fmt.Errorf("command %d: reportCause must be 3 or 11", c.IOA)
		}
	}
	// Files and uploads are IEC 104 services (fileServer, fileUploadServer
	// are false): a fixture that has them asks for what this station does
	// not do.
	if len(fx.Files) > 0 || len(fx.Uploads) > 0 {
		return nil, fmt.Errorf("files and uploads are not served by an IEC 101 station")
	}
	return fx, nil
}

// fitsFields checks that every address of the fixture fits the field sizes.
func fitsFields(fx *fixture, lc linkConfig) error {
	for _, p := range fx.Points {
		if p.IOA > lc.maxIOA() {
			return fmt.Errorf("point %d does not fit an information object address of %d octets", p.IOA, lc.ioaSize)
		}
	}
	for _, c := range fx.Commands {
		if c.IOA > lc.maxIOA() {
			return fmt.Errorf("command %d does not fit an information object address of %d octets", c.IOA, lc.ioaSize)
		}
	}
	if fx.Station.CommonAddr >= 1<<(8*lc.caSize)-1 {
		return fmt.Errorf("common address %d does not fit a common address of %d octets", fx.Station.CommonAddr, lc.caSize)
	}
	return nil
}

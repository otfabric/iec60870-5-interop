// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/otfabric/go-iec60870-5/iec102"
	"github.com/otfabric/go-iec60870-5/iec102/ree"
)

var stdoutMu sync.Mutex

// writeJSONLine writes v as one line of JSON.
func writeJSONLine(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only our own documents are marshalled
	}
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	_, _ = w.Write(append(b, '\n'))
}

// field is one member of an ordered JSON object.
type field struct {
	key   string
	value any
}

// object is a JSON object whose members keep their order.
type object []field

// MarshalJSON writes the members in order.
func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.key)
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// typeName is the mnemonic of a type identification (REE names for
// 128..255), or nil for a type the library does not name.
func typeName(t iec102.TypeID) any {
	if s := ree.Name(t); !strings.HasPrefix(s, "TypeID(") {
		return s
	}
	return nil
}

// Request types whose document has no information object.
var noObjects = map[iec102.TypeID]bool{
	iec102.C_RD_NA_2: true, iec102.C_TI_NA_2: true, ree.C_TA_VC_2: true, ree.C_AC_NA_2: true, ree.C_FS_NA_2: true,
}

// asduDoc is the ASDU document of the contract ("IEC 102 ASDU documents").
func asduDoc(a *iec102.ASDU, p iec102.Params) object {
	d := object{
		{"type", typeName(a.Type)}, {"typeId", int(a.Type)},
		{"cot", int(a.Cause.Code())}, {"negative", a.Cause.Negative()}, {"test", a.Cause.Test()},
		{"originator", 0}, {"commonAddress", int(a.CommonAddr)}, {"record", int(a.Record)},
		{"sequence", a.VSQ.SQ()}, {"count", a.VSQ.Number()},
	}
	objects := []any{}
	switch b := a.Body.(type) {
	case iec102.IntegratedTotals:
		for _, t := range b.Totals {
			objects = append(objects, object{{"address", int(t.Addr)}, {"value", t.Value}, {"quality", int(t.Quality)}})
		}
		return append(d, field{"objects", objects}, field{"time", textA(b.Time)},
			field{"su", b.Time.SummerTime}, field{"timeInvalid", b.Time.Invalid})
	case iec102.ProductSpec:
		objects = append(objects, object{{"standard", int(b.Standard)}, {"manufacturer", int(b.Manufacturer)}, {"product", b.Product}})
	case iec102.SystemTime:
		objects = append(objects, timeBDoc(b.Time))
	case ree.SetClock:
		objects = append(objects, timeBDoc(b.Time))
	case iec102.Read:
		switch {
		case b.T == iec102.C_CI_NT_2 || b.T == iec102.C_CI_NU_2:
			objects = append(objects, object{{"first", int(b.First)}, {"last", int(b.Last)}, {"from", textA(b.From)}, {"to", textA(b.To)}})
		case !noObjects[b.T]:
			return append(d, field{"objects", objects}, field{"data", bodyHex(a, p)})
		}
	case ree.Request:
		switch {
		case b.T == ree.C_TA_VM_2:
			objects = append(objects, object{{"from", textA(b.From)}, {"to", textA(b.To)}})
		case !noObjects[b.T]:
			return append(d, field{"objects", objects}, field{"data", bodyHex(a, p)})
		}
	case ree.AccessKey:
		// the access key is not reported
	case ree.TariffInfo:
		for _, r := range b.Records {
			objects = append(objects, tariffDoc(r))
		}
	default:
		if !noObjects[a.Type] {
			return append(d, field{"objects", objects}, field{"data", bodyHex(a, p)})
		}
	}
	return append(d, field{"objects", objects})
}

func timeBDoc(t iec102.TimeB) object {
	return object{{"time", textB(t)}, {"su", t.SummerTime}, {"timeInvalid", t.Invalid}}
}

func tariffDoc(r ree.TariffRecord) object {
	return object{
		{"address", int(r.Addr)}, {"activeAbs", r.ActiveAbs}, {"activeInc", r.ActiveInc}, {"activeQuality", int(r.ActiveQual)},
		{"reactiveIndAbs", r.ReactiveIndAbs}, {"reactiveIndInc", r.ReactiveIndInc}, {"reactiveIndQuality", int(r.ReactiveIndQual)},
		{"reactiveCapAbs", r.ReactiveCapAbs}, {"reactiveCapInc", r.ReactiveCapInc}, {"reactiveCapQuality", int(r.ReactiveCapQual)},
		{"maxPower", r.MaxPower}, {"maxPowerTime", textA(r.MaxPowerTime)}, {"maxPowerQuality", int(r.MaxPowerQual)},
		{"excessPower", r.ExcessPower}, {"excessPowerQuality", int(r.ExcessPowerQual)},
		{"periodStart", textA(r.Start)}, {"periodEnd", textA(r.End)},
	}
}

// bodyHex is the octets after the record address, in lower-case hex.
func bodyHex(a *iec102.ASDU, p iec102.Params) string {
	if r, ok := a.Body.(iec102.Raw); ok {
		return hex.EncodeToString(r.Data)
	}
	b, err := a.Body.AppendTo(nil, p)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

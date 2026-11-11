// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/riclolsen/go-iecp5/cs103"
)

// typeNames are the names of the IEC 103 type identifications in ASDU
// documents (docs/CONTAINER_CONTRACT.md, IEC 103).
var typeNames = map[byte]string{
	1: "time-tagged-message", 2: "time-tagged-message-relative", 3: "measurands-i",
	4: "time-tagged-measurands", 5: "identification", 6: "time-synchronization",
	7: "general-interrogation", 8: "general-interrogation-termination", 9: "measurands-ii",
	10: "generic-data", 11: "generic-identification", 20: "general-command", 21: "generic-command",
	23: "disturbance-list", 24: "disturbance-order", 25: "disturbance-acknowledgement",
	26: "disturbance-ready", 27: "channel-ready", 28: "tags-ready", 29: "tags",
	30: "disturbance-values", 31: "disturbance-end",
}

// elementLength is the length of the information elements after FUN and INF
// that the standard lays out for a type; 0 for the measurands, whose length
// follows from VSQ, and -1 for a type this adapter does not decode.
func elementLength(t byte) int {
	switch t {
	case 1:
		return 6 // DPI, CP32Time2a, SIN
	case 2:
		return 10 // DPI, RET, FAN, CP32Time2a, SIN
	case 3, 9:
		return 0
	case 4:
		return 12 // SCL, RET, FAN, CP32Time2a
	case 5:
		return 13 // COL, 8 ASCII characters, 4 octets of software identification
	case 6:
		return 7 // CP56Time2a
	case 7, 8:
		return 1 // SCN
	case 20:
		return 2 // DCO, RII
	}
	return -1
}

// time32 formats a CP32Time2a as HH:MM:SS.mmm; the date is not on the wire.
func time32(b []byte) (string, bool) {
	ms := int(binary.LittleEndian.Uint16(b))
	return fmt.Sprintf("%02d:%02d:%02d.%03d", b[3]&0x1f, b[2]&0x3f, ms/1000, ms%1000), b[2]&0x80 != 0
}

// asduDoc is the ASDU document of the contract for one received ASDU, built
// from what cs103 decodes.
func asduDoc(a *cs103.ASDU) object {
	t := byte(a.Type)
	var name any
	if n, ok := typeNames[t]; ok {
		name = n
	}
	doc := object{
		"type": name, "typeId": int(t), "cot": int(a.Coa),
		"negative": false, "test": false, "originator": 0,
		"commonAddress": int(a.CommonAddr), "sequence": a.Variable.IsSequence,
		"count": int(a.Variable.Number), "objects": []object{},
	}
	if len(a.InfoObj) < 2 {
		return doc
	}
	obj := object{"fun": int(a.Fun()), "inf": int(a.Inf())}
	el := a.InfoObj[2:]
	want := elementLength(t)
	if t == 3 || t == 9 {
		want = 2 * int(a.Variable.Number)
	}
	if want < 0 || len(el) != want {
		obj["data"] = hex.EncodeToString(el)
		doc["objects"] = []object{obj}
		return doc
	}
	switch t {
	case 1, 2:
		info, err := a.GetTimeTagged()
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		obj["dpi"] = int(info.Dpi)
		tb := el[1:5]
		if t == 2 {
			obj["ret"] = int(info.RelativeTime)
			obj["fan"] = int(info.FaultNumber)
			tb = el[5:9]
		}
		s, iv := time32(tb)
		obj["time32"] = s
		if iv {
			obj["timeInvalid"] = true
		}
		obj["sin"] = int(info.Sin)
	case 3, 9:
		info, err := a.GetMeasurands()
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		vals := []object{}
		for _, m := range info.Values {
			vals = append(vals, object{"value": int(m.Val), "ov": m.Overflow, "er": m.Invalid})
		}
		obj["values"] = vals
	case 4:
		info, err := a.GetTimeTaggedMeasurands()
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		obj["scl"] = float64(info.Value)
		obj["ret"] = int(info.RelativeTime)
		obj["fan"] = int(info.FaultNumber)
		s, iv := time32(el[8:12])
		obj["time32"] = s
		if iv {
			obj["timeInvalid"] = true
		}
	case 5:
		info, err := a.GetIdentification()
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		obj["col"] = int(info.Col)
		obj["ascii"] = info.ASCII
		obj["software"] = hex.EncodeToString(info.Extra)
	case 6:
		tm, err := a.GetTimeSync()
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		obj["time"] = tm.UTC().Format("2006-01-02T15:04:05.000Z")
		if el[2]&0x80 != 0 {
			obj["timeInvalid"] = true
		}
	case 7:
		obj["scn"] = int(el[0])
	case 8:
		scn, err := a.GetGITermination()
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		obj["scn"] = int(scn)
	case 20:
		info, err := a.GetGeneralCommand()
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		obj["dco"] = int(el[0])
		obj["rii"] = int(info.Rii)
	}
	doc["objects"] = []object{obj}
	return doc
}

// parseTime reads the --time flag of clock-sync.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Now().UTC(), nil
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return t, usagef("--time: '%s' is not like 2026-01-02T03:04:05.678Z", s)
	}
	return t, nil
}

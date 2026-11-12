// SPDX-License-Identifier: MIT

package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/cp"
	"github.com/otfabric/go-iec60870-5/iec103"
)

// typeNames are the names of the IEC 103 type identifications in ASDU
// documents (docs/CONTAINER_CONTRACT.md, "IEC 103 ASDU documents").
var typeNames = map[byte]string{
	1: "time-tagged-message", 2: "time-tagged-message-relative", 3: "measurands-i",
	4: "time-tagged-measurands", 5: "identification", 6: "time-synchronization",
	7: "general-interrogation", 8: "general-interrogation-termination", 9: "measurands-ii",
	10: "generic-data", 11: "generic-identification", 20: "general-command", 21: "generic-command",
	23: "disturbance-list", 24: "disturbance-order", 25: "disturbance-acknowledgement",
	26: "disturbance-ready", 27: "channel-ready", 28: "tags-ready", 29: "tags",
	30: "disturbance-values", 31: "disturbance-end",
}

// elementLength is the length of the information elements after FUN and
// INF that the standard lays out for a type: for the measurands it
// follows from the number of the VSQ; -1 for a type whose elements are
// reported as data.
func elementLength(t byte, n int) int {
	switch t {
	case 1:
		return 6 // DPI, CP32Time2a, SIN
	case 2:
		return 10 // DPI, RET, FAN, CP32Time2a, SIN
	case 3, 9:
		return 2 * n // MEA
	case 4:
		return 12 // SCL, RET, FAN, CP32Time2a
	case 5:
		return 13 // COL, eight ASCII characters, four octets of software identification
	case 6:
		return 7 // CP56Time2a
	case 7, 8:
		return 1 // SCN
	case 20:
		return 2 // DCO, RII
	}
	return -1
}

// time32 formats a CP32Time2a as HH:MM:SS.mmm (it has no date) and
// reports its IV bit.
func time32(b []byte) (string, bool) {
	ms := int(binary.LittleEndian.Uint16(b))
	return fmt.Sprintf("%02d:%02d:%02d.%03d", b[3]&0x1f, b[2]&0x3f, ms/1000, ms%1000), b[2]&0x80 != 0
}

// asduDoc is the ASDU document of the contract for the octets of one
// received ASDU, as the link layer of the library handed them.
func asduDoc(raw []byte) object {
	doc := object{
		"type": nil, "typeId": 0, "cot": 0, "negative": false, "test": false, "originator": 0,
		"commonAddress": 0, "sequence": false, "count": 0, "objects": []object{},
	}
	if len(raw) < 4 {
		return doc
	}
	t := raw[0]
	if n, ok := typeNames[t]; ok {
		doc["type"] = n
	}
	vsq := iec103.VSQ(raw[1])
	doc["typeId"], doc["cot"], doc["commonAddress"] = int(t), int(raw[2]), int(raw[3])
	doc["sequence"], doc["count"] = vsq.SQ(), vsq.Number()
	if len(raw) < 6 {
		return doc
	}
	obj := object{"fun": int(raw[4]), "inf": int(raw[5])}
	doc["objects"] = []object{obj}
	el := raw[6:]
	if want := elementLength(t, vsq.Number()); want < 0 || len(el) != want {
		obj["data"] = hex.EncodeToString(el)
		return doc
	}
	switch t {
	case 1:
		obj["dpi"] = int(el[0] & 3)
		putTime32(obj, el[1:5])
		obj["sin"] = int(el[5])
	case 2:
		obj["dpi"] = int(el[0] & 3)
		obj["ret"] = int(binary.LittleEndian.Uint16(el[1:]))
		obj["fan"] = int(binary.LittleEndian.Uint16(el[3:]))
		putTime32(obj, el[5:9])
		obj["sin"] = int(el[9])
	case 3, 9:
		vals := []object{}
		for i := 0; i+1 < len(el); i += 2 {
			w := binary.LittleEndian.Uint16(el[i:])
			v := w >> 3
			vals = append(vals, object{"value": int(int16(v&0x0FFF) - int16(v&0x1000)), "ov": w&1 != 0, "er": w&2 != 0})
		}
		obj["values"] = vals
	case 4:
		obj["scl"] = float64(math.Float32frombits(binary.LittleEndian.Uint32(el)))
		obj["ret"] = int(binary.LittleEndian.Uint16(el[4:]))
		obj["fan"] = int(binary.LittleEndian.Uint16(el[6:]))
		putTime32(obj, el[8:12])
	case 5:
		obj["col"] = int(el[0])
		obj["ascii"] = strings.TrimRight(string(el[1:9]), " \x00")
		obj["software"] = hex.EncodeToString(el[9:13])
	case 6:
		ts, err := cp.ParseCP56(el, nil)
		if err != nil {
			obj["data"] = hex.EncodeToString(el)
			break
		}
		obj["time"] = ts.UTC().Format(timeLayout)
		if ts.Invalid {
			obj["timeInvalid"] = true
		}
	case 7, 8:
		obj["scn"] = int(el[0])
	case 20:
		obj["dco"] = int(el[0])
		obj["rii"] = int(el[1])
	}
	return doc
}

func putTime32(obj object, b []byte) {
	s, iv := time32(b)
	obj["time32"] = s
	if iv {
		obj["timeInvalid"] = true
	}
}

// setDoc is one data set of generic data: GIN, KOD, the GDD and the values
// of the GID as its data type lays them out; another data type has data.
func setDoc(d iec103.DataSet) object {
	v := d.Value
	doc := object{
		"gin": fmt.Sprintf("%d.%d", d.GIN.Group, d.GIN.Entry), "kod": int(d.KOD),
		"type": int(v.Type), "size": int(v.Size), "number": int(v.Number),
	}
	values := []any{}
	switch v.Type {
	case iec103.DataASCII:
		values = append(values, strings.TrimRight(v.Text(), "\x00"))
	case iec103.DataUint, iec103.DataGRC, iec103.DataInt:
		for i := range int(v.Number) {
			var x any
			var err error
			if v.Type == iec103.DataInt {
				x, err = v.Int(i)
			} else {
				x, err = v.Uint(i)
			}
			if err != nil {
				doc["data"] = hex.EncodeToString(v.Data)
				return doc
			}
			values = append(values, x)
		}
	case iec103.DataFloat32, iec103.DataFloat64:
		for i := range int(v.Number) {
			x, err := v.Float(i)
			if err != nil {
				doc["data"] = hex.EncodeToString(v.Data)
				return doc
			}
			values = append(values, x)
		}
	default:
		doc["data"] = hex.EncodeToString(v.Data)
		return doc
	}
	doc["values"] = values
	return doc
}

func setDocs(sets []iec103.DataSet) []object {
	out := []object{}
	for _, s := range sets {
		out = append(out, setDoc(s))
	}
	return out
}

// faultDoc is one entry of the list of recorded disturbances.
func faultDoc(f iec103.RecordedFault) object {
	return object{
		"fan": int(f.FAN),
		"sof": object{
			"tp": f.SOF&iec103.SOFTrip != 0, "tm": f.SOF&iec103.SOFTM != 0,
			"test": f.SOF&iec103.SOFTest != 0, "otev": f.SOF&iec103.SOFOTEV != 0,
		},
		"time": f.Time.UTC().Format(timeLayout),
	}
}

// disturbanceDoc is a transferred record: the description of ASDU 26, the
// tags of the ASDUs 29 and the channels of the ASDUs 27 and 30.
func disturbanceDoc(rec *iec103.Disturbance) object {
	tags := []object{}
	for _, t := range rec.Tags {
		tags = append(tags, object{"pos": int(t.Pos), "fun": int(t.Fun), "inf": int(t.Inf), "dpi": int(t.DPI)})
	}
	channels := []object{}
	noe := 0
	for i, c := range rec.Channels {
		vals := make([]int, 0, len(c.Values))
		for _, v := range c.Values {
			vals = append(vals, int(v))
		}
		if i == 0 {
			noe = len(vals)
		}
		channels = append(channels, object{
			"acc": int(c.ACC), "rpv": float64(c.RPV), "rsv": float64(c.RSV), "rfa": float64(c.RFA), "values": vals,
		})
	}
	return object{
		"fan": int(rec.FAN), "tov": int(rec.TOV), "nof": int(rec.NOF), "noc": len(rec.Channels), "noe": noe,
		"interval": int(rec.Interval), "start": rec.Start.UTC().Format("15:04:05.000"),
		"tags": tags, "channels": channels,
	}
}

// parseTime reads the --time flag of clock-sync.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Now().UTC(), nil
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return t, usagef("--time: '%s' is not like 2026-01-02T03:04:05.678Z", s)
	}
	return t, nil
}

// SPDX-License-Identifier: MIT

package main

import (
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
)

// Conversions between go-iec60870-5's ASDUs and the JSON of the contract
// (CONTAINER_CONTRACT.md, ASDU documents).

type object = map[string]any

// modelled are the types the contract models for IEC 101; anything else is
// reported with "type": null and no objects.
var modelled = map[asdu.TypeID]bool{}

func init() {
	for _, t := range []asdu.TypeID{
		asdu.M_SP_NA_1, asdu.M_DP_NA_1, asdu.M_ST_NA_1, asdu.M_BO_NA_1, asdu.M_ME_NA_1, asdu.M_ME_NB_1,
		asdu.M_ME_NC_1, asdu.M_IT_NA_1,
		asdu.M_SP_TB_1, asdu.M_DP_TB_1, asdu.M_ST_TB_1, asdu.M_BO_TB_1, asdu.M_ME_TD_1, asdu.M_ME_TE_1,
		asdu.M_ME_TF_1, asdu.M_IT_TB_1,
		asdu.C_SC_NA_1, asdu.C_DC_NA_1, asdu.C_RC_NA_1, asdu.C_SE_NA_1, asdu.C_SE_NB_1, asdu.C_SE_NC_1,
		asdu.C_SC_TA_1, asdu.C_DC_TA_1, asdu.C_RC_TA_1, asdu.C_SE_TA_1, asdu.C_SE_TB_1, asdu.C_SE_TC_1,
		asdu.M_EI_NA_1, asdu.C_IC_NA_1, asdu.C_CI_NA_1, asdu.C_RD_NA_1, asdu.C_CS_NA_1, asdu.C_TS_NA_1,
		asdu.C_TS_TA_1,
	} {
		modelled[t] = true
	}
}

// timeTagged maps a point type to its CP56Time2a variant, the type of the
// report after a command.
var timeTagged = map[asdu.TypeID]asdu.TypeID{
	asdu.M_SP_NA_1: asdu.M_SP_TB_1, asdu.M_DP_NA_1: asdu.M_DP_TB_1, asdu.M_ST_NA_1: asdu.M_ST_TB_1,
	asdu.M_BO_NA_1: asdu.M_BO_TB_1, asdu.M_ME_NA_1: asdu.M_ME_TD_1, asdu.M_ME_NB_1: asdu.M_ME_TE_1,
	asdu.M_ME_NC_1: asdu.M_ME_TF_1, asdu.M_IT_NA_1: asdu.M_IT_TB_1,
}

const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(timeLayout, s)
	return t, err == nil
}

// qualityJSON lists the flags of q in the contract's order; overflow is
// false for the types whose descriptor (SIQ, DIQ) has no OV.
func qualityJSON(q asdu.Quality, overflow bool) []string {
	out := []string{}
	for _, f := range []struct {
		bit  asdu.Quality
		name string
	}{
		{asdu.QualityOverflow, "OV"}, {asdu.QualityBlocked, "BL"}, {asdu.QualitySubstituted, "SB"},
		{asdu.QualityNotTopical, "NT"}, {asdu.QualityInvalid, "IV"},
	} {
		if q&f.bit != 0 && (overflow || f.bit != asdu.QualityOverflow) {
			out = append(out, f.name)
		}
	}
	return out
}

func counterQualityJSON(o asdu.IntegratedTotal) []string {
	out := []string{}
	if o.Carry {
		out = append(out, "CY")
	}
	if o.Adjusted {
		out = append(out, "CA")
	}
	if o.Invalid {
		out = append(out, "IV")
	}
	return out
}

func normalized(o object, v asdu.Normalized) {
	o["value"] = int(v)
	o["normalized"] = float64(v) / 32768
}

func withTime(o object, t asdu.Timestamp) {
	o["time"] = formatTime(t.Time)
	if t.Invalid {
		o["timeInvalid"] = true
	}
}

// objectJSON is the contract's form of one information object of an ASDU
// of type t.
func objectJSON(t asdu.TypeID, io asdu.InformationObject) object {
	o := object{"ioa": int(io.Address())}
	timed := t.HasTimeTag()
	switch v := io.(type) {
	case asdu.SinglePoint:
		o["value"], o["quality"] = v.Value, qualityJSON(v.Quality, false)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.DoublePoint:
		o["value"], o["quality"] = int(v.Value), qualityJSON(v.Quality, false)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.StepPosition:
		o["value"], o["transient"], o["quality"] = int(v.Value), v.Transient, qualityJSON(v.Quality, true)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.Bitstring32:
		o["value"], o["quality"] = v.Value, qualityJSON(v.Quality, true)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.MeasuredNormalized:
		normalized(o, v.Value)
		o["quality"] = qualityJSON(v.Quality, true)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.MeasuredScaled:
		o["value"], o["quality"] = int(v.Value), qualityJSON(v.Quality, true)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.MeasuredFloat:
		o["value"], o["quality"] = float64(v.Value), qualityJSON(v.Quality, true)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.IntegratedTotal:
		o["value"], o["sequence"], o["quality"] = int(v.Value), int(v.Sequence), counterQualityJSON(v)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.SingleCommand:
		o["value"], o["select"], o["qualifier"] = v.Value, v.Select, int(v.Qualifier)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.DoubleCommand:
		o["value"], o["select"], o["qualifier"] = int(v.Value), v.Select, int(v.Qualifier)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.StepCommand:
		o["value"], o["select"], o["qualifier"] = int(v.Value), v.Select, int(v.Qualifier)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.SetpointNormalized:
		normalized(o, v.Value)
		o["select"], o["qualifier"] = v.Select, int(v.Qualifier)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.SetpointScaled:
		o["value"], o["select"], o["qualifier"] = int(v.Value), v.Select, int(v.Qualifier)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.SetpointFloat:
		o["value"], o["select"], o["qualifier"] = float64(v.Value), v.Select, int(v.Qualifier)
		if timed {
			withTime(o, v.Time)
		}
	case asdu.EndOfInitialization:
		coi := int(v.Cause)
		if v.LocalChange {
			coi |= 0x80
		}
		o["coi"] = coi
	case asdu.Interrogation:
		o["qoi"] = int(v.Qualifier)
	case asdu.CounterInterrogation:
		o["qcc"] = int(v.Request) | int(v.Freeze)<<6
	case asdu.ClockSync:
		withTime(o, v.Time)
	case asdu.TestCommand:
		if t == asdu.C_TS_NA_1 {
			o["fbp"] = int(v.Counter)
		} else {
			o["counter"] = int(v.Counter)
			withTime(o, v.Time)
		}
	case asdu.Read:
	}
	return o
}

// asduJSON is the ASDU document of the contract for a received ASDU.
func asduJSON(a *asdu.ASDU) object {
	j := object{
		"type": nil, "typeId": int(a.Type),
		"cot": int(a.Cause), "negative": a.Negative, "test": a.Test,
		"originator": int(a.Originator), "commonAddress": int(a.CommonAddr),
		"sequence": a.Sequence, "count": a.Len(),
		"objects": []object{},
	}
	if !modelled[a.Type] {
		return j
	}
	j["type"] = a.Type.String()
	objs := make([]object, 0, len(a.Objects))
	for _, o := range a.Objects {
		objs = append(objs, objectJSON(a.Type, o))
	}
	j["objects"] = objs
	return j
}

// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import com.digitalpetri.iec60870.asdu.Asdu;
import com.digitalpetri.iec60870.asdu.AsduType;
import com.digitalpetri.iec60870.asdu.InformationObject;
import com.digitalpetri.iec60870.asdu.element.BinaryCounterReading;
import com.digitalpetri.iec60870.asdu.element.Qds;
import com.digitalpetri.iec60870.asdu.object.Bitstring32;
import com.digitalpetri.iec60870.asdu.object.Bitstring32WithCp56Time;
import com.digitalpetri.iec60870.asdu.object.ClockSynchronizationCommand;
import com.digitalpetri.iec60870.asdu.object.CounterInterrogationCommand;
import com.digitalpetri.iec60870.asdu.object.DoubleCommand;
import com.digitalpetri.iec60870.asdu.object.DoubleCommandWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.DoublePointInformation;
import com.digitalpetri.iec60870.asdu.object.DoublePointWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.EndOfInitialization;
import com.digitalpetri.iec60870.asdu.object.IntegratedTotals;
import com.digitalpetri.iec60870.asdu.object.IntegratedTotalsWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.InterrogationCommand;
import com.digitalpetri.iec60870.asdu.object.MeasuredValueNormalized;
import com.digitalpetri.iec60870.asdu.object.MeasuredValueNormalizedWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.MeasuredValueScaled;
import com.digitalpetri.iec60870.asdu.object.MeasuredValueScaledWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.MeasuredValueShortFloat;
import com.digitalpetri.iec60870.asdu.object.MeasuredValueShortFloatWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.RegulatingStepCommand;
import com.digitalpetri.iec60870.asdu.object.RegulatingStepCommandWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.SetpointNormalized;
import com.digitalpetri.iec60870.asdu.object.SetpointNormalizedWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.SetpointScaled;
import com.digitalpetri.iec60870.asdu.object.SetpointScaledWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.SetpointShortFloat;
import com.digitalpetri.iec60870.asdu.object.SetpointShortFloatWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.SingleCommand;
import com.digitalpetri.iec60870.asdu.object.SingleCommandWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.SinglePointInformation;
import com.digitalpetri.iec60870.asdu.object.SinglePointWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.StepPositionInformation;
import com.digitalpetri.iec60870.asdu.object.StepPositionWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.TestCommand;
import com.digitalpetri.iec60870.asdu.time.Cp56Time2a;
import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import java.util.ArrayList;
import java.util.EnumSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** The JSON of the contract: documents, ASDUs, times. */
final class Codec {
    private Codec() {}

    private static final Gson GSON = new GsonBuilder().serializeNulls().disableHtmlEscaping().create();
    private static final PrintStream OUT = new PrintStream(System.out, true, StandardCharsets.UTF_8);
    static final DateTimeFormatter TIME =
            DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'").withZone(ZoneOffset.UTC);

    /** The types the contract models; anything else is reported with "type": null. */
    private static final Set<AsduType> MODELLED = EnumSet.of(
            AsduType.M_SP_NA_1, AsduType.M_DP_NA_1, AsduType.M_ST_NA_1, AsduType.M_BO_NA_1,
            AsduType.M_ME_NA_1, AsduType.M_ME_NB_1, AsduType.M_ME_NC_1, AsduType.M_IT_NA_1,
            AsduType.M_SP_TB_1, AsduType.M_DP_TB_1, AsduType.M_ST_TB_1, AsduType.M_BO_TB_1,
            AsduType.M_ME_TD_1, AsduType.M_ME_TE_1, AsduType.M_ME_TF_1, AsduType.M_IT_TB_1,
            AsduType.C_SC_NA_1, AsduType.C_DC_NA_1, AsduType.C_RC_NA_1, AsduType.C_SE_NA_1,
            AsduType.C_SE_NB_1, AsduType.C_SE_NC_1, AsduType.C_SC_TA_1, AsduType.C_DC_TA_1,
            AsduType.C_RC_TA_1, AsduType.C_SE_TA_1, AsduType.C_SE_TB_1, AsduType.C_SE_TC_1,
            AsduType.M_EI_NA_1, AsduType.C_IC_NA_1, AsduType.C_CI_NA_1, AsduType.C_RD_NA_1,
            AsduType.C_CS_NA_1, AsduType.C_TS_NA_1);

    /** Prints one JSON document on one line of stdout. */
    static synchronized void emit(Map<String, Object> doc) {
        OUT.println(GSON.toJson(doc));
        OUT.flush();
    }

    static void log(String format, Object... args) {
        System.err.println("[" + Main.ADAPTER + "] " + String.format(format, args));
    }

    static Map<String, Object> object() {
        return new LinkedHashMap<>();
    }

    static String formatTime(Instant t) {
        return TIME.format(t);
    }

    static String formatTime(Cp56Time2a t) {
        try {
            return formatTime(t.toInstant(ZoneOffset.UTC));
        } catch (RuntimeException e) {
            return "invalid";
        }
    }

    static List<String> quality(Qds q, boolean overflow) {
        List<String> out = new ArrayList<>();
        if (overflow && q.overflow()) {
            out.add("OV");
        }
        if (q.blocked()) {
            out.add("BL");
        }
        if (q.substituted()) {
            out.add("SB");
        }
        if (q.notTopical()) {
            out.add("NT");
        }
        if (q.invalid()) {
            out.add("IV");
        }
        return out;
    }

    private static List<String> counterQuality(BinaryCounterReading c) {
        List<String> out = new ArrayList<>();
        if (c.carry()) {
            out.add("CY");
        }
        if (c.adjusted()) {
            out.add("CA");
        }
        if (c.invalid()) {
            out.add("IV");
        }
        return out;
    }

    private static void time(Map<String, Object> o, Cp56Time2a t) {
        o.put("time", formatTime(t));
        if (t.invalid()) {
            o.put("timeInvalid", true);
        }
    }

    private static void normalized(Map<String, Object> o, short raw) {
        o.put("value", (int) raw);
        o.put("normalized", raw / 32768.0);
    }

    private static void counter(Map<String, Object> o, BinaryCounterReading c) {
        o.put("value", c.value());
        o.put("sequence", c.sequenceNumber());
        o.put("quality", counterQuality(c));
    }

    /** The contract's form of one information object. */
    static Map<String, Object> objectJson(InformationObject io) {
        Map<String, Object> o = object();
        o.put("ioa", io.address().value().intValue());
        switch (io) {
            case SinglePointInformation p -> {
                o.put("value", p.on());
                o.put("quality", quality(p.quality(), false));
            }
            case SinglePointWithCp56Time p -> {
                time(o, p.time());
                o.put("value", p.on());
                o.put("quality", quality(p.quality(), false));
            }
            case DoublePointInformation p -> {
                o.put("value", p.state().value());
                o.put("quality", quality(p.quality(), false));
            }
            case DoublePointWithCp56Time p -> {
                time(o, p.time());
                o.put("value", p.state().value());
                o.put("quality", quality(p.quality(), false));
            }
            case StepPositionInformation p -> {
                o.put("value", p.value().value());
                o.put("transient", p.value().transientState());
                o.put("quality", quality(p.quality(), true));
            }
            case StepPositionWithCp56Time p -> {
                time(o, p.time());
                o.put("value", p.value().value());
                o.put("transient", p.value().transientState());
                o.put("quality", quality(p.quality(), true));
            }
            case Bitstring32 p -> {
                o.put("value", Integer.toUnsignedLong(p.bits()));
                o.put("quality", quality(p.quality(), true));
            }
            case Bitstring32WithCp56Time p -> {
                time(o, p.time());
                o.put("value", Integer.toUnsignedLong(p.bits()));
                o.put("quality", quality(p.quality(), true));
            }
            case MeasuredValueNormalized p -> {
                normalized(o, p.value().rawValue());
                o.put("quality", quality(p.quality(), true));
            }
            case MeasuredValueNormalizedWithCp56Time p -> {
                time(o, p.time());
                normalized(o, p.value().rawValue());
                o.put("quality", quality(p.quality(), true));
            }
            case MeasuredValueScaled p -> {
                o.put("value", (int) p.value());
                o.put("quality", quality(p.quality(), true));
            }
            case MeasuredValueScaledWithCp56Time p -> {
                time(o, p.time());
                o.put("value", (int) p.value());
                o.put("quality", quality(p.quality(), true));
            }
            case MeasuredValueShortFloat p -> {
                o.put("value", (double) p.value());
                o.put("quality", quality(p.quality(), true));
            }
            case MeasuredValueShortFloatWithCp56Time p -> {
                time(o, p.time());
                o.put("value", (double) p.value());
                o.put("quality", quality(p.quality(), true));
            }
            case IntegratedTotals p -> counter(o, p.counter());
            case IntegratedTotalsWithCp56Time p -> {
                time(o, p.time());
                counter(o, p.counter());
            }
            case SingleCommand c -> {
                o.put("value", c.on());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().qualifier());
            }
            case SingleCommandWithCp56Time c -> {
                time(o, c.time());
                o.put("value", c.on());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().qualifier());
            }
            case DoubleCommand c -> {
                o.put("value", c.state().value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().qualifier());
            }
            case DoubleCommandWithCp56Time c -> {
                time(o, c.time());
                o.put("value", c.state().value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().qualifier());
            }
            case RegulatingStepCommand c -> {
                o.put("value", c.state().value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().qualifier());
            }
            case RegulatingStepCommandWithCp56Time c -> {
                time(o, c.time());
                o.put("value", c.state().value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().qualifier());
            }
            case SetpointNormalized c -> {
                normalized(o, c.value().rawValue());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().ql());
            }
            case SetpointNormalizedWithCp56Time c -> {
                time(o, c.time());
                normalized(o, c.value().rawValue());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().ql());
            }
            case SetpointScaled c -> {
                o.put("value", (int) c.value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().ql());
            }
            case SetpointScaledWithCp56Time c -> {
                time(o, c.time());
                o.put("value", (int) c.value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().ql());
            }
            case SetpointShortFloat c -> {
                o.put("value", (double) c.value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().ql());
            }
            case SetpointShortFloatWithCp56Time c -> {
                time(o, c.time());
                o.put("value", (double) c.value());
                o.put("select", c.qualifier().select());
                o.put("qualifier", c.qualifier().ql());
            }
            case EndOfInitialization e -> o.put("coi", e.cause().value() | (e.cause().afterParameterChange() ? 0x80 : 0));
            case InterrogationCommand c -> o.put("qoi", c.qualifier().value().intValue());
            case CounterInterrogationCommand c ->
                    o.put("qcc", c.qualifier().request() | (c.qualifier().freeze().value() << 6));
            case ClockSynchronizationCommand c -> time(o, c.time());
            case TestCommand c -> o.put("fbp", c.pattern().value().intValue());
            default -> {
                // C_RD_NA_1 has no field beyond the address.
            }
        }
        return o;
    }

    /** The ASDU document of the contract for a decoded ASDU. */
    static Map<String, Object> asduJson(Asdu a) {
        Map<String, Object> j = object();
        boolean modelled = MODELLED.contains(a.type());
        j.put("type", modelled ? a.type().name() : null);
        j.put("typeId", a.type().typeId());
        j.put("cot", a.cause().value());
        j.put("negative", a.negative());
        j.put("test", a.test());
        j.put("originator", a.originatorAddress().value().intValue());
        j.put("commonAddress", a.commonAddress().value().intValue());
        j.put("sequence", a.sequence());
        j.put("count", a.objects().size());
        List<Map<String, Object>> objects = new ArrayList<>();
        if (modelled) {
            for (InformationObject io : a.objects()) {
                objects.add(objectJson(io));
            }
        }
        j.put("objects", objects);
        return j;
    }

    /** The ASDU document of an ASDU the library could not decode: the header only. */
    static Map<String, Object> rawAsduJson(byte[] raw, LinkConfig lc) {
        Map<String, Object> j = object();
        j.put("type", null);
        j.put("typeId", raw.length > 0 ? raw[0] & 0xff : 0);
        int header = 2 + lc.cotSize + lc.caSize;
        if (raw.length >= header) {
            j.put("cot", raw[2] & 0x3f);
            j.put("negative", (raw[2] & 0x40) != 0);
            j.put("test", (raw[2] & 0x80) != 0);
            j.put("originator", lc.cotSize == 2 ? raw[3] & 0xff : 0);
            int at = 2 + lc.cotSize;
            int ca = raw[at] & 0xff;
            if (lc.caSize == 2) {
                ca |= (raw[at + 1] & 0xff) << 8;
            }
            j.put("commonAddress", ca);
            j.put("sequence", (raw[1] & 0x80) != 0);
            j.put("count", raw[1] & 0x7f);
        }
        j.put("objects", List.of());
        return j;
    }
}

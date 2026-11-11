// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import com.digitalpetri.iec60870.address.CommonAddress;
import com.digitalpetri.iec60870.address.InformationObjectAddress;
import com.digitalpetri.iec60870.address.OriginatorAddress;
import com.digitalpetri.iec60870.asdu.Asdu;
import com.digitalpetri.iec60870.asdu.AsduType;
import com.digitalpetri.iec60870.asdu.Cause;
import com.digitalpetri.iec60870.asdu.InformationObject;
import com.digitalpetri.iec60870.asdu.element.BinaryCounterReading;
import com.digitalpetri.iec60870.asdu.element.DoublePointState;
import com.digitalpetri.iec60870.asdu.element.NormalizedValue;
import com.digitalpetri.iec60870.asdu.element.Qds;
import com.digitalpetri.iec60870.asdu.element.Vti;
import com.digitalpetri.iec60870.asdu.object.Bitstring32;
import com.digitalpetri.iec60870.asdu.object.Bitstring32WithCp56Time;
import com.digitalpetri.iec60870.asdu.object.ClockSynchronizationCommand;
import com.digitalpetri.iec60870.asdu.object.CounterInterrogationCommand;
import com.digitalpetri.iec60870.asdu.object.DoubleCommand;
import com.digitalpetri.iec60870.asdu.object.DoublePointInformation;
import com.digitalpetri.iec60870.asdu.object.DoublePointWithCp56Time;
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
import com.digitalpetri.iec60870.asdu.object.SetpointNormalized;
import com.digitalpetri.iec60870.asdu.object.SetpointScaled;
import com.digitalpetri.iec60870.asdu.object.SetpointShortFloat;
import com.digitalpetri.iec60870.asdu.object.SingleCommand;
import com.digitalpetri.iec60870.asdu.object.SinglePointInformation;
import com.digitalpetri.iec60870.asdu.object.SinglePointWithCp56Time;
import com.digitalpetri.iec60870.asdu.object.StepPositionInformation;
import com.digitalpetri.iec60870.asdu.object.StepPositionWithCp56Time;
import com.digitalpetri.iec60870.asdu.time.Cp56Time2a;
import com.digitalpetri.iec60870.server.Iec60870Server;
import com.digitalpetri.iec60870.server.ServerContext;
import com.digitalpetri.iec60870.server.ServerEvent;
import com.digitalpetri.iec60870.server.ServerHandler;
import com.digitalpetri.iec60870.tcp.TcpIec101Server;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Instant;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Flow;
import java.util.function.Predicate;

/**
 * The controlled station of the contract, driven by a fixture, on digitalpetri's
 * TcpIec101Server. The library runs the FT 1.2 link (unbalanced secondary or
 * balanced station), decodes and encodes the ASDUs; the station answers every
 * request in the raw hook, since the fixture's behaviour (docs/FIXTURES.md) is
 * not the library's built-in station model.
 */
final class ServerCommand implements ServerHandler {
    /** Objects per ASDU when answering an interrogation. */
    private static final int MAX_OBJECTS = 20;

    private final Fixture fx;
    private final LinkConfig lc;
    private final int ca;
    private final Object lock = new Object();
    /** How long a command reply waits for the one before it to leave (LinkTap.Sent). */
    private static final long ORDER_WAIT_MS = 5_000;
    private final LinkTap.Sent sent = new LinkTap.Sent();

    private ServerCommand(Fixture fx, LinkConfig lc) {
        this.fx = fx;
        this.lc = lc;
        this.ca = fx.commonAddress;
    }

    static int run(Args a) throws Exception {
        String fixturePath = a.string("--fixture", Main.DEFAULT_FIXTURE);
        String bind = a.string("--bind-address", "0.0.0.0");
        String readyFile = a.string("--ready-file", Main.DEFAULT_READY_FILE);
        int port = a.integer("--port", Main.DEFAULT_PORT);
        LinkConfig lc = LinkConfig.parse(a);
        a.finish("server");
        if (port < 1 || port > 65535) {
            throw Args.usage("--port: %d is not a TCP port", port);
        }
        Fixture fx;
        try {
            fx = Fixture.load(Path.of(fixturePath));
        } catch (Fixture.InvalidException e) {
            Codec.log("fixture %s: %s", fixturePath, e.getMessage());
            return Main.EXIT_FIXTURE_INVALID;
        }
        if (lc.commonAddr != 0) {
            fx.commonAddress = lc.commonAddr;
        }
        String misfit = fitsFields(fx, lc);
        if (misfit != null) {
            Codec.log("fixture %s: %s", fixturePath, misfit);
            return Main.EXIT_FIXTURE_INVALID;
        }
        Files.deleteIfExists(Path.of(readyFile));

        ServerCommand station = new ServerCommand(fx, lc);
        Iec60870Server server = TcpIec101Server.builder()
                .bindAddress(bind)
                .port(port)
                .profile(lc.profile())
                .linkSettings(lc.settings(false))
                .maxConnections(1) // one line, one controlling station
                .handler(station)
                .serverBootstrapCustomizer(b -> {
                    // IEC101_TRACE=1: every frame of the line on stderr (LinkTap).
                    io.netty.channel.ChannelHandler library = b.config().childHandler();
                    b.childHandler(new io.netty.channel.ChannelInitializer<io.netty.channel.Channel>() {
                        @Override
                        protected void initChannel(io.netty.channel.Channel ch) {
                            ch.pipeline().addLast(library);
                            ch.pipeline().addFirst("link-tap", new LinkTap(lc.addrSize, () -> { }, station.sent));
                        }
                    });
                })
                .build();
        server.events().subscribe(new Flow.Subscriber<ServerEvent>() {
            @Override
            public void onSubscribe(Flow.Subscription s) {
                s.request(Long.MAX_VALUE);
            }

            @Override
            public void onNext(ServerEvent e) {
                if (e instanceof ServerEvent.ConnectionAccepted c) {
                    Map<String, Object> ev = event("connection-opened");
                    ev.put("peer", peer(c.remoteAddress()));
                    Codec.emit(ev);
                } else if (e instanceof ServerEvent.ConnectionClosed c) {
                    Map<String, Object> ev = event("connection-closed");
                    ev.put("peer", peer(c.remoteAddress()));
                    Codec.emit(ev);
                } else if (e instanceof ServerEvent.UndecodableAsdu u) {
                    Codec.log("undecodable ASDU from %s: %s", peer(u.remoteAddress()), u.cause().getMessage());
                }
            }

            @Override
            public void onError(Throwable t) {}

            @Override
            public void onComplete() {}
        });
        try {
            server.start();
        } catch (RuntimeException e) {
            Codec.log("cannot listen on %s:%d: %s", bind, port, e.getMessage());
            server.close();
            return Main.EXIT_OPERATION_FAILED;
        }
        try {
            Files.writeString(Path.of(readyFile), "");
        } catch (java.io.IOException e) {
            Codec.log("cannot write the ready file %s: %s", readyFile, e.getMessage());
            server.close();
            return Main.EXIT_OPERATION_FAILED;
        }
        Map<String, Object> ready = event("ready");
        ready.put("adapter", Main.ADAPTER);
        ready.put("address", bind + ":" + port);
        ready.put("fixture", fx.name);
        ready.put("commonAddress", fx.commonAddress);
        ready.put("standard", Main.STANDARD);
        ready.put("mode", lc.modeName());
        ready.put("linkAddress", lc.address);
        ready.put("linkAddressSize", lc.addrSize);
        ready.put("cotSize", lc.cotSize);
        ready.put("caSize", lc.caSize);
        ready.put("ioaSize", lc.ioaSize);
        Codec.emit(ready);

        // SIGTERM and SIGINT run the shutdown hook. The JVM would then exit with
        // 128+signal; the contract asks for 0 after a clean stop, hence halt(0).
        CountDownLatch forever = new CountDownLatch(1);
        Runtime.getRuntime().addShutdownHook(new Thread(() -> {
            try {
                Files.deleteIfExists(Path.of(readyFile));
            } catch (java.io.IOException ignored) {
                // the file is gone either way
            }
            try {
                server.close();
            } catch (RuntimeException e) {
                Codec.log("close: %s", e.getMessage());
            }
            Codec.emit(event("stopped"));
            Runtime.getRuntime().halt(Main.EXIT_OK);
        }));
        forever.await();
        return Main.EXIT_OK;
    }

    static Map<String, Object> event(String name) {
        Map<String, Object> e = Codec.object();
        e.put("event", name);
        return e;
    }

    static String peer(SocketAddress a) {
        if (a instanceof InetSocketAddress i && i.getAddress() != null) {
            return i.getAddress().getHostAddress() + ":" + i.getPort();
        }
        return String.valueOf(a);
    }

    /** Checks that every address of the fixture fits the field sizes; null when they do. */
    static String fitsFields(Fixture fx, LinkConfig lc) {
        long maxIoa = (1L << (8 * lc.ioaSize)) - 1;
        for (Fixture.Point p : fx.points) {
            if (p.ioa > maxIoa) {
                return "point " + p.ioa + " does not fit an information object address of " + lc.ioaSize + " octets";
            }
        }
        for (Fixture.Command c : fx.commands) {
            if (c.ioa > maxIoa) {
                return "command " + c.ioa + " does not fit an information object address of " + lc.ioaSize + " octets";
            }
        }
        if (fx.commonAddress >= lc.broadcastCA()) {
            return "common address " + fx.commonAddress + " does not fit a common address of " + lc.caSize + " octets";
        }
        return null;
    }

    // ---- the station ----

    private static final Cp56Time2a ZERO_TIME = null;

    private Asdu asdu(AsduType type, Cause cause, OriginatorAddress oa, List<InformationObject> objects) {
        return new Asdu(type, false, cause, false, false, oa, CommonAddress.of(ca), objects);
    }

    /** The request with another cause and P/N bit, from the station's common address. */
    private Asdu reply(Asdu r, Cause cause, boolean negative) {
        return new Asdu(r.type(), r.sequence(), cause, negative, r.test(), r.originatorAddress(),
                CommonAddress.of(ca), r.objects());
    }

    /** The request refused: P/N set, its own common address kept. */
    private static Asdu reject(Asdu r, Cause cause) {
        return new Asdu(r.type(), r.sequence(), cause, true, r.test(), r.originatorAddress(),
                r.commonAddress(), r.objects());
    }

    private static Qds qds(Fixture.Point p) {
        return new Qds(p.quality.contains("OV"), p.quality.contains("BL"), p.quality.contains("SB"),
                p.quality.contains("NT"), p.quality.contains("IV"));
    }

    private static AsduType typeOf(Fixture.Point p, boolean timed) {
        return switch (p.type) {
            case "M_SP_NA_1" -> timed ? AsduType.M_SP_TB_1 : AsduType.M_SP_NA_1;
            case "M_DP_NA_1" -> timed ? AsduType.M_DP_TB_1 : AsduType.M_DP_NA_1;
            case "M_ST_NA_1" -> timed ? AsduType.M_ST_TB_1 : AsduType.M_ST_NA_1;
            case "M_BO_NA_1" -> timed ? AsduType.M_BO_TB_1 : AsduType.M_BO_NA_1;
            case "M_ME_NA_1" -> timed ? AsduType.M_ME_TD_1 : AsduType.M_ME_NA_1;
            case "M_ME_NB_1" -> timed ? AsduType.M_ME_TE_1 : AsduType.M_ME_NB_1;
            case "M_ME_NC_1" -> timed ? AsduType.M_ME_TF_1 : AsduType.M_ME_NC_1;
            default -> timed ? AsduType.M_IT_TB_1 : AsduType.M_IT_NA_1;
        };
    }

    /** The information object of a point, with a CP56Time2a time tag when time is not null. */
    private static InformationObject pointObject(Fixture.Point p, Cp56Time2a time) {
        InformationObjectAddress ioa = InformationObjectAddress.of(p.ioa);
        Qds q = qds(p);
        boolean timed = time != null;
        return switch (p.type) {
            case "M_SP_NA_1" -> timed ? new SinglePointWithCp56Time(ioa, p.value != 0, q, time)
                    : new SinglePointInformation(ioa, p.value != 0, q);
            case "M_DP_NA_1" -> timed
                    ? new DoublePointWithCp56Time(ioa, DoublePointState.fromValue((int) p.value), q, time)
                    : new DoublePointInformation(ioa, DoublePointState.fromValue((int) p.value), q);
            case "M_ST_NA_1" -> timed
                    ? new StepPositionWithCp56Time(ioa, new Vti((int) p.value, p.transientState), q, time)
                    : new StepPositionInformation(ioa, new Vti((int) p.value, p.transientState), q);
            case "M_BO_NA_1" -> timed ? new Bitstring32WithCp56Time(ioa, (int) (long) p.value, q, time)
                    : new Bitstring32(ioa, (int) (long) p.value, q);
            case "M_ME_NA_1" -> timed
                    ? new MeasuredValueNormalizedWithCp56Time(ioa, new NormalizedValue((short) p.value), q, time)
                    : new MeasuredValueNormalized(ioa, new NormalizedValue((short) p.value), q);
            case "M_ME_NB_1" -> timed ? new MeasuredValueScaledWithCp56Time(ioa, (short) p.value, q, time)
                    : new MeasuredValueScaled(ioa, (short) p.value, q);
            case "M_ME_NC_1" -> timed ? new MeasuredValueShortFloatWithCp56Time(ioa, (float) p.value, q, time)
                    : new MeasuredValueShortFloat(ioa, (float) p.value, q);
            default -> {
                BinaryCounterReading c = new BinaryCounterReading((int) p.value, p.sequence,
                        p.quality.contains("CY"), p.quality.contains("CA"), p.quality.contains("IV"));
                yield timed ? new IntegratedTotalsWithCp56Time(ioa, c, time) : new IntegratedTotals(ioa, c);
            }
        };
    }

    /** The points selected, ascending, consecutive points of one type sharing an ASDU. */
    private List<Asdu> pointAsdus(Cause cause, OriginatorAddress oa, Predicate<Fixture.Point> selected) {
        List<Asdu> out = new ArrayList<>();
        List<InformationObject> batch = new ArrayList<>();
        AsduType batchType = null;
        synchronized (lock) {
            for (Fixture.Point p : fx.points) {
                if (!selected.test(p)) {
                    continue;
                }
                AsduType t = typeOf(p, false);
                if (!batch.isEmpty() && (t != batchType || batch.size() >= MAX_OBJECTS)) {
                    out.add(asdu(batchType, cause, oa, batch));
                    batch = new ArrayList<>();
                }
                batchType = t;
                batch.add(pointObject(p, ZERO_TIME));
            }
        }
        if (!batch.isEmpty()) {
            out.add(asdu(batchType, cause, oa, batch));
        }
        return out;
    }

    @Override
    public boolean onRawAsdu(ServerContext ctx, Asdu a) {
        try {
            handle(ctx, a);
        } catch (RuntimeException e) {
            Codec.log("cannot answer %s: %s", a.type(), e);
        }
        return true; // every request is the fixture's, never the library's station model
    }

    private void handle(ServerContext ctx, Asdu a) {
        int requestCA = a.commonAddress().value().intValue();
        if (requestCA != ca && requestCA != lc.broadcastCA()) {
            ctx.send(reject(a, Cause.UNKNOWN_COMMON_ADDRESS));
            return;
        }
        boolean one = a.objects().size() == 1;
        switch (a.type()) {
            case C_IC_NA_1 -> {
                if (a.cause() != Cause.ACTIVATION || !one) {
                    ctx.send(reject(a, Cause.UNKNOWN_CAUSE));
                    return;
                }
                int qoi = ((InterrogationCommand) a.objects().get(0)).qualifier().value().intValue();
                int group = qoi - 20;
                boolean accepted = qoi == 20 || group >= 1 && group <= 16 && fx.hasGroup(group);
                Map<String, Object> e = event("interrogation");
                e.put("commonAddress", requestCA);
                e.put("qoi", qoi);
                e.put("accepted", accepted);
                Codec.emit(e);
                ctx.send(reply(a, Cause.ACTIVATION_CONFIRMATION, !accepted));
                if (accepted) {
                    Cause cause = Cause.fromValue(qoi);
                    for (Asdu d : pointAsdus(cause, a.originatorAddress(),
                            p -> group == 0 ? !p.type.equals("M_IT_NA_1") : p.groups.contains(group))) {
                        ctx.send(d);
                    }
                    ctx.send(reply(a, Cause.ACTIVATION_TERMINATION, false));
                }
            }
            case C_CI_NA_1 -> {
                if (a.cause() != Cause.ACTIVATION || !one) {
                    ctx.send(reject(a, Cause.UNKNOWN_CAUSE));
                    return;
                }
                CounterInterrogationCommand c = (CounterInterrogationCommand) a.objects().get(0);
                int qcc = c.qualifier().request() | (c.qualifier().freeze().value() << 6);
                boolean accepted = qcc == 5; // general request counter, read
                Map<String, Object> e = event("counter-interrogation");
                e.put("commonAddress", requestCA);
                e.put("qcc", qcc);
                e.put("accepted", accepted);
                Codec.emit(e);
                ctx.send(reply(a, Cause.ACTIVATION_CONFIRMATION, !accepted));
                if (accepted) {
                    for (Asdu d : pointAsdus(Cause.REQUESTED_BY_GENERAL_COUNTER, a.originatorAddress(),
                            p -> p.type.equals("M_IT_NA_1"))) {
                        ctx.send(d);
                    }
                    ctx.send(reply(a, Cause.ACTIVATION_TERMINATION, false));
                }
            }
            case C_RD_NA_1 -> {
                if (a.cause() != Cause.REQUEST || !one) {
                    ctx.send(reject(a, Cause.UNKNOWN_CAUSE));
                    return;
                }
                int ioa = a.objects().get(0).address().value().intValue();
                Asdu answer = null;
                synchronized (lock) {
                    Fixture.Point p = fx.point(ioa);
                    if (p != null) {
                        answer = asdu(typeOf(p, false), Cause.REQUEST, a.originatorAddress(),
                                List.of(pointObject(p, ZERO_TIME)));
                    }
                }
                Map<String, Object> e = event("read");
                e.put("ioa", ioa);
                e.put("accepted", answer != null);
                Codec.emit(e);
                ctx.send(answer != null ? answer : reject(a, Cause.UNKNOWN_INFORMATION_OBJECT_ADDRESS));
            }
            case C_CS_NA_1 -> {
                if (a.cause() != Cause.ACTIVATION || !one) {
                    ctx.send(reject(a, Cause.UNKNOWN_CAUSE));
                    return;
                }
                Map<String, Object> e = event("clock-sync");
                e.put("time", Codec.formatTime(((ClockSynchronizationCommand) a.objects().get(0)).time()));
                Codec.emit(e);
                ctx.send(reply(a, Cause.ACTIVATION_CONFIRMATION, false));
            }
            case C_TS_NA_1 -> {
                if (a.cause() != Cause.ACTIVATION) {
                    ctx.send(reject(a, Cause.UNKNOWN_CAUSE));
                    return;
                }
                ctx.send(reply(a, Cause.ACTIVATION_CONFIRMATION, false));
            }
            case C_SC_NA_1, C_DC_NA_1, C_RC_NA_1, C_SE_NA_1, C_SE_NB_1, C_SE_NC_1 -> {
                if (a.cause() != Cause.ACTIVATION && a.cause() != Cause.DEACTIVATION || !one) {
                    ctx.send(reject(a, Cause.UNKNOWN_CAUSE));
                    return;
                }
                command(ctx, a);
            }
            // Includes the CP56Time2a command types and C_TS_TA_1, which IEC 101 does not have.
            default -> ctx.send(reject(a, Cause.UNKNOWN_TYPE_ID));
        }
    }

    private void command(ServerContext ctx, Asdu a) {
        InformationObject o = a.objects().get(0);
        int ioa = o.address().value().intValue();
        boolean select;
        boolean valid = true;
        Object eventValue;
        double value;
        switch (o) {
            case SingleCommand c -> {
                select = c.qualifier().select();
                eventValue = c.on();
                value = c.on() ? 1 : 0;
            }
            case DoubleCommand c -> {
                select = c.qualifier().select();
                eventValue = c.state().value();
                value = c.state().value();
                valid = value == 1 || value == 2; // 0 and 3 are not permitted (7.2.6.16)
            }
            case RegulatingStepCommand c -> {
                select = c.qualifier().select();
                eventValue = c.state().value();
                value = c.state().value();
                valid = value == 1 || value == 2; // 0 and 3 are not permitted (7.2.6.17)
            }
            case SetpointNormalized c -> {
                select = c.qualifier().select();
                eventValue = (int) c.value().rawValue();
                value = c.value().rawValue();
            }
            case SetpointScaled c -> {
                select = c.qualifier().select();
                eventValue = (int) c.value();
                value = c.value();
            }
            case SetpointShortFloat c -> {
                select = c.qualifier().select();
                eventValue = (double) c.value();
                value = c.value();
            }
            default -> {
                ctx.send(reject(a, Cause.UNKNOWN_TYPE_ID));
                return;
            }
        }
        Map<String, Object> e = event("command");
        e.put("type", a.type().name());
        e.put("ioa", ioa);
        e.put("cot", a.cause().value());
        String outcome;
        Asdu report = null;
        synchronized (lock) {
            Fixture.Command cmd = fx.command(ioa);
            if (cmd == null || !cmd.type.equals(a.type().name())) {
                e.put("outcome", "unknown-ioa");
                Codec.emit(e);
                ctx.send(reject(a, Cause.UNKNOWN_INFORMATION_OBJECT_ADDRESS));
                return;
            }
            e.put("value", eventValue);
            e.put("select", select);
            if (a.cause() == Cause.DEACTIVATION) {
                cmd.selected = false;
                outcome = "deactivated";
            } else if (!valid) {
                outcome = "rejected";
            } else if (select) {
                cmd.selected = true;
                outcome = "selected";
            } else if (cmd.selectRequired && !cmd.selected) {
                outcome = "rejected";
            } else {
                cmd.selected = false;
                Fixture.Point t = cmd.target;
                double next = value;
                if (cmd.type.equals("C_RC_NA_1")) {
                    next = value == 2 ? t.value + 1 : t.value - 1;
                }
                if (cmd.type.equals("C_RC_NA_1") && (next < -64 || next > 63)) {
                    outcome = "rejected"; // the step position is at its limit
                } else {
                    t.value = next;
                    Cp56Time2a now = Cp56Time2a.from(Instant.now(), ZoneOffset.UTC);
                    report = asdu(typeOf(t, true), Cause.fromValue(cmd.reportCause), OriginatorAddress.none(),
                            List.of(pointObject(t, now)));
                    outcome = "executed";
                }
            }
        }
        e.put("outcome", outcome);
        Codec.emit(e);
        switch (outcome) {
            case "deactivated" -> ctx.send(reply(a, Cause.DEACTIVATION_CONFIRMATION, false));
            case "rejected" -> ctx.send(reply(a, Cause.ACTIVATION_CONFIRMATION, true));
            default -> {
                long mark = sent.mark();
                ctx.send(reply(a, Cause.ACTIVATION_CONFIRMATION, false));
                if (report != null) {
                    // Confirmation, then the new state of the target, then termination.
                    // The report is class 1 and the others class 2 in an unbalanced
                    // link (LinkTap.Sent): each waits until the one before has left.
                    // Bounded, so a controlling station that stops polling ends it.
                    int type = a.type().typeId();
                    sent.await(mark, type, Cause.ACTIVATION_CONFIRMATION.value(), ORDER_WAIT_MS);
                    mark = sent.mark();
                    ctx.send(report);
                    sent.await(mark, report.type().typeId(), report.cause().value(), ORDER_WAIT_MS);
                    ctx.send(reply(a, Cause.ACTIVATION_TERMINATION, false));
                }
            }
        }
    }
}

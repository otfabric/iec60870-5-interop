// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import com.digitalpetri.iec60870.ProtocolTimeoutException;
import com.digitalpetri.iec60870.address.CommonAddress;
import com.digitalpetri.iec60870.address.InformationObjectAddress;
import com.digitalpetri.iec60870.address.OriginatorAddress;
import com.digitalpetri.iec60870.asdu.Asdu;
import com.digitalpetri.iec60870.asdu.AsduType;
import com.digitalpetri.iec60870.asdu.Cause;
import com.digitalpetri.iec60870.asdu.InformationObject;
import com.digitalpetri.iec60870.asdu.element.DoubleCommandState;
import com.digitalpetri.iec60870.asdu.element.FixedTestBitPattern;
import com.digitalpetri.iec60870.asdu.element.FreezeMode;
import com.digitalpetri.iec60870.asdu.element.NormalizedValue;
import com.digitalpetri.iec60870.asdu.element.QualifierOfCommand;
import com.digitalpetri.iec60870.asdu.element.QualifierOfCounterInterrogation;
import com.digitalpetri.iec60870.asdu.element.QualifierOfInterrogation;
import com.digitalpetri.iec60870.asdu.element.QualifierOfSetpoint;
import com.digitalpetri.iec60870.asdu.element.StepCommandState;
import com.digitalpetri.iec60870.asdu.object.ClockSynchronizationCommand;
import com.digitalpetri.iec60870.asdu.object.CounterInterrogationCommand;
import com.digitalpetri.iec60870.asdu.object.DoubleCommand;
import com.digitalpetri.iec60870.asdu.object.InterrogationCommand;
import com.digitalpetri.iec60870.asdu.object.ReadCommand;
import com.digitalpetri.iec60870.asdu.object.RegulatingStepCommand;
import com.digitalpetri.iec60870.asdu.object.SetpointNormalized;
import com.digitalpetri.iec60870.asdu.object.SetpointScaled;
import com.digitalpetri.iec60870.asdu.object.SetpointShortFloat;
import com.digitalpetri.iec60870.asdu.object.SingleCommand;
import com.digitalpetri.iec60870.asdu.object.TestCommand;
import com.digitalpetri.iec60870.asdu.time.Cp56Time2a;
import com.digitalpetri.iec60870.client.ClientEvent;
import com.digitalpetri.iec60870.client.Iec60870Client;
import com.digitalpetri.iec60870.client.ReconnectPolicy;
import com.digitalpetri.iec60870.tcp.TcpIec101Client;
import io.netty.channel.Channel;
import io.netty.channel.ChannelHandler;
import io.netty.channel.ChannelInitializer;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneOffset;
import java.time.format.DateTimeParseException;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Flow;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.function.BooleanSupplier;

/**
 * The controlling station of the contract: one bounded operation per invocation,
 * on digitalpetri's TcpIec101Client. The library starts the link (and in
 * unbalanced mode polls the station); the operation sends its ASDUs itself with
 * {@code Iec60870Client.send} and reads every answer from the client's events,
 * rather than the library's request methods, which answer for themselves what
 * the contract wants to see.
 */
final class ClientCommand {
    /** A failed operation: it becomes the error object of the result. */
    static final class Failure extends Exception {
        private static final long serialVersionUID = 1L;
        final String code;

        Failure(String code, String message) {
            super(message);
            this.code = code;
        }
    }

    /** What the receive side shares with the operation. */
    private final List<Map<String, Object>> asdus = new ArrayList<>();
    private final List<Map<String, Object>> confirmations = new ArrayList<>();
    private boolean closed;
    private Throwable closeCause;
    private boolean linkUp;
    private boolean terminated;
    private int negativeCot;
    private boolean readAnswered;
    private AsduType expectType;
    private boolean expectRead;
    private int expectIoa;

    private synchronized void update(Runnable r) {
        r.run();
        notifyAll();
    }

    /** Waits until cond holds, the connection closes or the timeout passes. */
    private synchronized boolean waitFor(BooleanSupplier cond, long timeoutMs) {
        long deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs);
        while (!cond.getAsBoolean() && !closed) {
            long left = deadline - System.nanoTime();
            if (left <= 0) {
                break;
            }
            try {
                TimeUnit.NANOSECONDS.timedWait(this, left);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                break;
            }
        }
        return cond.getAsBoolean();
    }

    private synchronized boolean isClosed() {
        return closed;
    }

    private void onAsdu(Asdu a) {
        Map<String, Object> doc = Codec.asduJson(a);
        int cot = a.cause().value();
        update(() -> {
            asdus.add(doc);
            if (expectType != null && a.type() == expectType) {
                boolean unknown = cot >= 44 && cot <= 47;
                if (cot == 7 || cot == 9 || unknown) {
                    boolean negative = a.negative() || unknown;
                    Map<String, Object> c = Codec.object();
                    c.put("cot", cot);
                    c.put("negative", negative);
                    confirmations.add(c);
                    if (negative) {
                        negativeCot = cot;
                    }
                } else if (cot == 10) {
                    terminated = true;
                }
            } else if (expectRead && cot == 5) {
                for (InformationObject o : a.objects()) {
                    if (o.address().value().intValue() == expectIoa) {
                        readAnswered = true;
                    }
                }
            }
        });
    }

    private void awaitConfirmation(int n, long timeoutMs) throws Failure {
        if (!waitFor(() -> confirmations.size() >= n, timeoutMs)) {
            if (isClosed()) {
                throw new Failure("connection-lost", "connection closed while waiting for confirmation " + n);
            }
            throw new Failure("timeout", "no confirmation " + n + " from the station");
        }
        synchronized (this) {
            if (negativeCot != 0) {
                throw new Failure("negative-confirmation", "the station refused the request (cause " + negativeCot + ")");
            }
        }
    }

    private void awaitTermination(long timeoutMs) throws Failure {
        if (!waitFor(() -> terminated || negativeCot != 0, timeoutMs)) {
            if (isClosed()) {
                throw new Failure("connection-lost", "connection closed while waiting for termination");
            }
            throw new Failure("timeout", "no activation termination from the station");
        }
    }

    // ---- arguments ----

    private static boolean parseBool(String s, boolean[] out) {
        switch (s.toLowerCase()) {
            case "true", "1", "on" -> out[0] = true;
            case "false", "0", "off" -> out[0] = false;
            default -> {
                return false;
            }
        }
        return true;
    }

    /** A process command of the contract, built with either state of the select bit. */
    private interface ProcessCommand {
        InformationObject object(boolean select);
    }

    private static ProcessCommand parseCommand(Args a, AsduType[] type, int[] ioaOut) {
        String typeArg = a.string("--type", "");
        String value = a.string("--value", "");
        int ioa = a.integer("--ioa", 0);
        int qualifier = a.integer("--qualifier", 0);
        AsduType t;
        try {
            t = switch (typeArg) {
                case "C_SC_NA_1", "C_DC_NA_1", "C_RC_NA_1", "C_SE_NA_1", "C_SE_NB_1", "C_SE_NC_1" -> AsduType.valueOf(typeArg);
                default -> null;
            };
        } catch (IllegalArgumentException e) {
            t = null;
        }
        if (t == null || ioa < 1 || value.isEmpty()) {
            throw Args.usage("command: --type (C_SC_NA_1, C_DC_NA_1, C_RC_NA_1, C_SE_NA_1, C_SE_NB_1, "
                    + "C_SE_NC_1), --ioa and --value are required");
        }
        type[0] = t;
        ioaOut[0] = ioa;
        InformationObjectAddress addr = InformationObjectAddress.of(ioa);
        boolean isSetpoint = t == AsduType.C_SE_NA_1 || t == AsduType.C_SE_NB_1 || t == AsduType.C_SE_NC_1;
        if (qualifier < 0 || qualifier > (isSetpoint ? 127 : 31)) {
            throw Args.usage("command: --qualifier must be 0..%d for %s", isSetpoint ? 127 : 31, typeArg);
        }
        RuntimeException bad = Args.usage("command: --value '%s' is not valid for %s", value, typeArg);
        switch (t) {
            case C_SC_NA_1 -> {
                boolean[] b = new boolean[1];
                if (!parseBool(value, b)) {
                    throw bad;
                }
                return select -> new SingleCommand(addr, b[0], new QualifierOfCommand(qualifier, select));
            }
            case C_DC_NA_1, C_RC_NA_1 -> {
                int n = parseInt(value, bad);
                if (n < 0 || n > 3) {
                    throw bad;
                }
                if (t == AsduType.C_DC_NA_1) {
                    return select -> new DoubleCommand(addr, DoubleCommandState.fromValue(n),
                            new QualifierOfCommand(qualifier, select));
                }
                return select -> new RegulatingStepCommand(addr, StepCommandState.fromValue(n),
                        new QualifierOfCommand(qualifier, select));
            }
            case C_SE_NA_1 -> {
                int n = parseInt(value, bad);
                if (n < -32768 || n > 32767) {
                    throw bad;
                }
                return select -> new SetpointNormalized(addr, new NormalizedValue((short) n),
                        new QualifierOfSetpoint(qualifier, select));
            }
            case C_SE_NB_1 -> {
                int n = parseInt(value, bad);
                if (n < -32768 || n > 32767) {
                    throw bad;
                }
                return select -> new SetpointScaled(addr, (short) n, new QualifierOfSetpoint(qualifier, select));
            }
            default -> {
                float f;
                try {
                    f = Float.parseFloat(value);
                } catch (NumberFormatException e) {
                    throw bad;
                }
                if (Float.isNaN(f) || Float.isInfinite(f)) {
                    throw bad;
                }
                return select -> new SetpointShortFloat(addr, f, new QualifierOfSetpoint(qualifier, select));
            }
        }
    }

    private static int parseInt(String s, RuntimeException bad) {
        try {
            return Integer.parseInt(s);
        } catch (NumberFormatException e) {
            throw bad;
        }
    }

    // ---- the operation ----

    static int run(String op, Args a) {
        return new ClientCommand().runOperation(op, a);
    }

    private int runOperation(String op, Args a) {
        String host = a.string("--host", "");
        int port = a.integer("--port", Main.DEFAULT_PORT);
        int oa = a.integer("--originator-address", 0);
        int timeoutMs = a.integer("--timeout-ms", 5000);
        int connectMs = a.integer("--connect-timeout-ms", 5000);
        int collectMs = a.integer("--collect-ms", 0);
        LinkConfig lc = LinkConfig.parse(a);
        int ca = lc.commonAddr == 0 ? 1 : lc.commonAddr;
        if (oa < 0 || oa > 255) {
            throw Args.usage("--originator-address: must be 0..255");
        }

        // Operation flags, parsed before connecting so that a usage error never
        // touches the network.
        int qoi = 0;
        int qcc = 0;
        int ioa = 0;
        int holdMs = 0;
        int durationMs = 0;
        int maxAsdus = 0;
        Instant clock = Instant.now();
        ProcessCommand cmd = null;
        AsduType[] cmdType = new AsduType[1];
        String mode = "direct";
        switch (op) {
            case "connect" -> holdMs = a.integer("--hold-ms", 0);
            case "interrogate" -> qoi = a.integer("--qoi", 20);
            case "counter-interrogate" -> qcc = a.integer("--qcc", 5);
            case "read" -> {
                ioa = a.integer("--ioa", 0);
                if (ioa < 1) {
                    throw Args.usage("read: --ioa is required");
                }
            }
            case "clock-sync" -> {
                String t = a.string("--time", null);
                if (t != null) {
                    try {
                        clock = Instant.from(Codec.TIME.parse(t));
                    } catch (DateTimeParseException e) {
                        throw Args.usage("clock-sync: --time must look like 2026-01-02T03:04:05.678Z");
                    }
                }
            }
            case "test-command" -> {
                // no flags
            }
            case "command" -> {
                mode = a.string("--mode", "direct");
                if (!List.of("direct", "select", "sbo", "cancel").contains(mode)) {
                    throw Args.usage("command: --mode must be direct, select, sbo or cancel");
                }
                int[] cmdIoa = new int[1];
                cmd = parseCommand(a, cmdType, cmdIoa);
                ioa = cmdIoa[0];
            }
            case "monitor" -> {
                durationMs = a.integer("--duration-ms", 1000);
                maxAsdus = a.integer("--max-asdus", 0);
            }
            default -> throw Args.usage("client: unknown operation %s", op);
        }
        if (host.isEmpty()) {
            throw Args.usage("client: --host is required");
        }
        a.finish("client " + op);
        if (qoi < 0 || qoi > 255 || qcc < 0 || qcc > 255) {
            throw Args.usage("--qoi and --qcc are one octet");
        }
        if (ioa >= (1L << (8 * lc.ioaSize))) {
            throw Args.usage("--ioa: does not fit an information object address of %d octets", lc.ioaSize);
        }
        Cp56Time2a clockTime;
        try {
            clockTime = Cp56Time2a.from(clock, ZoneOffset.UTC);
        } catch (IllegalArgumentException e) {
            throw Args.usage("clock-sync: %s", e.getMessage());
        }
        // digitalpetri's unbalanced master sends an ASDU to the station whose link
        // address equals the ASDU's common address (and one with the broadcast
        // link address as SEND/NO REPLY to all stations): another common address
        // over this station's link is something it cannot send.
        if (!lc.balanced && ca != lc.address && !op.equals("connect") && !op.equals("monitor")) {
            throw Args.usage("--common-address: in unbalanced mode digitalpetri/iec60870 sends a request to the "
                    + "station whose link address equals its common address; %d is not --link-address %d "
                    + "(feature unbalancedCommonAddress is false)", ca, lc.address);
        }

        long begin = System.nanoTime();
        int[] exitCode = {Main.EXIT_OPERATION_FAILED};
        boolean[] connected = {false};
        Iec60870Client client = null;
        Failure failure = null;
        try {
            LinkTap tap = new LinkTap(lc.addrSize, () -> update(() -> linkUp = true));
            client = TcpIec101Client.builder()
                    .host(host)
                    .port(port)
                    .connectTimeout(Duration.ofMillis(Math.max(1, connectMs)))
                    .profile(lc.profile())
                    .linkSettings(lc.settings(true))
                    .startDataTransferOnConnect(false)
                    .originatorAddress(OriginatorAddress.of(oa))
                    .reconnectPolicy(ReconnectPolicy.disabled())
                    .bootstrapCustomizer(b -> {
                        ChannelHandler library = b.config().handler();
                        b.handler(new ChannelInitializer<Channel>() {
                            @Override
                            protected void initChannel(Channel ch) {
                                ch.pipeline().addLast(library);
                                ch.pipeline().addFirst("link-tap", tap);
                            }
                        });
                    })
                    .build();
            client.events().subscribe(new Flow.Subscriber<ClientEvent>() {
                @Override
                public void onSubscribe(Flow.Subscription s) {
                    s.request(Long.MAX_VALUE);
                }

                @Override
                public void onNext(ClientEvent e) {
                    if (e instanceof ClientEvent.AsduReceived r) {
                        onAsdu(r.asdu());
                    } else if (e instanceof ClientEvent.UndecodableAsdu u) {
                        Map<String, Object> doc = Codec.rawAsduJson(u.asdu(), lc);
                        update(() -> asdus.add(doc));
                    } else if (e instanceof ClientEvent.ConnectionClosed c) {
                        update(() -> {
                            closed = true;
                            closeCause = c.cause();
                        });
                    }
                }

                @Override
                public void onError(Throwable t) {
                    update(() -> closed = true);
                }

                @Override
                public void onComplete() {
                    update(() -> closed = true);
                }
            });
            operation(client, op, lc, ca, oa, timeoutMs, holdMs, durationMs, maxAsdus, qoi, qcc, ioa, clockTime,
                    cmd, cmdType[0], mode, collectMs, exitCode, connected);
        } catch (Failure f) {
            failure = f;
        } catch (RuntimeException e) {
            failure = new Failure("failed", String.valueOf(e));
        } finally {
            if (client != null) {
                try {
                    client.close();
                } catch (RuntimeException e) {
                    Codec.log("close: %s", e);
                }
            }
        }

        Map<String, Object> r = Codec.object();
        r.put("schemaVersion", Main.SCHEMA_VERSION);
        r.put("adapter", Main.ADAPTER);
        r.put("operation", op);
        r.put("ok", failure == null);
        if (failure == null) {
            r.put("error", null);
        } else {
            Map<String, Object> err = Codec.object();
            err.put("code", failure.code);
            err.put("message", failure.getMessage());
            r.put("error", err);
        }
        synchronized (this) {
            r.put("connected", connected[0]);
            r.put("confirmations", new ArrayList<>(confirmations));
            r.put("terminated", terminated);
            r.put("asdus", new ArrayList<>(asdus));
        }
        r.put("elapsedMs", TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - begin));
        Codec.emit(r);
        return exitCode[0];
    }

    private void send(Iec60870Client client, AsduType t, Cause c, int ca, int oa, InformationObject o, long timeoutMs)
            throws Failure {
        Asdu asdu = new Asdu(t, false, c, false, false, OriginatorAddress.of(oa), CommonAddress.of(ca), List.of(o));
        CompletableFuture<Void> sent = client.sendAsync(asdu).toCompletableFuture();
        try {
            sent.get(timeoutMs, TimeUnit.MILLISECONDS);
        } catch (TimeoutException e) {
            throw new Failure(isClosed() ? "connection-lost" : "timeout", "the station's link did not take the request");
        } catch (ExecutionException e) {
            throw new Failure(isClosed() ? "connection-lost" : "failed", "send: " + e.getCause());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new Failure("failed", "interrupted");
        }
    }

    @SuppressWarnings("checkstyle:ParameterNumber")
    private void operation(Iec60870Client client, String op, LinkConfig lc, int ca, int oa, long timeoutMs,
            int holdMs, int durationMs, int maxAsdus, int qoi, int qcc, int ioa, Cp56Time2a clock,
            ProcessCommand cmd, AsduType cmdType, String mode, int collectMs, int[] exitCode, boolean[] connected)
            throws Failure {
        try {
            client.connect();
        } catch (RuntimeException e) {
            exitCode[0] = Main.EXIT_CONNECT_FAILED;
            throw new Failure("connect-failed", String.valueOf(e.getCause() != null ? e.getCause() : e));
        }
        // The link start-up: request status of link, reset of remote link. The
        // balanced link gives up after its retries and closes the session with a
        // ProtocolTimeoutException, which is a link that did not start, not a
        // lost connection.
        Throwable startError = null;
        try {
            client.startDataTransferAsync().toCompletableFuture().get(timeoutMs, TimeUnit.MILLISECONDS);
        } catch (TimeoutException e) {
            // judged below by the reset's confirmation
        } catch (ExecutionException e) {
            startError = e.getCause();
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new Failure("failed", "interrupted");
        }
        if (startError != null || !waitFor(() -> linkUp, timeoutMs)) {
            exitCode[0] = Main.EXIT_CONNECT_FAILED;
            boolean gaveUp = false;
            Throwable cause;
            synchronized (this) {
                cause = closeCause;
            }
            for (Throwable e : new Throwable[] {startError, cause}) {
                for (Throwable t = e; t != null; t = t.getCause()) {
                    gaveUp |= t instanceof ProtocolTimeoutException;
                }
            }
            if (isClosed() && !gaveUp) {
                throw new Failure("connection-lost", "the line closed during the link start-up");
            }
            throw new Failure("link-init-timeout", "the reset of the remote link was not confirmed"
                    + (startError != null ? ": " + startError : ""));
        }
        connected[0] = true;

        switch (op) {
            case "connect" -> {
                if (holdMs > 0) {
                    waitFor(() -> false, holdMs);
                }
                if (isClosed()) {
                    throw new Failure("connection-lost", "the line closed while it was held");
                }
            }
            case "interrogate" -> {
                update(() -> expectType = AsduType.C_IC_NA_1);
                send(client, AsduType.C_IC_NA_1, Cause.ACTIVATION, ca, oa,
                        new InterrogationCommand(InformationObjectAddress.of(0), QualifierOfInterrogation.of(qoi)),
                        timeoutMs);
                awaitConfirmation(1, timeoutMs);
                awaitTermination(timeoutMs);
            }
            case "counter-interrogate" -> {
                update(() -> expectType = AsduType.C_CI_NA_1);
                send(client, AsduType.C_CI_NA_1, Cause.ACTIVATION, ca, oa,
                        new CounterInterrogationCommand(InformationObjectAddress.of(0),
                                new QualifierOfCounterInterrogation(qcc & 0x3f, FreezeMode.fromValue(qcc >> 6))),
                        timeoutMs);
                awaitConfirmation(1, timeoutMs);
                awaitTermination(timeoutMs);
            }
            case "read" -> {
                update(() -> {
                    expectType = AsduType.C_RD_NA_1;
                    expectRead = true;
                    expectIoa = ioa;
                });
                send(client, AsduType.C_RD_NA_1, Cause.REQUEST, ca, oa,
                        new ReadCommand(InformationObjectAddress.of(ioa)), timeoutMs);
                if (!waitFor(() -> readAnswered || negativeCot != 0, timeoutMs)) {
                    if (isClosed()) {
                        throw new Failure("connection-lost", "connection closed while waiting for the answer");
                    }
                    throw new Failure("timeout", "no answer to the read command");
                }
                synchronized (this) {
                    if (negativeCot != 0) {
                        throw new Failure("negative-confirmation",
                                "the station refused the request (cause " + negativeCot + ")");
                    }
                }
            }
            case "clock-sync" -> {
                update(() -> expectType = AsduType.C_CS_NA_1);
                send(client, AsduType.C_CS_NA_1, Cause.ACTIVATION, ca, oa,
                        new ClockSynchronizationCommand(InformationObjectAddress.of(0), clock), timeoutMs);
                awaitConfirmation(1, timeoutMs);
            }
            case "test-command" -> {
                update(() -> expectType = AsduType.C_TS_NA_1);
                // FBP: the fixed test bit pattern 55AA (7.2.6.14).
                send(client, AsduType.C_TS_NA_1, Cause.ACTIVATION, ca, oa,
                        new TestCommand(InformationObjectAddress.of(0), FixedTestBitPattern.DEFAULT), timeoutMs);
                awaitConfirmation(1, timeoutMs);
            }
            case "command" -> {
                update(() -> expectType = cmdType);
                int n = 0;
                if (!mode.equals("direct")) {
                    send(client, cmdType, Cause.ACTIVATION, ca, oa, cmd.object(true), timeoutMs);
                    awaitConfirmation(++n, timeoutMs);
                    if (mode.equals("cancel")) {
                        send(client, cmdType, Cause.DEACTIVATION, ca, oa, cmd.object(true), timeoutMs);
                        awaitConfirmation(++n, timeoutMs);
                    }
                }
                if (mode.equals("direct") || mode.equals("sbo")) {
                    send(client, cmdType, Cause.ACTIVATION, ca, oa, cmd.object(false), timeoutMs);
                    awaitConfirmation(++n, timeoutMs);
                    awaitTermination(timeoutMs);
                }
            }
            default -> { // monitor
                waitFor(() -> maxAsdus > 0 && asdus.size() >= maxAsdus, durationMs);
                if (isClosed()) {
                    throw new Failure("connection-lost", "connection closed while monitoring");
                }
            }
        }
        if (collectMs > 0) {
            waitFor(() -> false, collectMs);
        }
        exitCode[0] = Main.EXIT_OK;
    }
}

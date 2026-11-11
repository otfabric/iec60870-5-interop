/* SPDX-License-Identifier: GPL-3.0-or-later */
/*
 * The controlling station: a lib60870-C CS101 master (unbalanced primary, or
 * balanced station A) on the pty of bridge.c, connected to the station over
 * TCP. Everything runs on this thread: CS101_Master_run and the polling are
 * called from the wait loop, and the handlers fill the state below.
 */
#include "common.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <time.h>

#include "hal_thread.h"
#include "hal_time.h"

static struct {
    cJSON* asdus;
    cJSON* confirmations;
    int asduCount;
    bool linkUp;      /* the reset of the remote link was confirmed */
    bool terminated;
    int negativeCot;
    bool readAnswered;
    IEC60870_5_TypeID expectType;
    int expectIoa;
    bool expectRead;
} st;

static CS101_Master master;
static Bridge* bridge;
static LinkArgs lnk;
static uint64_t lastPoll;

static void linkStateHandler(void* parameter, int address, LinkLayerState state)
{
    (void)parameter;
    (void)address;
    if (state == LL_STATE_AVAILABLE)
        st.linkUp = true;
}

static bool asduHandler(void* parameter, int address, CS101_ASDU asdu)
{
    (void)parameter;
    (void)address;
    cJSON_AddItemToArray(st.asdus, asduToJson(asdu));
    st.asduCount++;
    IEC60870_5_TypeID type = CS101_ASDU_getTypeID(asdu);
    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    if (st.expectType != 0 && type == st.expectType) {
        bool unknown = cot >= CS101_COT_UNKNOWN_TYPE_ID && cot <= CS101_COT_UNKNOWN_IOA;
        if (cot == CS101_COT_ACTIVATION_CON || cot == CS101_COT_DEACTIVATION_CON || unknown) {
            cJSON* c = cJSON_CreateObject();
            cJSON_AddNumberToObject(c, "cot", cot);
            bool negative = CS101_ASDU_isNegative(asdu) || unknown;
            cJSON_AddBoolToObject(c, "negative", negative);
            cJSON_AddItemToArray(st.confirmations, c);
            if (negative)
                st.negativeCot = cot;
        } else if (cot == CS101_COT_ACTIVATION_TERMINATION) {
            st.terminated = true;
        }
    } else if (st.expectRead && cot == CS101_COT_REQUEST) {
        for (int i = 0; i < CS101_ASDU_getNumberOfElements(asdu); i++) {
            InformationObject io = CS101_ASDU_getElement(asdu, i);
            if (io == NULL)
                break;
            if (InformationObject_getObjectAddress(io) == st.expectIoa)
                st.readAnswered = true;
            InformationObject_destroy(io);
        }
    }
    return true;
}

/* One turn of the master: its state machine, and on an unbalanced line a
 * poll (class 2, then class 1 while ACD is set) every 50 ms. */
static void step(void)
{
    if (!lnk.balanced && Hal_getTimeInMs() - lastPoll >= 50 && CS101_Master_isChannelReady(master, lnk.address)) {
        CS101_Master_pollSingleSlave(master, lnk.address);
        lastPoll = Hal_getTimeInMs();
    }
    CS101_Master_run(master);
}

typedef bool (*Predicate)(int arg);

/* Runs the master until pred holds, the TCP connection ends or the timeout
 * passes. */
static bool waitFor(Predicate pred, int arg, int timeoutMs)
{
    uint64_t end = Hal_getTimeInMs() + (uint64_t)timeoutMs;
    while (!pred(arg)) {
        if (Bridge_isClosed(bridge) || Hal_getTimeInMs() >= end)
            return pred(arg);
        step();
        Thread_sleep(1);
    }
    return true;
}

static bool hasLink(int a) { (void)a; return st.linkUp; }
static bool hasConfirmations(int n) { return cJSON_GetArraySize(st.confirmations) >= n; }
static bool hasTermination(int a) { (void)a; return st.terminated || st.negativeCot != 0; }
static bool hasReadAnswer(int a) { (void)a; return st.readAnswered || st.negativeCot != 0; }
static bool hasAsdus(int n) { return n > 0 && st.asduCount >= n; }
static bool never(int a) { (void)a; return false; }
/* Unbalanced: lib60870 sends one user data frame at a time per secondary. */
static bool channelReady(int a) { (void)a; return lnk.balanced || CS101_Master_isChannelReady(master, lnk.address); }

static bool parseBool(const char* s, bool* out)
{
    if (!strcasecmp(s, "true") || !strcmp(s, "1") || !strcasecmp(s, "on")) {
        *out = true;
        return true;
    }
    if (!strcasecmp(s, "false") || !strcmp(s, "0") || !strcasecmp(s, "off")) {
        *out = false;
        return true;
    }
    return false;
}

static bool parseTimeMs(const char* s, uint64_t* out)
{
    struct tm tm;
    memset(&tm, 0, sizeof(tm));
    const char* rest = strptime(s, "%Y-%m-%dT%H:%M:%S", &tm);
    if (rest == NULL)
        return false;
    int ms = 0;
    if (*rest == '.') {
        char* end;
        ms = (int)strtol(rest + 1, &end, 10);
        if (end - (rest + 1) != 3)
            return false;
        rest = end;
    }
    if (strcmp(rest, "Z") != 0)
        return false;
    *out = (uint64_t)timegm(&tm) * 1000 + (uint64_t)ms;
    return true;
}

static InformationObject buildCommand(IEC60870_5_TypeID type, int ioa, const char* value, bool select, int qualifier)
{
    char* end;
    switch (type) {
    case C_SC_NA_1: {
        bool b;
        if (!parseBool(value, &b))
            return NULL;
        return (InformationObject)SingleCommand_create(NULL, ioa, b, select, qualifier);
    }
    case C_DC_NA_1:
    case C_RC_NA_1:
    case C_SE_NA_1:
    case C_SE_NB_1: {
        long v = strtol(value, &end, 10);
        if (*value == 0 || *end != 0)
            return NULL;
        if (type == C_DC_NA_1 || type == C_RC_NA_1) {
            if (v < 0 || v > 3)
                return NULL;
            return type == C_DC_NA_1 ? (InformationObject)DoubleCommand_create(NULL, ioa, (int)v, select, qualifier)
                                     : (InformationObject)StepCommand_create(NULL, ioa, (StepCommandValue)v, select,
                                           qualifier);
        }
        if (v < -32768 || v > 32767)
            return NULL;
        if (type == C_SE_NA_1)
            return (InformationObject)SetpointCommandNormalized_create(NULL, ioa, normalizedFromRaw((int)v), select,
                qualifier);
        return (InformationObject)SetpointCommandScaled_create(NULL, ioa, (int)v, select, qualifier);
    }
    case C_SE_NC_1: {
        float f = strtof(value, &end);
        if (*value == 0 || *end != 0)
            return NULL;
        return (InformationObject)SetpointCommandShort_create(NULL, ioa, f, select, qualifier);
    }
    default:
        return NULL;
    }
}

static const char* errorCode = NULL;
static char errorMessage[200];

static bool failOp(const char* code, const char* message)
{
    errorCode = code;
    snprintf(errorMessage, sizeof(errorMessage), "%s", message);
    return false;
}

/* Waits until the master can take user data for the station. */
static bool awaitChannel(int timeoutMs)
{
    if (waitFor(channelReady, 0, timeoutMs))
        return true;
    return Bridge_isClosed(bridge) ? failOp("connection-lost", "connection closed while the link was busy")
                                   : failOp("timeout", "the link to the station stayed busy");
}

/* Waits for the n-th confirmation and checks that it is positive. */
static bool awaitConfirmation(int n, int timeoutMs)
{
    if (!waitFor(hasConfirmations, n, timeoutMs)) {
        snprintf(errorMessage, sizeof(errorMessage), "no confirmation %d from the station", n);
        errorCode = Bridge_isClosed(bridge) ? "connection-lost" : "timeout";
        return false;
    }
    if (st.negativeCot != 0) {
        errorCode = "negative-confirmation";
        snprintf(errorMessage, sizeof(errorMessage), "the station refused the request (cause %d)", st.negativeCot);
        return false;
    }
    return true;
}

static bool awaitTermination(int timeoutMs)
{
    if (!waitFor(hasTermination, 0, timeoutMs))
        return Bridge_isClosed(bridge) ? failOp("connection-lost", "connection closed while waiting for termination")
                                       : failOp("timeout", "no activation termination from the station");
    return true;
}

static void sendCommand(int ca, CS101_CauseOfTransmission cot, InformationObject io)
{
    CS101_Master_useSlaveAddress(master, lnk.address);
    CS101_Master_sendProcessCommand(master, cot, ca, io);
}

int runClient(const char* op, Args* args)
{
    const char* host = argString(args, "--host", NULL);
    int port, oa, timeoutMs, connectTimeoutMs, collectMs;
    if (!argInt(args, "--port", DEFAULT_PORT, &port) || !argInt(args, "--originator-address", 0, &oa) ||
        !argInt(args, "--timeout-ms", 5000, &timeoutMs) || !argInt(args, "--connect-timeout-ms", 5000, &connectTimeoutMs) ||
        !argInt(args, "--collect-ms", 0, &collectMs))
        return EXIT_USAGE;
    if (!linkFromArgs(args, &lnk))
        return EXIT_USAGE;
    int ca = lnk.commonAddress != 0 ? lnk.commonAddress : 1;
    if (oa < 0 || oa > 255) {
        logf_("--originator-address: must be 0..255");
        return EXIT_USAGE;
    }
    /* Operation arguments are parsed before connecting so that a usage
     * error never touches the network. */
    enum { OP_CONNECT, OP_INTERROGATE, OP_COUNTERS, OP_READ, OP_CLOCK, OP_TEST, OP_COMMAND, OP_MONITOR } kind;
    int qoi = 20, qcc = 5, ioa = 0, holdMs = 0, durationMs = 1000, maxAsdus = 0, qualifier = 0;
    uint64_t clockMs = Hal_getTimeInMs();
    const char* mode = "direct";
    const char* value = NULL;
    IEC60870_5_TypeID commandType = 0;
    if (!strcmp(op, "connect")) {
        kind = OP_CONNECT;
        if (!argInt(args, "--hold-ms", 0, &holdMs))
            return EXIT_USAGE;
    } else if (!strcmp(op, "interrogate")) {
        kind = OP_INTERROGATE;
        if (!argInt(args, "--qoi", 20, &qoi))
            return EXIT_USAGE;
    } else if (!strcmp(op, "counter-interrogate")) {
        kind = OP_COUNTERS;
        if (!argInt(args, "--qcc", 5, &qcc))
            return EXIT_USAGE;
    } else if (!strcmp(op, "read")) {
        kind = OP_READ;
        if (!argInt(args, "--ioa", 0, &ioa) || ioa < 1) {
            logf_("read: --ioa is required");
            return EXIT_USAGE;
        }
    } else if (!strcmp(op, "clock-sync")) {
        kind = OP_CLOCK;
        const char* t = argString(args, "--time", NULL);
        if (t != NULL && !parseTimeMs(t, &clockMs)) {
            logf_("clock-sync: --time must look like 2026-01-02T03:04:05.678Z");
            return EXIT_USAGE;
        }
    } else if (!strcmp(op, "test-command")) {
        kind = OP_TEST;
    } else if (!strcmp(op, "command")) {
        kind = OP_COMMAND;
        const char* typeArg = argString(args, "--type", "");
        commandType = (IEC60870_5_TypeID)typeFromName(typeArg);
        value = argString(args, "--value", NULL);
        mode = argString(args, "--mode", "direct");
        if (!argInt(args, "--ioa", 0, &ioa) || !argInt(args, "--qualifier", 0, &qualifier))
            return EXIT_USAGE;
        if (commandType < C_SC_NA_1 || commandType > C_SE_NC_1 || ioa < 1 || value == NULL) {
            logf_("command: --type (C_SC_NA_1, C_DC_NA_1, C_RC_NA_1, C_SE_NA_1, C_SE_NB_1, C_SE_NC_1), --ioa and --value are required");
            return EXIT_USAGE;
        }
        if (strcmp(mode, "direct") && strcmp(mode, "select") && strcmp(mode, "sbo") && strcmp(mode, "cancel")) {
            logf_("command: --mode must be direct, select, sbo or cancel");
            return EXIT_USAGE;
        }
        InformationObject probe = buildCommand(commandType, ioa, value, false, qualifier);
        if (probe == NULL) {
            logf_("command: --value '%s' is not valid for %s", value, typeArg);
            return EXIT_USAGE;
        }
        InformationObject_destroy(probe);
    } else if (!strcmp(op, "monitor")) {
        kind = OP_MONITOR;
        if (!argInt(args, "--duration-ms", 1000, &durationMs) || !argInt(args, "--max-asdus", 0, &maxAsdus))
            return EXIT_USAGE;
    } else {
        logf_("client: unknown operation %s", op);
        return EXIT_USAGE;
    }
    if (host == NULL) {
        logf_("client: --host is required");
        return EXIT_USAGE;
    }
    if (argUnknown(args) != NULL) {
        logf_("client %s: unknown argument %s", op, argUnknown(args));
        return EXIT_USAGE;
    }
    if (ioa >= (1 << (8 * lnk.ioaSize))) {
        logf_("--ioa: does not fit an information object address of %d octets", lnk.ioaSize);
        return EXIT_USAGE;
    }

    st.asdus = cJSON_CreateArray();
    st.confirmations = cJSON_CreateArray();
    uint64_t begin = Hal_getTimeInMs();
    bool ok = false;
    int exitCode = EXIT_OPERATION_FAILED;
    SerialPort sp = NULL;

    char err[160];
    int fd = tcpConnect(host, port, connectTimeoutMs, err, sizeof(err));
    if (fd < 0) {
        failOp("connect-failed", err);
        exitCode = EXIT_CONNECT_FAILED;
        goto report;
    }
    bridge = Bridge_create();
    if (bridge == NULL || !Bridge_attach(bridge, fd)) {
        failOp("failed", "cannot set up the pty bridge");
        goto report;
    }
    sp = SerialPort_create(Bridge_ptyPath(bridge), LINE_BAUD, 8, 'E', 1);
    master = CS101_Master_create(sp, NULL, NULL,
        lnk.balanced ? IEC60870_LINK_LAYER_BALANCED : IEC60870_LINK_LAYER_UNBALANCED);
    applyLinkArgs(&lnk, CS101_Master_getLinkLayerParameters(master), CS101_Master_getAppLayerParameters(master));
    CS101_Master_getAppLayerParameters(master)->originatorAddress = oa;
    CS101_Master_setASDUReceivedHandler(master, asduHandler, NULL);
    CS101_Master_setLinkLayerStateChanged(master, linkStateHandler, NULL);
    CS101_Master_setRawMessageHandler(master, rawFrameLogger, NULL);
    if (lnk.balanced) {
        /* lib60870's station A checks received frames against its own
         * address and sends to the other station's. */
        CS101_Master_setOwnAddress(master, lnk.remote);
        CS101_Master_useSlaveAddress(master, lnk.address);
    } else {
        CS101_Master_addSlave(master, lnk.address);
        CS101_Master_useSlaveAddress(master, lnk.address);
    }
    if (!SerialPort_open(sp)) {
        failOp("failed", "cannot open the pty");
        goto report;
    }
    /* Connected means: the link is initialized (reset of remote link confirmed). */
    if (!waitFor(hasLink, 0, timeoutMs)) {
        if (Bridge_isClosed(bridge))
            failOp("connection-lost", "the connection closed during the link start-up");
        else
            failOp("link-init-timeout", "the station's link was not reset within --timeout-ms");
        exitCode = EXIT_CONNECT_FAILED;
        goto report;
    }

    switch (kind) {
    case OP_CONNECT:
        if (holdMs > 0)
            waitFor(never, 0, holdMs);
        ok = !Bridge_isClosed(bridge) || failOp("connection-lost", "the connection closed while it was held");
        break;
    case OP_INTERROGATE:
        st.expectType = C_IC_NA_1;
        if ((ok = awaitChannel(timeoutMs))) {
            CS101_Master_sendInterrogationCommand(master, CS101_COT_ACTIVATION, ca, (QualifierOfInterrogation)qoi);
            ok = awaitConfirmation(1, timeoutMs) && awaitTermination(timeoutMs);
        }
        break;
    case OP_COUNTERS:
        st.expectType = C_CI_NA_1;
        if ((ok = awaitChannel(timeoutMs))) {
            CS101_Master_sendCounterInterrogationCommand(master, CS101_COT_ACTIVATION, ca, (uint8_t)qcc);
            ok = awaitConfirmation(1, timeoutMs) && awaitTermination(timeoutMs);
        }
        break;
    case OP_READ:
        st.expectType = C_RD_NA_1;
        st.expectRead = true;
        st.expectIoa = ioa;
        if (!awaitChannel(timeoutMs))
            break;
        CS101_Master_sendReadCommand(master, ca, ioa);
        if (!waitFor(hasReadAnswer, 0, timeoutMs))
            failOp(Bridge_isClosed(bridge) ? "connection-lost" : "timeout", "no answer to the read command");
        else if (st.negativeCot != 0) {
            errorCode = "negative-confirmation";
            snprintf(errorMessage, sizeof(errorMessage), "the station refused the request (cause %d)", st.negativeCot);
        } else
            ok = true;
        break;
    case OP_CLOCK: {
        struct sCP56Time2a t;
        CP56Time2a_createFromMsTimestamp(&t, clockMs);
        st.expectType = C_CS_NA_1;
        if ((ok = awaitChannel(timeoutMs))) {
            CS101_Master_sendClockSyncCommand(master, ca, &t);
            ok = awaitConfirmation(1, timeoutMs);
        }
        break;
    }
    case OP_TEST:
        st.expectType = C_TS_NA_1;
        if ((ok = awaitChannel(timeoutMs))) {
            CS101_Master_sendTestCommand(master, ca);
            ok = awaitConfirmation(1, timeoutMs);
        }
        break;
    case OP_COMMAND: {
        st.expectType = commandType;
        int confirmations = 0;
        ok = true;
        if (strcmp(mode, "direct") != 0) {
            InformationObject sel = buildCommand(commandType, ioa, value, true, qualifier);
            if ((ok = awaitChannel(timeoutMs))) {
                sendCommand(ca, CS101_COT_ACTIVATION, sel);
                ok = awaitConfirmation(++confirmations, timeoutMs);
            }
            if (ok && !strcmp(mode, "cancel") && (ok = awaitChannel(timeoutMs))) {
                sendCommand(ca, CS101_COT_DEACTIVATION, sel);
                ok = awaitConfirmation(++confirmations, timeoutMs);
            }
            InformationObject_destroy(sel);
        }
        if (ok && (!strcmp(mode, "direct") || !strcmp(mode, "sbo"))) {
            InformationObject exec = buildCommand(commandType, ioa, value, false, qualifier);
            if ((ok = awaitChannel(timeoutMs))) {
                sendCommand(ca, CS101_COT_ACTIVATION, exec);
                ok = awaitConfirmation(++confirmations, timeoutMs) && awaitTermination(timeoutMs);
            }
            InformationObject_destroy(exec);
        }
        break;
    }
    case OP_MONITOR:
        waitFor(hasAsdus, maxAsdus, durationMs);
        ok = !Bridge_isClosed(bridge) || failOp("connection-lost", "connection closed while monitoring");
        break;
    }
    if (ok && collectMs > 0)
        waitFor(never, 0, collectMs);
    if (ok)
        exitCode = EXIT_OK;

report:
    if (bridge != NULL)
        Bridge_stop(bridge);
    cJSON* r = cJSON_CreateObject();
    cJSON_AddStringToObject(r, "schemaVersion", SCHEMA_VERSION);
    cJSON_AddStringToObject(r, "adapter", ADAPTER_NAME);
    cJSON_AddStringToObject(r, "operation", op);
    cJSON_AddBoolToObject(r, "ok", ok);
    if (ok) {
        cJSON_AddNullToObject(r, "error");
    } else {
        cJSON* e = cJSON_AddObjectToObject(r, "error");
        cJSON_AddStringToObject(e, "code", errorCode ? errorCode : "failed");
        cJSON_AddStringToObject(e, "message", errorMessage);
    }
    cJSON_AddBoolToObject(r, "connected", st.linkUp);
    cJSON_AddItemToObject(r, "confirmations", st.confirmations);
    cJSON_AddBoolToObject(r, "terminated", st.terminated);
    cJSON_AddItemToObject(r, "asdus", st.asdus);
    cJSON_AddNumberToObject(r, "elapsedMs", (double)(Hal_getTimeInMs() - begin));
    emitJson(r);
    return exitCode;
}

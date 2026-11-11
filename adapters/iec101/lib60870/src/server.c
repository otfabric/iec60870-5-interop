/* SPDX-License-Identifier: GPL-3.0-or-later */
/*
 * The controlled station: a lib60870-C CS101 slave (an unbalanced secondary or
 * balanced station B) on the pty of bridge.c, serving the fixture.
 */
#include "common.h"

#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "hal_thread.h"
#include "hal_time.h"

static Fixture fx;
/* Handlers run on the thread that calls CS101_Slave_run; the lock is kept from
 * the 104 driver, whose handlers run one thread per connection. */
static pthread_mutex_t stateLock = PTHREAD_MUTEX_INITIALIZER;
static volatile sig_atomic_t running = 1;
static int broadcastCA = 0xFF;

static void onSignal(int sig)
{
    (void)sig;
    running = 0;
}

static cJSON* event(const char* name)
{
    cJSON* e = cJSON_CreateObject();
    cJSON_AddStringToObject(e, "event", name);
    return e;
}

/* Mirrors asdu with one of the "unknown ..." causes and the P/N bit set. */
static void reject(IMasterConnection connection, CS101_ASDU asdu, CS101_CauseOfTransmission cot)
{
    CS101_ASDU_setCOT(asdu, cot);
    CS101_ASDU_setNegative(asdu, true);
    IMasterConnection_sendASDU(connection, asdu);
}

static bool isCAAllowed(void* parameter, int ca)
{
    (void)parameter;
    return ca == fx.commonAddress || ca == broadcastCA;
}

/* Which points an interrogation reports. */
#define SELECT_COUNTERS (-1) /* the integrated totals */
#define SELECT_STATION 0     /* everything else */
/* 1..16: the points of that interrogation group */

static bool isSelected(const Point* p, int selection)
{
    if (selection == SELECT_COUNTERS)
        return p->type == M_IT_NA_1;
    if (selection == SELECT_STATION)
        return p->type != M_IT_NA_1;
    return (p->groups & (1u << (selection - 1))) != 0;
}

/* Sends the selected points in ascending address order, packing consecutive
 * points of one type into one ASDU. */
static void sendPoints(IMasterConnection connection, CS101_CauseOfTransmission cot, int oa, int selection)
{
    CS101_AppLayerParameters params = IMasterConnection_getApplicationLayerParameters(connection);
    CS101_ASDU asdu = NULL;
    IEC60870_5_TypeID current = 0;

    for (int i = 0; i < fx.pointCount; i++) {
        Point* p = &fx.points[i];
        if (!isSelected(p, selection))
            continue;
        InformationObject io = pointToObject(p, NULL);
        if (asdu != NULL && (p->type != current || !CS101_ASDU_addInformationObject(asdu, io))) {
            IMasterConnection_sendASDU(connection, asdu);
            CS101_ASDU_destroy(asdu);
            asdu = NULL;
        }
        if (asdu == NULL) {
            asdu = CS101_ASDU_create(params, false, cot, oa, fx.commonAddress, false, false);
            current = p->type;
            CS101_ASDU_addInformationObject(asdu, io);
        }
        InformationObject_destroy(io);
    }
    if (asdu != NULL) {
        IMasterConnection_sendASDU(connection, asdu);
        CS101_ASDU_destroy(asdu);
    }
}

static bool interrogationHandler(void* parameter, IMasterConnection connection, CS101_ASDU asdu, uint8_t qoi)
{
    (void)parameter;
    /* QOI 21..36 are the groups 1..16; a group nothing belongs to is refused. */
    int selection = SELECT_STATION;
    bool accepted = qoi == IEC60870_QOI_STATION;
    if (qoi >= IEC60870_QOI_GROUP_1 && qoi <= IEC60870_QOI_GROUP_16) {
        selection = qoi - IEC60870_QOI_STATION;
        for (int i = 0; i < fx.pointCount && !accepted; i++)
            accepted = isSelected(&fx.points[i], selection);
    }
    cJSON* e = event("interrogation");
    cJSON_AddNumberToObject(e, "commonAddress", CS101_ASDU_getCA(asdu));
    cJSON_AddNumberToObject(e, "qoi", qoi);
    cJSON_AddBoolToObject(e, "accepted", accepted);
    emitJson(e);

    /* A broadcast request is answered with the station's own address. */
    CS101_ASDU_setCA(asdu, fx.commonAddress);
    if (!accepted) {
        IMasterConnection_sendACT_CON(connection, asdu, true);
        return true;
    }
    IMasterConnection_sendACT_CON(connection, asdu, false);
    pthread_mutex_lock(&stateLock);
    /* The cause of an interrogation's answers is its QOI: 20, or 21..36. */
    sendPoints(connection, (CS101_CauseOfTransmission)qoi, CS101_ASDU_getOA(asdu), selection);
    pthread_mutex_unlock(&stateLock);
    IMasterConnection_sendACT_TERM(connection, asdu);
    return true;
}

static bool counterInterrogationHandler(void* parameter, IMasterConnection connection, CS101_ASDU asdu,
    QualifierOfCIC qcc)
{
    (void)parameter;
    /* Only "general request counter" with "read" (no freeze, no reset). */
    bool accepted = qcc == (IEC60870_QCC_RQT_GENERAL | IEC60870_QCC_FRZ_READ);
    cJSON* e = event("counter-interrogation");
    cJSON_AddNumberToObject(e, "commonAddress", CS101_ASDU_getCA(asdu));
    cJSON_AddNumberToObject(e, "qcc", qcc);
    cJSON_AddBoolToObject(e, "accepted", accepted);
    emitJson(e);

    CS101_ASDU_setCA(asdu, fx.commonAddress);
    if (!accepted) {
        IMasterConnection_sendACT_CON(connection, asdu, true);
        return true;
    }
    IMasterConnection_sendACT_CON(connection, asdu, false);
    pthread_mutex_lock(&stateLock);
    sendPoints(connection, CS101_COT_REQUESTED_BY_GENERAL_COUNTER, CS101_ASDU_getOA(asdu), SELECT_COUNTERS);
    pthread_mutex_unlock(&stateLock);
    IMasterConnection_sendACT_TERM(connection, asdu);
    return true;
}

static bool readHandler(void* parameter, IMasterConnection connection, CS101_ASDU asdu, int ioa)
{
    (void)parameter;
    pthread_mutex_lock(&stateLock);
    Point* p = Fixture_findPoint(&fx, ioa);
    InformationObject io = p ? pointToObject(p, NULL) : NULL;
    pthread_mutex_unlock(&stateLock);

    cJSON* e = event("read");
    cJSON_AddNumberToObject(e, "ioa", ioa);
    cJSON_AddBoolToObject(e, "accepted", io != NULL);
    emitJson(e);

    if (io == NULL) {
        reject(connection, asdu, CS101_COT_UNKNOWN_IOA);
        return true;
    }
    CS101_ASDU reply = CS101_ASDU_create(IMasterConnection_getApplicationLayerParameters(connection), false,
        CS101_COT_REQUEST, CS101_ASDU_getOA(asdu), fx.commonAddress, false, false);
    CS101_ASDU_addInformationObject(reply, io);
    IMasterConnection_sendASDU(connection, reply);
    CS101_ASDU_destroy(reply);
    InformationObject_destroy(io);
    return true;
}

static bool clockSyncHandler(void* parameter, IMasterConnection connection, CS101_ASDU asdu, CP56Time2a newTime)
{
    (void)parameter;
    (void)connection;
    (void)asdu;
    char buf[40];
    formatTime(CP56Time2a_toMsTimestamp(newTime), buf, sizeof(buf));
    cJSON* e = event("clock-sync");
    cJSON_AddStringToObject(e, "time", buf);
    emitJson(e);
    return true; /* lib60870 sends the activation confirmation */
}

/* Decodes the command object of asdu. Returns false when the value is one
 * the command type does not permit. */
static bool decodeCommand(InformationObject io, IEC60870_5_TypeID type, double* value, bool* select, cJSON* e)
{
    switch (type) {
    case C_SC_NA_1:
        *value = SingleCommand_getState((SingleCommand)io) ? 1 : 0;
        *select = SingleCommand_isSelect((SingleCommand)io);
        cJSON_AddBoolToObject(e, "value", *value != 0);
        return true;
    case C_DC_NA_1:
        *value = DoubleCommand_getState((DoubleCommand)io);
        *select = DoubleCommand_isSelect((DoubleCommand)io);
        cJSON_AddNumberToObject(e, "value", *value);
        return *value == 1 || *value == 2;
    case C_RC_NA_1:
        *value = StepCommand_getState((StepCommand)io);
        *select = StepCommand_isSelect((StepCommand)io);
        cJSON_AddNumberToObject(e, "value", *value);
        return *value == IEC60870_STEP_LOWER || *value == IEC60870_STEP_HIGHER;
    case C_SE_NA_1:
        *value = normalizedToRaw(SetpointCommandNormalized_getValue((SetpointCommandNormalized)io));
        *select = SetpointCommandNormalized_isSelect((SetpointCommandNormalized)io);
        cJSON_AddNumberToObject(e, "value", *value);
        return true;
    case C_SE_NB_1:
        *value = SetpointCommandScaled_getValue((SetpointCommandScaled)io);
        *select = SetpointCommandScaled_isSelect((SetpointCommandScaled)io);
        cJSON_AddNumberToObject(e, "value", *value);
        return true;
    case C_SE_NC_1:
        *value = (double)SetpointCommandShort_getValue((SetpointCommandShort)io);
        *select = SetpointCommandShort_isSelect((SetpointCommandShort)io);
        cJSON_AddNumberToObject(e, "value", *value);
        return true;
    default:
        return false;
    }
}

/* Applies an executed command to its target. Returns false when the target
 * cannot take the value (a step position at its limit). */
static bool applyCommand(Command* c, double value)
{
    Point* t = &fx.points[c->target];
    if (c->type == C_RC_NA_1) {
        double next = t->value + (value == IEC60870_STEP_HIGHER ? 1 : -1);
        if (next < -64 || next > 63)
            return false;
        t->value = next;
        return true;
    }
    t->value = value;
    return true;
}

static bool asduHandler(void* parameter, IMasterConnection connection, CS101_ASDU asdu)
{
    (void)parameter;
    IEC60870_5_TypeID wire = CS101_ASDU_getTypeID(asdu);
    IEC60870_5_TypeID type = baseType(wire);
    /* IEC 101 has no CP56Time2a command types (58..64): unknown type. */
    if (type < C_SC_NA_1 || type > C_SE_NC_1 || wire != type)
        return false; /* lib60870 answers "unknown type identification" */

    CS101_CauseOfTransmission cot = CS101_ASDU_getCOT(asdu);
    if (cot != CS101_COT_ACTIVATION && cot != CS101_COT_DEACTIVATION) {
        reject(connection, asdu, CS101_COT_UNKNOWN_COT);
        return true;
    }
    InformationObject io = CS101_ASDU_getElement(asdu, 0);
    if (io == NULL)
        return false;
    int ioa = InformationObject_getObjectAddress(io);

    cJSON* e = event("command");
    cJSON_AddStringToObject(e, "type", typeName(wire));
    cJSON_AddNumberToObject(e, "ioa", ioa);
    cJSON_AddNumberToObject(e, "cot", cot);

    pthread_mutex_lock(&stateLock);
    Command* c = Fixture_findCommand(&fx, ioa);
    if (c == NULL || c->type != type) {
        pthread_mutex_unlock(&stateLock);
        InformationObject_destroy(io);
        cJSON_AddStringToObject(e, "outcome", "unknown-ioa");
        emitJson(e);
        reject(connection, asdu, CS101_COT_UNKNOWN_IOA);
        return true;
    }

    double value = 0;
    bool select = false;
    bool valid = decodeCommand(io, type, &value, &select, e);
    InformationObject_destroy(io);
    cJSON_AddBoolToObject(e, "select", select);
    CS101_ASDU_setCA(asdu, fx.commonAddress);

    const char* outcome;
    InformationObject report = NULL;
    if (cot == CS101_COT_DEACTIVATION) {
        c->selected = false;
        outcome = "deactivated";
    } else if (!valid) {
        outcome = "rejected";
    } else if (select) {
        c->selected = true;
        outcome = "selected";
    } else if (c->selectRequired && !c->selected) {
        outcome = "rejected";
    } else {
        c->selected = false;
        if (applyCommand(c, value)) {
            struct sCP56Time2a now;
            CP56Time2a_createFromMsTimestamp(&now, Hal_getTimeInMs());
            report = pointToObject(&fx.points[c->target], &now);
            outcome = "executed";
        } else {
            outcome = "rejected";
        }
    }
    int reportCause = c->reportCause;
    pthread_mutex_unlock(&stateLock);

    cJSON_AddStringToObject(e, "outcome", outcome);
    emitJson(e);

    if (strcmp(outcome, "deactivated") == 0) {
        CS101_ASDU_setCOT(asdu, CS101_COT_DEACTIVATION_CON);
        CS101_ASDU_setNegative(asdu, false);
        IMasterConnection_sendASDU(connection, asdu);
    } else if (strcmp(outcome, "rejected") == 0) {
        IMasterConnection_sendACT_CON(connection, asdu, true);
    } else {
        IMasterConnection_sendACT_CON(connection, asdu, false);
        if (report != NULL) {
            /* Confirmation, then the new state of the target, then termination. */
            CS101_ASDU r = CS101_ASDU_create(IMasterConnection_getApplicationLayerParameters(connection), false,
                (CS101_CauseOfTransmission)reportCause, 0, fx.commonAddress, false, false);
            CS101_ASDU_addInformationObject(r, report);
            IMasterConnection_sendASDU(connection, r);
            CS101_ASDU_destroy(r);
            InformationObject_destroy(report);
            IMasterConnection_sendACT_TERM(connection, asdu);
        }
    }
    return true;
}


/* Every address of the fixture must fit the fields of the link flags. */
static bool fitsFields(const LinkArgs* l, char* err, size_t size)
{
    int maxIoa = (1 << (8 * l->ioaSize)) - 1;
    for (int i = 0; i < fx.pointCount; i++)
        if (fx.points[i].ioa > maxIoa) {
            snprintf(err, size, "point %d does not fit an information object address of %d octets",
                fx.points[i].ioa, l->ioaSize);
            return false;
        }
    for (int i = 0; i < fx.commandCount; i++)
        if (fx.commands[i].ioa > maxIoa) {
            snprintf(err, size, "command %d does not fit an information object address of %d octets",
                fx.commands[i].ioa, l->ioaSize);
            return false;
        }
    if (fx.commonAddress >= (1 << (8 * l->caSize)) - 1) {
        snprintf(err, size, "common address %d does not fit a common address of %d octets", fx.commonAddress,
            l->caSize);
        return false;
    }
    return true;
}

static CS101_Slave makeSlave(SerialPort sp, const LinkArgs* l)
{
    CS101_Slave slave = CS101_Slave_create(sp, NULL, NULL,
        l->balanced ? IEC60870_LINK_LAYER_BALANCED : IEC60870_LINK_LAYER_UNBALANCED);
    applyLinkArgs(l, CS101_Slave_getLinkLayerParameters(slave), CS101_Slave_getAppLayerParameters(slave));
    CS101_Slave_setLinkLayerAddress(slave, l->address);
    /* Balanced: the address of the frames station B sends as a primary. */
    CS101_Slave_setLinkLayerAddressOtherStation(slave, l->remote);
    CS101_Slave_setAllowedCAHandler(slave, isCAAllowed, NULL);
    CS101_Slave_setInterrogationHandler(slave, interrogationHandler, NULL);
    CS101_Slave_setCounterInterrogationHandler(slave, counterInterrogationHandler, NULL);
    CS101_Slave_setReadHandler(slave, readHandler, NULL);
    CS101_Slave_setClockSyncHandler(slave, clockSyncHandler, NULL);
    CS101_Slave_setASDUHandler(slave, asduHandler, NULL);
    CS101_Slave_setRawMessageHandler(slave, rawFrameLogger, NULL);
    return slave;
}

int runServer(Args* args)
{
    const char* fixturePath = argString(args, "--fixture", DEFAULT_FIXTURE);
    const char* bindAddress = argString(args, "--bind-address", "0.0.0.0");
    const char* readyFile = argString(args, "--ready-file", DEFAULT_READY_FILE);
    int port;
    if (!argInt(args, "--port", DEFAULT_PORT, &port) || port < 1 || port > 65535)
        return EXIT_USAGE;
    LinkArgs lnk;
    if (!linkFromArgs(args, &lnk))
        return EXIT_USAGE;
    if (argUnknown(args) != NULL) {
        logf_("server: unknown argument %s", argUnknown(args));
        return EXIT_USAGE;
    }

    char err[256];
    if (!Fixture_load(fixturePath, &fx, err, sizeof(err))) {
        logf_("fixture %s: %s", fixturePath, err);
        return EXIT_FIXTURE_INVALID;
    }
    if (lnk.commonAddress != 0)
        fx.commonAddress = lnk.commonAddress;
    if (!fitsFields(&lnk, err, sizeof(err))) {
        logf_("fixture %s: %s", fixturePath, err);
        return EXIT_FIXTURE_INVALID;
    }
    broadcastCA = (1 << (8 * lnk.caSize)) - 1;
    unlink(readyFile);

    char address[96];
    int listenFd = tcpListen(bindAddress, port, address, sizeof(address));
    if (listenFd < 0) {
        logf_("cannot listen on %s:%d", bindAddress, port);
        return EXIT_OPERATION_FAILED;
    }
    Bridge* bridge = Bridge_create();
    if (bridge == NULL)
        return EXIT_OPERATION_FAILED;

    SerialPort sp = SerialPort_create(Bridge_ptyPath(bridge), LINE_BAUD, 8, 'E', 1);
    if (!SerialPort_open(sp)) {
        logf_("cannot open the pty %s", Bridge_ptyPath(bridge));
        return EXIT_OPERATION_FAILED;
    }
    if (!Bridge_serve(bridge, listenFd)) {
        logf_("cannot start the bridge");
        return EXIT_OPERATION_FAILED;
    }

    signal(SIGTERM, onSignal);
    signal(SIGINT, onSignal);

    FILE* rf = fopen(readyFile, "w");
    if (rf == NULL) {
        logf_("cannot write the ready file %s", readyFile);
        return EXIT_OPERATION_FAILED;
    }
    fclose(rf);
    cJSON* ready = event("ready");
    cJSON_AddStringToObject(ready, "adapter", ADAPTER_NAME);
    cJSON_AddStringToObject(ready, "address", address);
    cJSON_AddStringToObject(ready, "fixture", fx.name);
    cJSON_AddNumberToObject(ready, "commonAddress", fx.commonAddress);
    cJSON_AddStringToObject(ready, "standard", STANDARD);
    cJSON_AddStringToObject(ready, "mode", lnk.balanced ? "balanced" : "unbalanced");
    cJSON_AddNumberToObject(ready, "linkAddress", lnk.address);
    emitJson(ready);

    /*
     * Every TCP connection is a line that has just come up: the station's
     * link layer starts afresh (new CS101_Slave, same fixture state), so no
     * frame count bit, queued ASDU or link state of the previous controlling
     * station carries over. The bridge holds the new connection's bytes back
     * until the new slave exists.
     */
    CS101_Slave slave = NULL;
    while (running) {
        int opened = Bridge_openedCount(bridge);
        if (opened != Bridge_armedCount(bridge)) {
            if (slave != NULL)
                CS101_Slave_destroy(slave);
            SerialPort_discardInBuffer(sp);
            slave = makeSlave(sp, &lnk);
            Bridge_arm(bridge, opened);
        }
        if (slave != NULL)
            CS101_Slave_run(slave);
        Thread_sleep(1);
    }

    unlink(readyFile);
    Bridge_stop(bridge);
    if (slave != NULL)
        CS101_Slave_destroy(slave);
    SerialPort_close(sp);
    SerialPort_destroy(sp);
    cJSON* stopped = event("stopped");
    emitJson(stopped);
    return EXIT_OK;
}

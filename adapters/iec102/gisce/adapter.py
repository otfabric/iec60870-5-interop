#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""iec60870-5-interop IEC 60870-5-102 adapter for gisce/iec870ree.

The container contract (docs/CONTAINER_CONTRACT.md, "IEC 102") for the
controlling station of gisce/iec870ree (AGPL-3.0), a master of the Spanish
REE electricity meter profile. The library has no controlled station, so
this image is a client only: "server" is a usage error.

The protocol is the library's: its LinkLayer (request status of link, reset
of remote link, the frame count bit), its AppLayer (session login and
logout, the request and class 2 polling loop, process_request and
process_requestresponse) and its ASDU classes. The adapter adds
  - a TCP physical layer (iec870ree.protocol.PhysicalLayer) whose waits are
    bounded by --timeout-ms and which reports a closed connection (the
    library's own iec870ree.ip.Ip waits 5 s after connecting and never
    notices the end of a connection),
  - a record of every variable frame the link layer returns, from which the
    result document is written, and
  - for read-totals, set-time and read-tariff the five lines of the library's
    read_absolute_values / set_datetime / current_tariff_info, so that the
    record address, the address range and the time can be given (the
    library fixes them to its register names, 1..8 and the current time).
Nothing here encodes or decodes a frame.
"""

import datetime
import json
import logging
import os
import re
import socket
import sys
import time

from iec870ree import app_asdu, base_asdu, protocol

ADAPTER = "gisce"
STANDARD = "iec60870-5-102"
SCHEMA_VERSION = "1.0"
ADAPTER_VERSION = os.environ.get("ADAPTER_VERSION", "dev")
UPSTREAM_VERSION = os.environ.get("IEC870REE_VERSION", "unknown")
UPSTREAM_REVISION = os.environ.get("IEC870REE_SHA", "unknown")
FIXTURES = "/fixtures"

EXIT_OK, EXIT_FAILED, EXIT_USAGE, EXIT_CONNECT = 0, 1, 2, 3

OPERATIONS = ["connect", "read-totals", "read-time", "set-time", "read-product", "read-tariff"]
REE_ONLY = {"set-time", "read-tariff"}

# Mnemonics of the types the contract models (IEC 102 and the REE profile).
NAMES = {
    8: "M_IT_TG_2", 11: "M_IT_TK_2", 71: "P_MP_NA_2", 72: "M_TI_TA_2",
    100: "C_RD_NA_2", 103: "C_TI_NA_2", 122: "C_CI_NT_2", 123: "C_CI_NU_2",
    133: "C_TA_VC_2", 134: "C_TA_VM_2", 135: "M_TA_VC_2", 136: "M_TA_VM_2",
    181: "C_CS_TA_2", 183: "C_AC_NA_2", 187: "C_FS_NA_2",
}
# Types whose document has no information object (nothing, or only the access key).
NO_OBJECTS = {100, 103, 133, 183, 187}
REFUSALS = set(range(13, 19))

TIME_A = re.compile(r"^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$")
TIME_B = re.compile(r"^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})\.(\d{3})$")

log = logging.getLogger("iec60870-5-interop")


class Usage(Exception):
    pass


class LineTimeout(Exception):
    pass


class LineLost(Exception):
    pass


# ---------------------------------------------------------------------------
# Transport and link instrumentation
# ---------------------------------------------------------------------------

class Line(protocol.PhysicalLayer):
    """The FT 1.2 byte stream over one TCP connection."""

    def __init__(self, host, port, connect_timeout, timeout):
        self.addr = (host, port)
        self.connect_timeout = connect_timeout
        self.timeout = timeout
        self.sock = None
        self.buf = bytearray()
        self.lost = False

    def connect(self):
        self.sock = socket.create_connection(self.addr, timeout=self.connect_timeout)
        self.sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)

    def disconnect(self):
        if self.sock is not None:
            try:
                self.sock.close()
            finally:
                self.sock = None

    def send_byte(self, byte):
        self.send_bytes(bytes([byte]))

    def send_bytes(self, bts):
        try:
            self.sock.sendall(bytes(bts))
        except OSError as e:
            self.lost = True
            raise LineLost(str(e)) from e

    def get_byte(self, timeout=60):
        if not self.buf:
            self.sock.settimeout(min(timeout, self.timeout))
            try:
                data = self.sock.recv(256)
            except socket.timeout as e:
                raise LineTimeout("no answer from the station") from e
            except OSError as e:
                self.lost = True
                raise LineLost(str(e)) from e
            if not data:
                self.lost = True
                raise LineLost("the station closed the connection")
            self.buf.extend(data)
        return self.buf.pop(0)


class Link(protocol.LinkLayer):
    """The library's link layer, keeping every variable frame it returns."""

    def __init__(self, der, dir_pm):
        super().__init__(der, dir_pm)
        self.asdus = []
        self.app = None

    def get_frame(self, timeout=60):
        frame = super().get_frame(timeout)
        if isinstance(frame, base_asdu.VariableAsdu):
            self.asdus.append(frame)
            # The library's read methods count every data ASDU as content.
            if self.app is not None:
                self.app.reset_content_received()
        return frame


# ---------------------------------------------------------------------------
# Documents
# ---------------------------------------------------------------------------

def time_a_text(t):
    return "%04d-%02d-%02dT%02d:%02d" % (2000 + t.year, t.month, t.dayofmonth, t.hour, t.minute)


def time_b_text(t):
    return "%s:%02d.%03d" % (time_a_text(t), t.seconds, t.microseconds // 1000)


def dt_text(dt):
    return "%04d-%02d-%02dT%02d:%02d" % (dt.year, dt.month, dt.day, dt.hour, dt.minute)


def tariff_object(r):
    return {
        "address": r.address, "activeAbs": r.active_abs, "activeInc": r.active_inc, "activeQuality": r.active_qual,
        "reactiveIndAbs": r.reactive_abs_ind, "reactiveIndInc": r.reactive_inc_ind, "reactiveIndQuality": r.reactive_qua_ind,
        "reactiveCapAbs": r.reactive_abs_cap, "reactiveCapInc": r.reactive_inc_cap, "reactiveCapQuality": r.reactive_qual_cap,
        "maxPower": r.max_power, "maxPowerTime": dt_text(r.max_power_date), "maxPowerQuality": r.max_power_qual,
        "excessPower": r.excess_power, "excessPowerQuality": r.ecxess_power_qual,
        "periodStart": dt_text(r.date_start), "periodEnd": dt_text(r.date_end),
    }


def asdu_doc(f):
    t = f.tipo
    name = NAMES.get(t)
    if name is None:
        cls = app_asdu.AppAsduRegistry.types.get(t)
        name = cls.__name__ if cls is not None else None
    d = {
        "type": name, "typeId": t, "cot": f.causa_tm, "negative": bool(f.pn), "test": bool(f.buffer[9] & 0x80),
        "originator": 0, "commonAddress": f.dir_pm, "record": f.dir_registro,
        "sequence": bool(f.cualificador_ev & 0x80), "count": f.cualificador_ev & 0x7F, "objects": [],
    }
    c = f.content
    if t in NO_OBJECTS:
        pass
    elif isinstance(c, app_asdu.M_IT_TX_2):
        d["objects"] = [{"address": v.address, "value": v.total, "quality": v.quality} for v in c.valores]
        d["time"] = time_a_text(c.tiempo)
        d["su"] = bool(c.tiempo.SU)
        d["timeInvalid"] = bool(c.tiempo.IV)
    elif isinstance(c, (app_asdu.M_TI_TA_2, app_asdu.C_CS_TA_2)):
        d["objects"] = [{"time": time_b_text(c.tiempo), "su": bool(c.tiempo.SU), "timeInvalid": bool(c.tiempo.IV)}]
    elif isinstance(c, app_asdu.P_MP_NA_2):
        d["objects"] = [{"standard": f.data[0], "manufacturer": c.codigo_fabricante, "product": c.codigo_equipo}]
    elif isinstance(c, app_asdu.M_TA_VX_2):
        d["objects"] = [tariff_object(r) for r in c.valores]
    elif isinstance(c, app_asdu.C_CI_NX_2):
        d["objects"] = [{"first": c.primer_integrado, "last": c.ultimo_integrado,
                         "from": time_a_text(c.tiempo_inicial), "to": time_a_text(c.tiempo_final)}]
    elif isinstance(c, app_asdu.C_TA_VM_2):
        d["objects"] = [{"from": time_a_text(c.start_date), "to": time_a_text(c.end_date)}]
    else:
        d["data"] = bytes(f.data).hex()
    return d


def emit(doc):
    sys.stdout.write(json.dumps(doc, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def capabilities():
    false = dict.fromkeys([
        "generalInterrogation", "groupInterrogation", "counterInterrogation", "read", "testCommand",
        "directCommands", "selectBeforeOperate", "commandDeactivation", "timeTaggedCommands", "spontaneousOnCommand",
        "apciParameters", "multipleConnections", "pointQualityOnBitstring", "tls", "tlsServer", "tlsClient",
        "dataTransferEvents", "fileTransfer", "fileServer", "fileClient", "fileUploadServer", "fileUploadClient",
        "fileDirectoryServer", "fileDirectoryClient", "redundancyServer", "redundancyClient", "secureAuthentication",
        "balancedMode", "distinctBalancedAddresses",
    ], False)
    features = dict(false, clockSync=True, unbalancedMode=True, integratedTotals=True, readTime=True, setTime=True,
                    productIdentification=True, tariffInformation=True, reeSession=True)
    return {
        "schemaVersion": SCHEMA_VERSION, "adapter": ADAPTER, "adapterVersion": ADAPTER_VERSION,
        "protocol": STANDARD, "standard": STANDARD, "transport": "tcp-stream", "linkModes": ["unbalanced"],
        "upstream": {
            "name": "gisce/iec870ree", "version": UPSTREAM_VERSION, "revision": UPSTREAM_REVISION,
            "license": "AGPL-3.0", "language": "Python", "independentEngine": True,
        },
        "roles": {"server": False, "client": True},
        "features": features,
        "pointTypes": [], "commandTypes": [],
        "asduTypes": {"monitor": [8, 11, 71, 72, 135, 136], "control": [100, 103, 122, 123, 133, 134, 181, 183, 187]},
        "linkAddressSizes": [2], "caSizes": [2], "profiles": ["standard", "ree"],
        "clientOperations": OPERATIONS,
    }


# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------

class Args:
    def __init__(self, argv):
        self.flags = {}
        i = 0
        while i < len(argv):
            a = argv[i]
            if not a.startswith("--") or i + 1 >= len(argv):
                raise Usage("unexpected argument '%s'" % a)
            if a in self.flags:
                raise Usage("%s given twice" % a)
            self.flags[a] = argv[i + 1]
            i += 2

    def str(self, name, default=None):
        return self.flags.pop(name, default)

    def int(self, name, default, lo, hi):
        s = self.flags.pop(name, None)
        if s is None:
            if default is None:
                raise Usage("%s is required" % name)
            return default
        if not re.fullmatch(r"-?\d+", s):
            raise Usage("%s: '%s' is not an integer" % (name, s))
        n = int(s)
        if n < lo or n > hi:
            raise Usage("%s: must be %d..%d, got %d" % (name, lo, hi, n))
        return n

    def finish(self):
        if self.flags:
            raise Usage("unknown flag %s" % sorted(self.flags)[0])


def parse_time(name, s, fmt):
    if s is None:
        raise Usage("%s is required" % name)
    m = (TIME_A if fmt == "a" else TIME_B).match(s)
    if not m:
        want = "YYYY-MM-DDTHH:MM" if fmt == "a" else "YYYY-MM-DDTHH:MM:SS.mmm"
        raise Usage("%s: '%s' is not a local time %s" % (name, s, want))
    v = [int(x) for x in m.groups()]
    try:
        if fmt == "a":
            dt = datetime.datetime(*v)
        else:
            dt = datetime.datetime(*v[:6], microsecond=v[6] * 1000)
    except ValueError as e:
        raise Usage("%s: %s" % (name, e)) from e
    if not 2000 <= dt.year <= 2099:
        raise Usage("%s: the year must be 2000..2099" % name)
    return dt


def parse_client(argv):
    if not argv or argv[0].startswith("--"):
        raise Usage("client: the operation is missing")
    op = argv[0]
    if op not in OPERATIONS:
        raise Usage("client: unknown operation '%s'" % op)
    a = Args(argv[1:])
    o = {"op": op}
    o["host"] = a.str("--host")
    if not o["host"]:
        raise Usage("--host is required")
    o["port"] = a.int("--port", 2401, 1, 65535)
    mode = a.str("--link-mode", "unbalanced")
    if mode != "unbalanced":
        raise Usage("--link-mode: IEC 102 is unbalanced only, got '%s'" % mode)
    size = a.int("--link-address-size", 2, 0, 2)
    if size != 2:
        raise Usage("--link-address-size: gisce/iec870ree has a link address of two octets only")
    o["link"] = a.int("--link-address", 1, 0, 65534)
    if a.int("--ca-size", 2, 1, 2) != 2:
        raise Usage("--ca-size: gisce/iec870ree has a common address of two octets only")
    o["ca"] = a.int("--common-address", 1, 0, 65535)
    profile = a.str("--profile", "standard")
    if profile not in ("standard", "ree"):
        raise Usage("--profile: 'standard' or 'ree', got '%s'" % profile)
    o["ree"] = profile == "ree"
    if o["ree"]:
        o["key"] = a.int("--access-key", None, 0, 4294967295)
    elif "--access-key" in a.flags:
        raise Usage("--access-key needs --profile ree")
    if op in REE_ONLY and not o["ree"]:
        raise Usage("%s is an operation of the REE profile: give --profile ree" % op)
    o["connect_timeout"] = a.int("--connect-timeout-ms", 5000, 1, 3600000) / 1000.0
    o["timeout"] = a.int("--timeout-ms", 5000, 1, 3600000) / 1000.0
    if op == "read-totals":
        kind = a.str("--kind", "absolute")
        if kind not in ("absolute", "incremental"):
            raise Usage("--kind: 'absolute' or 'incremental', got '%s'" % kind)
        o["kind"] = kind
        o["record"] = a.int("--record", 11, 0, 255)
        o["first"] = a.int("--first", 1, 1, 255)
        o["last"] = a.int("--last", 8, 1, 255)
        if o["first"] > o["last"]:
            raise Usage("--first is larger than --last")
        o["from"] = parse_time("--from", a.str("--from"), "a")
        o["to"] = parse_time("--to", a.str("--to"), "a")
        if o["from"] > o["to"]:
            raise Usage("--from is later than --to")
    elif op == "set-time":
        o["time"] = parse_time("--time", a.str("--time"), "b")
    elif op == "read-tariff":
        o["register"] = a.int("--register", 134, 0, 255)
        f, t = a.str("--from"), a.str("--to")
        if (f is None) != (t is None):
            raise Usage("read-tariff: --from and --to go together")
        o["from"] = parse_time("--from", f, "a") if f is not None else None
        o["to"] = parse_time("--to", t, "a") if t is not None else None
        if f is not None and o["from"] > o["to"]:
            raise Usage("--from is later than --to")
    a.finish()
    return o


# ---------------------------------------------------------------------------
# The client
# ---------------------------------------------------------------------------

class Failure(Exception):
    def __init__(self, code, message, exit_code=EXIT_FAILED, confirmations=None, terminated=False):
        super().__init__(message)
        self.code, self.message, self.exit_code = code, message, exit_code
        self.confirmations, self.terminated = confirmations or [], terminated


def mirrors(frames, type_id):
    return [f for f in frames if f.tipo == type_id]


def is_confirmation(f):
    return f.causa_tm == 7 or f.causa_tm in REFUSALS or bool(f.pn)


def is_negative(f):
    return bool(f.pn) or f.causa_tm in REFUSALS


class Client:
    def __init__(self, o):
        self.o = o
        self.line = Line(o["host"], o["port"], o["connect_timeout"], o["timeout"])
        self.link = Link(o["link"], o["ca"])
        self.link.initialize(self.line)
        self.app = protocol.AppLayer()
        self.app.set_content_timeout(o["timeout"])
        self.app.initialize(self.link)
        self.link.app = self.app
        self.connected = False

    def classify(self, err, frames):
        """The failure an exception of the library or the line stands for."""
        if any(is_negative(f) for f in frames):
            return Failure("negative-confirmation", "the station refused the request: %s" % err)
        if isinstance(err, LineLost) or self.line.lost:
            return Failure("connection-lost", "the connection closed during the operation")
        if isinstance(err, (LineTimeout, protocol.NoContentTimeoutException)):
            return Failure("timeout", "the station did not answer in time")
        return Failure("failed", "%s: %s" % (type(err).__name__, err))

    def call(self, fn, *args):
        """Runs a request of the library; returns the variable frames it received."""
        start = len(self.link.asdus)
        self.app.reset_content_received()
        try:
            fn(*args)
        except Exception as e:  # the library raises plain Exception for unknown causes
            raise self.classify(e, self.link.asdus[start:]) from e
        return self.link.asdus[start:]

    def start(self):
        try:
            self.line.connect()
        except OSError as e:
            raise Failure("connect-failed", "no TCP connection: %s" % e, EXIT_CONNECT) from e
        try:
            self.link.link_state_request()
            self.link.remote_link_reposition()
        except Exception as e:
            if self.line.lost:
                raise Failure("connection-lost", "the connection closed during the link start-up", EXIT_CONNECT) from e
            raise Failure("link-init-timeout", "the link start-up was not confirmed: %s" % e, EXIT_CONNECT) from e
        self.connected = True

    def login(self):
        got = self.call(self.app.authenticate, self.o["key"])
        conf = [f for f in mirrors(got, app_asdu.C_AC_NA_2.type) if is_confirmation(f)]
        if not conf or is_negative(conf[-1]):
            raise Failure("login-refused", "the station refused the access key")

    def logout(self):
        start = len(self.link.asdus)
        self.app.reset_content_received()
        self.app.finish_session()  # the library logs and swallows its own errors
        conf = [f for f in mirrors(self.link.asdus[start:], app_asdu.C_FS_NA_2.type) if is_confirmation(f)]
        return bool(conf) and not is_negative(conf[-1])

    def request(self, user_data, record, terminated):
        """create_asdu_request and the polls as the library's read methods do them."""
        def run():
            asdu = self.app.create_asdu_request(user_data, record)
            list(self.app.process_request(asdu))
            if terminated and not any(is_negative(f) for f in mirrors(self.link.asdus, user_data.type)):
                for _ in self.app.process_requestresponse():
                    pass
        return self.call(run)

    def operation(self):
        """Runs the operation; returns (confirmations, terminated)."""
        o, op = self.o, self.o["op"]
        if op == "connect":
            return [], False
        if op == "read-time":
            got = self.call(self.app.read_datetime)
            return self.expect(got, None, app_asdu.M_TI_TA_2.type)
        if op == "read-product":
            got = self.call(self.app.get_info)
            return self.expect(got, None, app_asdu.P_MP_NA_2.type)
        if op == "read-totals":
            cls = app_asdu.C_CI_NT_2 if o["kind"] == "absolute" else app_asdu.C_CI_NU_2
            req = cls(o["from"], o["to"])
            req.primer_integrado, req.ultimo_integrado = o["first"], o["last"]
            got = self.request(req, o["record"], True)
            return self.expect(got, req.type, None, terminated=True)
        if op == "set-time":
            req = app_asdu.C_CS_TA_2(o["time"])
            got = self.request(req, 0, False)
            return self.expect(got, req.type, None)
        if op == "read-tariff":
            if o["from"] is None:
                req = app_asdu.C_TA_VC_2()
            else:
                req = app_asdu.C_TA_VM_2(o["from"], o["to"])
            got = self.request(req, o["register"], True)
            return self.expect(got, req.type, None, terminated=True)
        raise Failure("failed", "operation %s not implemented" % op)

    @staticmethod
    def expect(got, req_type, answer_type, terminated=False):
        confs = [f for f in got if req_type is not None and f.tipo == req_type and is_confirmation(f)]
        term = any(f.tipo == req_type and f.causa_tm == 10 for f in got) if req_type is not None else False
        conf_docs = [{"cot": f.causa_tm, "negative": is_negative(f)} for f in confs]
        if any(is_negative(f) for f in got):
            raise Failure("negative-confirmation", "the station refused the request", EXIT_FAILED, conf_docs, term)
        if answer_type is not None:
            if not any(f.tipo == answer_type and f.causa_tm == 5 for f in got):
                raise Failure("timeout", "the answer did not arrive", EXIT_FAILED, conf_docs, term)
            return conf_docs, term
        if not confs:
            raise Failure("timeout", "no confirmation", EXIT_FAILED, conf_docs, term)
        if terminated and not term:
            raise Failure("timeout", "no activation termination", EXIT_FAILED, conf_docs, term)
        return conf_docs, term


def run_client(o):
    t0 = time.monotonic()
    c = Client(o)
    doc = {"schemaVersion": SCHEMA_VERSION, "adapter": ADAPTER, "operation": o["op"], "ok": False, "error": None,
           "connected": False, "confirmations": [], "terminated": False}
    session = {"login": False, "logout": False} if o["ree"] else None
    failure = None
    try:
        c.start()
        if o["ree"]:
            c.login()
            session["login"] = True
        try:
            doc["confirmations"], doc["terminated"] = c.operation()
        except Failure as f:
            failure = f
            doc["confirmations"], doc["terminated"] = f.confirmations, f.terminated
            if f.code == "negative-confirmation" and not doc["confirmations"]:
                req = [x for x in c.link.asdus if is_negative(x)]
                doc["confirmations"] = [{"cot": x.causa_tm, "negative": True} for x in req[-1:]]
        if o["ree"] and not c.line.lost:
            session["logout"] = c.logout()
            if failure is None and not session["logout"]:
                failure = Failure("timeout", "the end of the session was not confirmed")
    except Failure as f:
        failure = f
    finally:
        c.line.disconnect()
    doc["connected"] = c.connected
    if session is not None:
        doc["session"] = session
    doc["asdus"] = [asdu_doc(f) for f in c.link.asdus]
    doc["elapsedMs"] = round((time.monotonic() - t0) * 1000, 1)
    if failure is None:
        doc["ok"] = True
        emit(doc)
        return EXIT_OK
    doc["error"] = {"code": failure.code, "message": failure.message}
    log.warning("%s: %s", failure.code, failure.message)
    emit(doc)
    return failure.exit_code


# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------

def print_fixture(argv):
    name = argv[0] if argv else "baseline"
    if len(argv) > 1 or not re.fullmatch(r"[a-z0-9-]+", name):
        raise Usage("print-fixture: '%s' is not a fixture name" % name)
    path = os.path.join(FIXTURES, name, "fixture.json")
    try:
        with open(path, "rb") as f:
            data = f.read()
    except OSError:
        log.warning("print-fixture: no fixture named '%s' in this image", name)
        return EXIT_FAILED
    sys.stdout.buffer.write(data)
    sys.stdout.flush()
    return EXIT_OK


def main(argv):
    level = logging.DEBUG if os.environ.get("VERBOSE") else logging.WARNING
    logging.basicConfig(level=level, stream=sys.stderr, format="[gisce] %(name)s: %(message)s")
    try:
        if not argv:
            raise Usage("a command is missing")
        cmd = argv[0]
        if cmd == "server":
            raise Usage("server: gisce/iec870ree has no controlled station; this image is a client only")
        if cmd == "client":
            return run_client(parse_client(argv[1:]))
        if cmd == "print-capabilities":
            if len(argv) > 1:
                raise Usage("print-capabilities takes no arguments")
            emit(capabilities())
            return EXIT_OK
        if cmd == "print-fixture":
            return print_fixture(argv[1:])
        if cmd == "version":
            print("%s %s (gisce/iec870ree %s, IEC 60870-5-102)" % (ADAPTER, ADAPTER_VERSION, UPSTREAM_VERSION))
            return EXIT_OK
        raise Usage("unknown command '%s'" % cmd)
    except Usage as e:
        sys.stderr.write("[gisce] usage: %s\n" % e)
        return EXIT_USAGE


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

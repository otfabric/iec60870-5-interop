#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""A scripted IEC 60870-5-102 meter for the self-test of the IEC 102 client images.

NOT an independent implementation of IEC 60870-5-102. There is no open
controlled station (meter) to run the client images against
(docs/REFERENCE_STACKS.md), so the smoke test of a client-only IEC 102 image
needs a peer, and this is it: written in this repository from
docs/CONTAINER_CONTRACT.md ("IEC 102") and docs/FIXTURES.md ("IEC 102"), with
the frame layouts those documents give. It answers exactly the requests the
contract's operations send and nothing else. Passing against it shows that an
image keeps the contract, not that the image or this script is right about
IEC 102; docs/COMPATIBILITY.md says so.

    meter.py --fixture PATH [--port 2401] [--link-address 1] [--ready-file PATH]

Unbalanced, a link address and a common address of two octets, a cause of
transmission of one octet. One connection at a time; the clock (and nothing
else) lives across connections. Prints the server events of the contract as
JSON Lines on stdout; stops cleanly on SIGTERM. Standard library only.
"""

import argparse
import datetime
import json
import os
import signal
import socket
import struct
import sys
import threading

# Type identifications (IEC 102; 133..136, 181, 183, 187 of the REE profile).
M_IT_TG_2, M_IT_TK_2, P_MP_NA_2, M_TI_TA_2 = 8, 11, 71, 72
C_RD_NA_2, C_TI_NA_2, C_CI_NT_2, C_CI_NU_2 = 100, 103, 122, 123
C_TA_VC_2, C_TA_VM_2, M_TA_VC_2, M_TA_VM_2 = 133, 134, 135, 136
C_CS_TA_2, C_AC_NA_2, C_FS_NA_2 = 181, 183, 187
# Causes.
REQUEST, ACT_CON, ACT_TERM = 5, 7, 10
UNKNOWN_TYPE, UNKNOWN_RECORD_ADDR, NO_OBJECT, NO_PERIOD = 14, 16, 17, 18
PN = 0x40


def event(name, **fields):
    sys.stdout.write(json.dumps(dict({"event": name}, **fields), separators=(",", ":")) + "\n")
    sys.stdout.flush()


# ---- time a and time b ----------------------------------------------------

def parse_local(s):
    fmt = "%Y-%m-%dT%H:%M:%S.%f" if len(s) > 16 else "%Y-%m-%dT%H:%M"
    return datetime.datetime.strptime(s, fmt)


def time_a(dt, su=False, iv=False):
    return bytes([
        dt.minute | (0x80 if iv else 0),
        dt.hour | (0x80 if su else 0),
        dt.day | (dt.isoweekday() << 5),
        dt.month,
        dt.year % 100,
    ])


def read_time_a(b):
    """The date and time fields of a time a, without the flags."""
    return (2000 + (b[4] & 0x7F), b[3] & 0x0F, b[2] & 0x1F, b[1] & 0x1F, b[0] & 0x3F)


def time_a_key(dt):
    return (dt.year, dt.month, dt.day, dt.hour, dt.minute)


def time_b(dt, su=False, iv=False):
    ms = dt.microsecond // 1000
    return struct.pack("<H", ms | (dt.second << 10)) + time_a(dt, su, iv)


def read_time_b(b):
    w = struct.unpack("<H", b[:2])[0]
    y, mo, d, h, mi = read_time_a(b[2:])
    dt = datetime.datetime(y, mo, d, h, mi, w >> 10, (w & 0x3FF) * 1000)
    return dt, bool(b[3] & 0x80), bool(b[2] & 0x80)


def text_b(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%S.") + "%03d" % (dt.microsecond // 1000)


# ---- the station ------------------------------------------------------------

class Station:
    def __init__(self, fx, link_address):
        self.fx = fx
        self.link = link_address
        st = fx["station"]
        self.ca = st["commonAddress"]
        self.key = st["accessKey"]
        self.clock = (parse_local(st["clock"]), False, False)  # time, SU, IV: stands still until set
        self.totals = {}
        for t in fx["totals"]:
            kind = M_IT_TG_2 if t["kind"] == "absolute" else M_IT_TK_2
            self.totals.setdefault(t["record"], {})[kind] = t["periods"]
        self.tariffs = {t["register"]: t for t in fx.get("tariffs", [])}

    def asdu(self, type_id, vsq, cause, record, body):
        return bytes([type_id, vsq, cause]) + struct.pack("<H", self.ca) + bytes([record]) + body

    def mirror(self, req, cause):
        return bytes([req[0], req[1], cause]) + req[3:]

    def serve(self, a):
        """The answers (class 2 data, in order) to a control-direction ASDU."""
        t, cause = a[0], a[2]
        ca = struct.unpack("<H", a[3:5])[0]
        record, data = a[5], a[6:]
        if ca != self.ca:
            event("refused", typeId=t, commonAddress=ca, cot=UNKNOWN_RECORD_ADDR)
            return [self.mirror(a, UNKNOWN_RECORD_ADDR)]
        if t == C_AC_NA_2:
            key = struct.unpack("<I", data[:4])[0] if len(data) >= 4 else None
            ok = key == self.key
            event("login", accepted=ok)
            return [self.mirror(a, ACT_CON | (0 if ok else PN))]
        if t == C_FS_NA_2:
            event("logout")
            return [self.mirror(a, ACT_CON)]
        if t == C_RD_NA_2:
            p = self.fx["station"]["product"]
            event("read-product")
            return [self.asdu(P_MP_NA_2, 1, REQUEST, record,
                              bytes([p["standard"], p["manufacturer"]]) + struct.pack("<I", p["product"]))]
        if t == C_TI_NA_2:
            event("read-time", time=text_b(self.clock[0]))
            return [self.asdu(M_TI_TA_2, 1, REQUEST, record, time_b(*self.clock))]
        if t == C_CS_TA_2 and len(data) == 7:
            self.clock = read_time_b(data)
            event("set-time", time=text_b(self.clock[0]), su=self.clock[1])
            return [self.mirror(a, ACT_CON)]
        if t in (C_CI_NT_2, C_CI_NU_2) and len(data) == 12:
            return self.read_totals(a, record, data)
        if t == C_TA_VC_2 or (t == C_TA_VM_2 and len(data) == 10):
            return self.read_tariff(a, record, data)
        event("refused", typeId=t, commonAddress=ca, cot=UNKNOWN_TYPE)
        return [self.mirror(a, UNKNOWN_TYPE)]

    def read_totals(self, a, record, data):
        kind = M_IT_TG_2 if a[0] == C_CI_NT_2 else M_IT_TK_2
        first, last = data[0], data[1]
        lo, hi = read_time_a(data[2:7]), read_time_a(data[7:12])

        def refuse(cot):
            event("read-totals", typeId=a[0], record=record, cot=cot)
            return [self.mirror(a, cot)]

        if record not in self.totals:
            return refuse(UNKNOWN_RECORD_ADDR)
        if kind not in self.totals[record]:
            return refuse(UNKNOWN_TYPE)
        periods = [p for p in self.totals[record][kind] if lo <= time_a_key(parse_local(p["end"])) <= hi]
        if not periods:
            return refuse(NO_PERIOD)
        out = []
        for p in periods:
            objs = [o for o in p["totals"] if first <= o["address"] <= last]
            if not objs:
                continue
            body = b"".join(bytes([o["address"]]) + struct.pack("<I", o["value"]) + bytes([o["quality"]]) for o in objs)
            out.append(self.asdu(kind, len(objs), REQUEST, record, body + time_a(parse_local(p["end"]))))
        if not out:
            return refuse(NO_OBJECT)
        event("read-totals", typeId=a[0], record=record, cot=ACT_CON, periods=len(out))
        return [self.mirror(a, ACT_CON)] + out + [self.mirror(a, ACT_TERM)]

    def read_tariff(self, a, register, data):
        t = self.tariffs.get(register)
        if t is None:
            event("read-tariff", typeId=a[0], register=register, cot=UNKNOWN_RECORD_ADDR)
            return [self.mirror(a, UNKNOWN_RECORD_ADDR)]
        if a[0] == C_TA_VC_2:
            recs, answer = t["current"], M_TA_VC_2
        else:
            lo, hi = read_time_a(data[0:5]), read_time_a(data[5:10])
            recs = [r for r in t["stored"] if lo <= time_a_key(parse_local(r["periodEnd"])) <= hi]
            answer = M_TA_VM_2
        if not recs:
            event("read-tariff", typeId=a[0], register=register, cot=NO_PERIOD)
            return [self.mirror(a, NO_PERIOD)]
        out = [self.mirror(a, ACT_CON)]
        for r in recs:  # one record per ASDU
            u = lambda v: struct.pack("<I", v)  # noqa: E731
            body = (bytes([r["address"]])
                    + u(r["activeAbs"]) + u(r["activeInc"]) + bytes([r["activeQuality"]])
                    + u(r["reactiveIndAbs"]) + u(r["reactiveIndInc"]) + bytes([r["reactiveIndQuality"]])
                    + u(r["reactiveCapAbs"]) + u(r["reactiveCapInc"]) + bytes([r["reactiveCapQuality"]])
                    + u(0) + bytes([0]) + u(0) + bytes([0])  # reserved 7 and 8
                    + u(r["maxPower"]) + time_a(parse_local(r["maxPowerTime"])) + bytes([r["maxPowerQuality"]])
                    + u(r["excessPower"]) + bytes([r["excessPowerQuality"]])
                    + time_a(parse_local(r["periodStart"])) + time_a(parse_local(r["periodEnd"])))
            out.append(self.asdu(answer, 1, REQUEST, register, body))
        out.append(self.mirror(a, ACT_TERM))
        event("read-tariff", typeId=a[0], register=register, cot=ACT_CON, records=len(recs))
        return out


# ---- FT 1.2 and the secondary link procedures --------------------------------

class Line:
    """One connection: the link starts afresh."""

    def __init__(self, conn, station):
        self.conn, self.st = conn, station
        self.buf = bytearray()
        self.queue = []
        self.fcb = None
        self.last = None

    def fixed(self, fn):
        c = fn  # PRM 0, ACD 0, DFC 0
        addr = struct.pack("<H", self.st.link)
        return bytes([0x10, c]) + addr + bytes([(c + sum(addr)) & 0xFF, 0x16])

    def variable(self, fn, asdu):
        body = bytes([fn]) + struct.pack("<H", self.st.link) + asdu
        return bytes([0x68, len(body), len(body), 0x68]) + body + bytes([sum(body) & 0xFF, 0x16])

    def frames(self):
        """Complete frames from the buffer; drops garbage octets."""
        while True:
            while self.buf and self.buf[0] not in (0x10, 0x68):
                del self.buf[0]
            if not self.buf:
                return
            if self.buf[0] == 0x10:
                if len(self.buf) < 6:
                    return
                f, self.buf = bytes(self.buf[:6]), self.buf[6:]
                if f[5] == 0x16 and sum(f[1:4]) & 0xFF == f[4]:
                    yield f[1], struct.unpack("<H", f[2:4])[0], None
                continue
            if len(self.buf) < 4:
                return
            n = self.buf[1]
            if self.buf[2] != n or self.buf[3] != 0x68:
                del self.buf[0]
                continue
            if len(self.buf) < n + 6:
                return
            f, self.buf = bytes(self.buf[:n + 6]), self.buf[n + 6:]
            body = f[4:4 + n]
            if f[-1] == 0x16 and sum(body) & 0xFF == f[4 + n] and n >= 3:
                yield body[0], struct.unpack("<H", body[1:3])[0], body[3:]

    def answer(self, c, data):
        if not c & 0x40:  # not from a primary station
            return None
        fn, fcv, fcb = c & 0x0F, bool(c & 0x10), bool(c & 0x20)
        if fn == 9:
            return self.fixed(11)  # status of link
        if fn == 0:
            self.queue, self.fcb, self.last = [], None, None
            return self.fixed(0)
        if fcv and self.fcb is not None and fcb == self.fcb and self.last is not None:
            return self.last  # repetition: the same answer again
        if fcv:
            self.fcb = fcb
        if fn == 3 and data is not None:
            self.last = self.fixed(0)
            if len(data) >= 6:
                self.queue.extend(self.st.serve(data))
            return self.last
        if fn in (10, 11):
            self.last = self.variable(8, self.queue.pop(0)) if fn == 11 and self.queue else self.fixed(9)
            return self.last
        return None  # function not implemented: no answer

    def run(self):
        while True:
            try:
                chunk = self.conn.recv(1024)
            except OSError:
                return
            if not chunk:
                return
            self.buf.extend(chunk)
            for c, addr, data in self.frames():
                if addr != self.st.link:
                    continue
                out = self.answer(c, data)
                if out is not None:
                    self.conn.sendall(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--fixture", required=True)
    ap.add_argument("--port", type=int, default=2401)
    ap.add_argument("--link-address", type=int, default=1)
    ap.add_argument("--ready-file", default="/tmp/meter-ready")
    args = ap.parse_args()
    with open(args.fixture) as f:
        fx = json.load(f)
    st = Station(fx, args.link_address)

    ln = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    ln.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    ln.bind(("0.0.0.0", args.port))
    ln.listen(4)
    busy = threading.Lock()

    def stop(*_):
        event("stopped")
        os._exit(0)

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    event("ready", adapter="scripted-meter", address="0.0.0.0:%d" % args.port, fixture=fx["name"],
          commonAddress=st.ca, standard="iec60870-5-102", mode="unbalanced", linkAddress=args.link_address)
    with open(args.ready_file, "w"):
        pass

    def handle(conn, peer):
        try:
            event("connection-opened", peer="%s:%d" % peer)
            conn.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
            Line(conn, st).run()
        finally:
            conn.close()
            event("connection-closed", peer="%s:%d" % peer)
            busy.release()

    while True:
        conn, peer = ln.accept()
        if not busy.acquire(blocking=False):
            conn.close()  # one line, one controlling station
            continue
        threading.Thread(target=handle, args=(conn, peer), daemon=True).start()


if __name__ == "__main__":
    main()

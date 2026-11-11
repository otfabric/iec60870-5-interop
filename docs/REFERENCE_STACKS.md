# Reference stack survey

Candidates for independent IEC 60870-5 reference implementations, assessed
from their source code on 2026-11-10/11 (clones of the SHAs below). Nothing
in this document was built or run unless a row says so; every "yes" is a
plan, not evidence. The IEC 103 rows were built and run. A stack becomes a reference only after its adapter
passes the self-tests (`status: validated` in `versions.yaml`).

Verdicts: **yes** = suitable as an independent oracle; **with adapter work**
= suitable once wrapped in the container contract; **no** = not usable, with
the reason.

## IEC 60870-5-101

| Stack | Licence | Roles / modes | Transport | Verdict |
|---|---|---|---|---|
| [mz-automation/lib60870](https://github.com/mz-automation/lib60870) C, v2.4.1 `7a388e3e` | GPL-3.0 (or commercial) | master + slave, balanced + unbalanced | serial (termios HAL) only | **yes, with adapter work**: C driver + socat pty bridge (pattern proven by cB-Sigma `interop/peer` and digitalpetri `docker/lib60870c`). Issue #204: Linux master drops frames slower than its 300 ms character timeout (long frames below 9600 Bd); a pty bridge avoids it. |
| [digitalpetri/iec60870](https://github.com/digitalpetri/iec60870) Java 17, `33e52b3` (0.1.0-SNAPSHOT) | EPL-2.0 | balanced, unbalanced master, unbalanced slave | serial (jSerialComm) and native 101-over-TCP | **yes, with adapter work**: strongest independent 101 peer; small Java CLI from a pinned SHA |
| [cB-Sigma/iec60870](https://github.com/cB-Sigma/iec60870) Go, v0.3.0 `d0cad7ab` | Apache-2.0 | controlling + controlled, balanced + unbalanced, shared line | any io.ReadWriteCloser | **yes, with adapter work**, as third peer: debugged against lib60870, so not independent evidence on its own |
| mz-automation/lib60870.NET `4ad2923d` | GPL-3.0 | master + slave | serial, TCP | no: same vendor and design as lib60870-C |
| dscsystems/rs-iec60870-5 Rust `8f3274e` | DSC Source-Available 1.0 | 101 primary/secondary, 103 master | serial, TCP | CI-built private peer only: licence forbids binary redistribution |
| FreyrSCADA/IEC-60870-5-101 | proprietary eval SDK | | | no |
| roboplc/iec60870-5, pascaldekloe/part5 (FT1.2 codec only), paopaol/QIEC60870, openmuc j60870 (104 only) | | | | no: not a 101 link layer, or licence |

Licence note (GPL-3.0 references): running lib60870 in CI is not
distribution. A published image of a driver linked against it is conveying:
the image ships its licence texts and the public Dockerfile, pinned upstream
tag and driver source are the corresponding source (GPL-3.0 section 6). The
production library only talks to it over the wire.

## IEC 60870-5-102

No open, licensed, general IEC 60870-5-102 implementation exists. Every
open stack implements the Spanish REE electricity meter profile, and all of
them are controlling stations (masters) only.

| Stack | Licence | Roles | Type IDs seen in code | Verdict |
|---|---|---|---|---|
| [gisce/iec870ree](https://github.com/gisce/iec870ree) Python `993242da` (PyPI 0.14.1) | AGPL-3.0 | master | 1, 8, 11, 71, 72, 100, 102, 103, 122, 123, 133-136, 140-142, 144, 145, 147, 148, 150, 151, 156, 157, 162, 163, 181, 183, 187, 190 | **adapter `iec102-gisce`** (contract 2.2, experimental): controlling-station oracle for the REE profile against a controlled station under test; link and common address of two octets only; cannot parse the single character E5h |
| javierdelapuente/reeprotocol `d64166ce` | AGPL-3.0 | master | 183, 187, 123, 11 | no: archived, superseded by gisce/iec870ree |
| daiiniel/IEC60870-5-102 C# `a268d45b` | **none** | master | REE profile (183 login, 187 logout, ...) | no: no licence (all rights reserved) |
| sennet-info/simulador_IEC102 `6cca515` | **none** | meter (controlled) simulator, PySide6 GUI | 8, 11, 71, 72, 100, 103, 122, 123, 156, 157, 162, 163, 183, 187 | no: no licence, GUI |
| 0x7A77/IEC Go | none | master | | no: no licence |

Consequences: the IEC 102 controlled-station role can be checked against
gisce/iec870ree (the image `iec102-gisce`); the controlling-station role has
no independent peer and is validated by specification-derived tests and
scripted peers (fixtures), documented as such. The harness's own self-test
of the gisce image runs against such a scripted peer, `tests/iec102/meter.py`
(docs/COMPATIBILITY.md). Wireshark has no IEC 102 dissector. The base-standard
type table is not verified against IEC 60870-5-102 (we do not hold the
text): see go-iec60870-5 docs/STANDARDS.md.

## IEC 60870-5-103

Re-assessed on 2026-11-11 by building and running every candidate (clones of
the SHAs below, Docker on linux/arm64; the four adapter images were also built
for linux/amd64 under emulation). Wire evidence is in
[COMPATIBILITY.md](COMPATIBILITY.md#iec-60870-5-103) and
`tests/iec103/known-bugs.tsv`. "Server" is the protection equipment
(secondary station), "client" the control system (primary).

| Stack | Licence | Built / ran | Roles here | Verdict |
|---|---|---|---|---|
| [vitusdong/iec103-dotnet](https://github.com/vitusdong/iec103-dotnet) C# v0.1.0 `38aa54ce` | MIT | net8 build, runs headless; TCP natively (no pty) | server, client | **adapter `iec103-vitusdong`, validated**. The only fixture-driven server of the survey, and far from the standard on the wire: interrogation replies with cause 11 and a 12-octet ASDU 1 (DPI, `FF FF FF FF`, CP56Time2a, no SIN), command confirmation as ASDU 1 cause 11 (139 for a refusal) instead of 20/21, identification without COL or software id (FUN 255, INF 80/81); measurands only as one raw value in ASDU 9; no GI flag; ignores the common address (looks the station up by link address); clock synchronization request with FUN 0. Not independent of ARIEC103: its tests use a copy of the ArIEC103 slave simulator as the oracle |
| [masarray/ARIEC103](https://github.com/masarray/ARIEC103) C# `ec08dd73` (v1.2.31-3; props 1.3.0) | Apache-2.0 | net8 CLI builds and runs headless; **its serial transport fails on a pty**: `SerialPort` with `RtsEnable = true` throws `IOException: Inappropriate ioctl for device` in `RtsEnabledNative` before the first frame (socat pty, run 2026-11-11). Runs over TCP through its public `IByteTransport` | client | **adapter `iec103-ariec103`, validated** (client only). Master sends reset of link and reset of FCB, GI with SCN, class 1/2 polls; clock synchronization ASDU 6 carries a CP32Time2a plus one octet (5 octets, known bug), general command has no RII. The slave simulator (ran over TCP with an `IByteTransport` of ours) has a model of its own and cannot serve a fixture: fixed FUN 160/161 INF 1..3 points, animated ASDU 9 currents (one value plus a stray `00` octet), random pickup/trip events unless disabled, identification with cause 3 FUN 100 INF 2 and 24 characters, GI termination always with SCN 0, ACK (function 0) instead of status of link, no confirmation of commands or clock synchronization |
| [masarray/ARIEC60870](https://github.com/masarray/ARIEC60870) C# `37baba35` (v3.6.8-rc.2+10) | Apache-2.0 | net8 CLI `ARIEC60870.Cli` exists (headless, 103 master and slave simulator, serial); not built separately | - | no: its 103 code is ARIEC103's (the same `Iec103MasterSession` and slave simulator, renamed); not a second peer |
| [riclolsen/go-iecp5](https://github.com/riclolsen/go-iecp5) Go v0.4.7 `fbe6719d`, package cs103 | LICENSE file LGPL-3.0; README and cs103 file headers GPL-3.0 | builds with Go 1.27, runs headless; TCP client transport of its own (`cs101.TransportTCPClient`) | client | **adapter `iec103-goiecp5`, validated and published** (either licence reading allows the GPL-3.0-or-later image; the maintainer decided to publish it). Smoke and cross-stack passed. The most standard-conforming master of the survey: request status of link, reset CU, FCB 1 after reset, ASDU 1/2/3/4/5/8/9 codecs as the standard lays them out, RII matched to SIN, FUN 255 in ASDU 6/7. No SEND/NO REPLY (broadcast). Independence: fork of the thinkgos/wendy512 go-iecp5 lineage (101/104), cs103 written by the same author as dscsystems/rs-iec60870-5; no trace of ARIEC103 or vitusdong in the sources |
| [XVincentX/Open103](https://github.com/XVincentX/Open103) C++ `887ff5a5` | Apache-2.0 | **compiles on Linux unchanged** (g++ 12 and 14) with a shim of ours: `OutputDebugStringA`, `localtime_s`, the include guard of the Windows-only `gettimeofday.h`, and `Logger::ToString`, which Open103 calls but defines nowhere in the repository; TCP `CommunicationPort` that returns one frame per `Read` | client | **adapter `iec103-open103`, validated** (client only, driven through P/Invoke). Defects seen in the source: `StatusLink` toggles the FCB though FCV is 0; `(Control & 0x40) == 1` (PRM/DFC checks) is always false; the function-type switch of `CommandTrasmission` falls through; `StationInit` polls class 1 forever when the station has no user data (the shim bounds every call); `TimeSync` takes whole seconds. It refuses to send a command whose INF does not fit the function type of the identification, so against vitusdong (FUN 255) it sends no command |
| [TheBam1990/ioBroker.iec103](https://github.com/TheBam1990/ioBroker.iec103) JS `b1e491a5` (0.1.6) | MIT | runs without ioBroker with a stand-in for `@iobroker/adapter-core` (20 lines) | - | no: not an IEC 103 link layer. Master: no reset of link, constant FCB (`5A`, `7B` every time), class 1 and class 2 requests sent back to back without waiting, GI with cause 6 and no SCN. Slave: does not answer link requests at all (goiecp5 got `link-init-timeout`); answers a GI with `E5` and then sends the ASDUs as unsolicited primary frames (`73`), which unbalanced transmission does not allow |
| dscsystems/rs-iec60870-5 `cs103` | source-available | not built | - | CI-built private peer only (licence forbids redistribution) |
| themeyic/go-iec103 | MIT | - | - | no: a renamed Modbus client |

Consequences: the IEC 103 client role of a library under test has one
fixture-driven reference server (vitusdong), whose replies deviate from the
standard in the ways listed; the server role has four independent clients
(goiecp5, ariec103, open103, vitusdong), of which goiecp5 is the closest to
the standard. No open stack offers a standard-conforming IEC 103 secondary
station; the ARIEC103 simulator is the second independent server on the wire
but not fixture-driven ([ROADMAP.md](ROADMAP.md)).

Generic services (ASDU 10, 11, 21) and disturbance data (ASDU 23 to 31),
read again in the pinned sources for contract 2.3 (2026-11-11; file and line
references in [CAPABILITIES.md](CAPABILITIES.md#features-of-the-iec-103-images)):

| Stack | Generic services | Disturbance data |
|-------|------------------|------------------|
| vitusdong | master and slave (read of entries, write; every generic ASDU with INF 0) | master (list, a procedure of its own that is not clause 7.4.7's, tags separately) and slave (list, records with generated values and the station's indications as tags) |
| ARIEC103 | no | no (type 23 named in its decoder only) |
| Open103 | no (`GenericService()` returns false) | master: the procedure of clause 7.4.7 driven by the ASDU 23 the station sends, after the GI; NFE of ASDU 30 read through an unpacked struct (known bug); `DisturbanceData` lacks a return for most types (undefined behaviour the shim avoids) |
| go-iecp5 cs103 | no (type identifications only) | no ("not implemented by this package") |

vitusdong and ARIEC103 are not independent of each other (vitusdong's tests
use a copy of ArIEC103 as their oracle), but ARIEC103 has neither function,
so that does not touch the evidence of contract 2.3. Open103, judged usable
only in part for the basic functions, is usable for the transmission of
disturbance data as a control system: it ran the whole procedure against an
independent protection equipment (the consumer run in COMPATIBILITY.md).

## Passive decoding oracle

Wireshark (`epan/dissectors/packet-iec104.c`, master `55b5def4`) dissects
IEC 101 (`iec60870_101`) and IEC 103 (`iec60870_5_103`) over TCP via
Decode As (and SEL RTAC serial captures), with preferences for the 101 field
sizes. 103 field dissection covers ASDU 1, 5, 6, 7, 8, 9, 20, 205 only.
`tshark -d tcp.port==N,iec60870_101` therefore decodes TCP-wrapped serial
traffic headlessly. There is no IEC 102 dissector.

## IEC 60870-5-104

The references of otfabric/iec104-interop v0.6.0 (lib60870-C v2.4.1,
OpenMUC j60870 1.8.0, wendy512/iec104 v1.0.4, cB-Sigma/iec60870 v0.3.0,
pascaldekloe/part5 v0.3.0) are carried over as adapter sources; see
ROADMAP.md.

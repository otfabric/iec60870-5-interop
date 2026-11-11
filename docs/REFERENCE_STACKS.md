# Reference stack survey

Candidates for independent IEC 60870-5 reference implementations, assessed
from their source code on 2026-11-10/11 (clones of the SHAs below). Nothing
in this document was built or run unless a row says so; every "yes" is a
plan, not evidence. A stack becomes a reference only after its adapter
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
| [gisce/iec870ree](https://github.com/gisce/iec870ree) Python `993242da` (PyPI 0.14.1) | AGPL-3.0 | master | 1, 8, 11, 71, 72, 100, 102, 103, 122, 123, 133-136, 140-142, 144, 145, 147, 148, 150, 151, 156, 157, 162, 163, 181, 183, 187, 190 | **with adapter work**: Python CLI wrapper; controlling-station oracle for the REE profile against our controlled station |
| javierdelapuente/reeprotocol `d64166ce` | AGPL-3.0 | master | 183, 187, 123, 11 | no: archived, superseded by gisce/iec870ree |
| daiiniel/IEC60870-5-102 C# `a268d45b` | **none** | master | REE profile (183 login, 187 logout, ...) | no: no licence (all rights reserved) |
| sennet-info/simulador_IEC102 `6cca515` | **none** | meter (controlled) simulator, PySide6 GUI | 8, 11, 71, 72, 100, 103, 122, 123, 156, 157, 162, 163, 183, 187 | no: no licence, GUI |
| 0x7A77/IEC Go | none | master | | no: no licence |

Consequences: the IEC 102 controlled-station role can be checked against
gisce/iec870ree; the controlling-station role has no independent peer and
is validated by specification-derived tests and scripted peers (fixtures),
documented as such. Wireshark has no IEC 102 dissector. The base-standard
type table is not verified against IEC 60870-5-102 (we do not hold the
text): see go-iec60870-5 docs/STANDARDS.md.

## IEC 60870-5-103

| Stack | Licence | Roles | Transport | ASDUs | Verdict |
|---|---|---|---|---|---|
| [masarray/ARIEC103](https://github.com/masarray/ARIEC103) C# `ec08dd73` (v1.3.0) | Apache-2.0 | master + slave simulator (CLI `slave`) | serial only (System.IO.Ports) | decode 1, 2, 5-9, 20, 23; master sends 6, 7, 20, class polls; slave emits 1, 5, 8, 9 | **with adapter work**: Linux net8.0 build, socat pty, JSON wrapper; RtsEnable on a pty unverified |
| [vitusdong/iec103-dotnet](https://github.com/vitusdong/iec103-dotnet) C# `38aa54c` | MIT | master + slave (multi-master TCP) | TCP, serial, any Stream | 1, 2, 5-11, 20, 21, 23-31 (generic services, disturbance transfer) | **with adapter work**; widest coverage. Not independent of ARIEC103 (its test oracle is a copy of it). COT 11/12 mapping differs from rs-iec60870-5: to be checked against the standard (103 table 5: 11 local operation, 12 remote operation). |
| dscsystems/rs-iec60870-5 `cs103` | source-available | master | serial, TCP | 1-11, 20, 21 | CI-built private peer only |
| TheBam1990/ioBroker.iec103 `b1e491a` | MIT | sniffer, master, slave | TCP, serial | | possible, heavy (ioBroker runtime) |
| masarray/ARIEC60870 `59ebc36` | Apache-2.0 | Windows desktop tester 101/103/104 | | | not inspected for headless use |
| XVincentX/Open103 `887ff5a5` | Apache-2.0 | master | abstract port | | no: Windows-only headers, does not compile (`Logger::ToString` undefined), DFC test always false |
| themeyic/go-iec103 | MIT | | | | no: a renamed Modbus client |

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

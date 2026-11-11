# Compatibility

What the self-tests of this repository showed, with exact counts. A count is
the number of outcomes in `reports/results.json` (interop-result-v1); only
`pass` passes. Every run below was made locally on 2026-11-11 on Docker for
macOS (linux/arm64 engine) with the `:dev` images built from this commit's
sources. Nothing has run on amd64 or in CI yet.

Reproduce: `make images`, then `make documents` (or the commands per row).

`reports/results.json` of these runs: 4014 pass, 0 fail, 583 unsupported,
44 known-difference, 0 infra-error. The 7 capability documents and 1647
client result documents kept in `reports/` validate against `schemas/`
(`scripts/validate-documents.py`, 0 errors). The IEC 101 rows are the final
runs on the committed sources; the IEC 104 images were built before the last
edit of THIRD_PARTY_NOTICES.md (which they copy to `/licenses`), with
otherwise identical sources.

## IEC 60870-5-101

| Run | Command | pass | fail | other |
|-----|---------|-----:|-----:|------:|
| smoke lib60870, unbalanced | `LINK_MODE=unbalanced scripts/smoke.sh iec101 lib60870` | 46 | 0 | 0 |
| smoke lib60870, balanced | `LINK_MODE=balanced scripts/smoke.sh iec101 lib60870` | 46 | 0 | 0 |
| smoke cbsigma, unbalanced | `LINK_MODE=unbalanced scripts/smoke.sh iec101 cbsigma` | 46 | 0 | 0 |
| smoke cbsigma, balanced | `LINK_MODE=balanced scripts/smoke.sh iec101 cbsigma` | 46 | 0 | 0 |
| cross-stack, unbalanced | `LINK_MODE=unbalanced scripts/interop.sh iec101 lib60870 cbsigma` | 245 | 0 | 0 |
| cross-stack, balanced | `LINK_MODE=balanced scripts/interop.sh iec101 lib60870 cbsigma` | 245 | 0 | 0 |

A cross-stack run is every client against every server (lib60870 →
lib60870, cbsigma → lib60870, lib60870 → cbsigma, cbsigma → cbsigma) on the
31 cases of `tests/iec101/cases.tsv`: 124 outcome checks plus 93 checks that
all four pairings return the same normalized result document, then 20 value
checks (interrogation and counter interrogation return the fixture, a set
point is reported and read back, a single command shows in the next
interrogation) and 8 session checks (a line idle for 3 s stays up; the next
connection is served). Both cross-stack runs were repeated once more without
keeping documents, with the same result (245/0 each).

So in both link modes lib60870-C v2.4.1 and cB-Sigma/iec60870 v0.3.0 agree,
in both roles, on: link start-up, general and counter interrogation
(including the broadcast common address 255 and the refusal of an unknown
one with cause 46), read (and cause 47 for an unknown address), clock
synchronization, the test command C_TS_NA_1, direct, select, select-before-
operate and cancelled commands of all six command types, the refusals of the
fixture (select required, invalid double and step states, unknown object),
and the spontaneous M_*_TB_1 report after a command.

The smoke tests also exercise, per adapter against itself: two-octet link
address, cause of transmission (with originator address 9 echoed) and common
address with a three-octet object address; a link that does not start
(`link-init-timeout`, exit 3); a second connection while one is served
(refused).

### Not shown by these runs

- The pair is not two fully independent oracles: cB-Sigma's IEC 101 link was
  debugged against lib60870-C (docs/REFERENCE_STACKS.md). A third peer
  (digitalpetri) is planned.
- Non-default field sizes were not run across stacks, only each adapter
  against itself.
- Balanced mode with distinct link addresses (`--remote-link-address` other
  than `--link-address`): cbsigma cannot, and no lib60870 pairing ran it.
- `--link-address-size 0` was not run.
- Interrogation groups and qualities on M_BO_NA_1 have no IEC 101 fixture yet.

### Observations

- A balanced lib60870 station (and station A) accepts frames whatever link
  address they carry; cB-Sigma ignores frames for another address. Both
  readings are possible on a point-to-point balanced line, so this is not
  listed as a defect.
- lib60870's CS101 slave keeps its link state machine and queues across what
  is, for it, one serial port. With one slave for the life of the process, a
  balanced lib60870 station lost the first ASDU (the confirmation) of most
  operations after the first connection, against a lib60870 controlling
  station; the likely cause is link state (frame count bit, primary link
  state) carried over from the previous connection. The adapter now creates a new slave for
  every connection (contract: every connection is a line that has just come
  up). This was a harness issue, not a defect of lib60870.
- Before that change and a client rebuild, one manual run of lib60870 (client)
  against cbsigma (server), balanced, lost the first ASDU of most operations.
  It did not reproduce afterwards: three manual repeats and three full
  cross-stack runs passed. The cause is not established.

### Known upstream defects

None found for IEC 101 (`tests/iec101/known-bugs.tsv` is empty).

## IEC 60870-5-102

**There is no open IEC 102 controlled station, so nothing here is a
cross-stack result.** The one image, `iec102-gisce` (gisce/iec870ree v0.14.1,
the controlling station of the Spanish REE profile), is smoke-tested against
`tests/iec102/meter.py`, a scripted meter written in this repository from
CONTAINER_CONTRACT.md and FIXTURES.md ("IEC 102"). The scripted meter is
**not an independent implementation of IEC 60870-5-102**: it answers the
frames the contract's operations send, as the contract describes them. A pass
shows that the image keeps the contract (flags, exit codes, documents) and
that gisce's requests and decoding agree with the contract's frames and the
fixture's values; it says nothing about how gisce meets real meters, and the
contract's frames are themselves an interpretation (we hold neither IEC
60870-5-102 nor an REE document). versions.yaml keeps the image
`experimental`.

Run locally on 2026-11-11 (linux/arm64, the `:dev` image of this commit),
with `REPORT_DIR` set: 61 pass, 0 fail, 0 other; the capability document and
the 18 client result documents kept validate against `schemas/` (0 errors).

| Run | Command | pass | fail | other |
|-----|---------|-----:|-----:|------:|
| smoke gisce (client, against the scripted meter) | `scripts/smoke.sh iec102 gisce` | 61 | 0 | 0 |

The 61: the capability document with its IEC 102 fields and the baked
fixture (11); 30 usage errors that never touch the network (every flag that
does not exist for IEC 102, field sizes the image lacks, REE operations
without `--profile ree`, malformed times, `server` of a client-only image);
a connection that cannot be made (exit 3); and 19 checks of operations
against the scripted meter, each compared with the fixture (one of them that
the result is one line): link start-up failure on another link address
(`link-init-timeout`), `connect` with and without the REE session, a wrong
access key (`login-refused`), `read-product`, `read-time` (the fixture's
clock), `read-totals` absolute, incremental in a session, and two periods of
addresses 1..3 (every period, address, value, quality octet and period end
equal to the fixture), four refusals (causes 18, 16, 17 and 16 for another
common address), `read-tariff` current, stored in a range and of a register
the station lacks (cause 16), `set-time` and the time read back.

What gisce sent, as its own log shows it (read-totals of one period in a
session):

```text
10 49 01 00 4A 16                         request status of link
10 40 01 00 41 16                         reset of remote link
68 0D 0D 68 73 01 00 B7 01 06 01 00 00 07 00 00 00 3A 16
                                          C_AC_NA_2, VSQ 1, cause 6, CA 1, record 0, key 7
10 5B 01 00 5C 16                         request class 2 (FCB alternating from here)
68 15 15 68 73 01 00 7A 01 06 01 00 0B 01 02 00 02 81 01 1A 00 02 81 01 1A 40 16
                                          C_CI_NT_2, record 11, addresses 1..2, 2026-01-01 02:00 (Thursday) twice
68 09 09 68 73 01 00 BB 00 06 01 00 00 36 16
                                          C_FS_NA_2, VSQ 0
```

### Not shown by these runs

- Any independent controlled station: none exists (REFERENCE_STACKS.md).
- One-octet link or common addresses: gisce has two octets only.
- The single character E5h: gisce cannot parse it (its frame parser raises
  on that start octet), so it needs a station that answers with fixed
  frames, as the contract's station does. Read in the source, not run.
- The read commands other than C_CI_NT_2 and C_CI_NU_2, single points
  (C_SP_NB_2), blocks (C_CB_UN_2), configuration, contracted powers and
  special days: no operation in contract 2.2.
- The image was built for linux/amd64 under emulation and printed its
  version there (2026-11-11); the smoke test ran on linux/arm64 only.

### Known upstream defects

None recorded (`tests/iec102/known-bugs.tsv` is empty). Seen in gisce's
source and handled by the adapter, not by the contract: `authenticate`
returns the first frame it receives and ignores the P/N bit (the adapter
reads the confirmation from the frames); its own TCP transport waits 5 s
after connecting and never notices a closed connection (the adapter brings
its own); `M_TA_VX_2` decodes the first tariff record of an ASDU only (the
contract's station sends one per ASDU); `read_absolute_values` and its
siblings fix the address range to 1..8 and the record to its register names
(the adapter sets them on the request object).

## IEC 60870-5-103

Run locally on 2026-11-11 (linux/arm64, `:dev` images of this commit), with
`REPORT_DIR` set; `results.json` of the IEC 103 runs: 182 pass, 0 fail,
4 unsupported, 45 known-difference, 0 infra-error. The 4 capability
documents and 48 client result documents kept validate against `schemas/`
(0 errors).

| Run | Command | pass | fail | other |
|-----|---------|-----:|-----:|------:|
| smoke vitusdong (server and client) | `scripts/smoke.sh iec103 vitusdong` | 53 | 0 | 0 |
| smoke goiecp5 (client, against vitusdong's server) | `scripts/smoke.sh iec103 goiecp5` | 34 | 0 | 0 |
| smoke ariec103 (client, against vitusdong's server) | `scripts/smoke.sh iec103 ariec103` | 34 | 0 | 0 |
| smoke open103 (client, against vitusdong's server) | `scripts/smoke.sh iec103 open103` | 34 | 0 | 0 |
| cross-stack, unbalanced | `scripts/interop.sh iec103 vitusdong goiecp5 ariec103 open103` | 27 | 0 | 45 known bugs, 4 unsupported |

The cross-stack run pairs every client (vitusdong, goiecp5, ariec103,
open103) with the one server (vitusdong): the 9 cases of
`tests/iec103/cases.tsv` (36 outcome checks and the agreement of the
pairings), then the values (interrogation, identification, command
acknowledgement, its change of state with cause 12 and the command seen by
the next interrogation, measurands) and
2 session checks per pairing. What passed: the link start-up of all four
clients (reset of the communication unit, ArIEC103 adding a reset of the FCB,
Open103 a status of link), `connect` and `monitor` with identical documents
from all four, the clock synchronization of goiecp5, vitusdong and open103
(ASDU 6, cause 8, confirmed with the station's time), a line idle for 3 s and
the interrogation on the next connection (8 session checks), and the
termination of the interrogation with the requested scan number for every
client. The 4 unsupported are the measurands: vitusdong's server is given
none (CAPABILITIES.md). The 45 known differences are the six entries of
`tests/iec103/known-bugs.tsv`; each reproduced in this run. When entries of
both adapters match, the client's is counted (its request is the first
frame), so the command checks of ariec103 and open103 against vitusdong count
as their own bugs (7 each) and those of goiecp5 and vitusdong as vitusdong's
(14):

| Known bug | Cases it explains | Wire evidence (this run) |
|-----------|-------------------|--------------------------|
| vitusdong v0.1.0: interrogation replies with cause 11 and a 12-octet ASDU 1 | `interrogate` (2 cases × 4 clients), values.interrogation (4) | `68 14 14 68 28 01 01 81 0B 01 A0 10 02 FF FF FF FF 24 66 02 02 0B 0B 1A 23 16`: ASDU 1, cause 11, FUN 160 INF 16, DPI 2, `FF FF FF FF`, CP56Time2a; the standard's is DPI, CP32Time2a, SIN with cause 9 |
| vitusdong v0.1.0: command confirmed with ASDU 1 cause 11 / 139, no cause 12 report | 4 command cases × 2 clients (goiecp5, vitusdong), values.command-* (3 × 2) | positive: `... 01 81 0B 01 A0 10 01 FF FF FF FF ...` (cause 11, which its `Cot103` calls remote operation; IEC 103 has 11 local and 12 remote operation); DCO 3: cause 0x8B (11 with the P/N bit); 160/18, an indication the fixture gives no command, is executed; never cause 20 or 21 with the RII, never cause 12 |
| vitusdong v0.1.0: identification without COL and software id | values.identification (4) | `68 10 10 68 28 01 05 81 04 01 FF 50 4F 54 46 41 42 31 30 33 03 16`: ASDU 5, cause 4, FUN 255, INF 80, then `OTFAB103` only; a second one with INF 81, cause 5 |
| ariec103 v1.2.31-3-gec08dd7: clock synchronization with a CP32Time2a | `clock-sync` (ariec103 → vitusdong) | `68 0D 0D 68 53 01 06 81 08 01 FF 00 39 30 23 0F 00 7E 16`: five octets after FUN/INF (ms, minute, hour, 0); Iec103Server needs seven and sends no confirmation |
| ariec103 v1.2.31-3-gec08dd7: general command without the RII | 4 command cases, values.command-* (3) (ariec103 → vitusdong) | `68 09 09 68 53 01 14 81 14 01 A0 10 02 B0 16`: FUN 160, INF 16, DCO 2 and no RII (clause 7.3.2.4); go-iec60870-5's consumer suite saw `14 81 14 01 A0 63 02` and its server, which checks the length, sends no acknowledgement; Iec103Server takes the short ASDU |
| open103 887ff5a5: a command only for the typical INF of the identified function type | 4 command cases, values.command-* (3) (open103 → vitusdong) | nothing sent (error `failed`): vitusdong identifies with FUN 255, for which `CommandTrasmission` allows no INF; against a station of FUN 160 (go-iec60870-5's consumer suite) it sent 16..19 and nothing for 160/23, whose FUN column in table 17 is typical, not mandatory |

### Not shown by these runs

- No server in this run follows IEC 60870-5-103 in its replies, so none of the
  case expectations that involve an interrogation, an identification or a
  command was met by any pairing: they are the contract's, derived from the
  standard, and wait for a conforming reference server (ROADMAP.md).
- Only one server: the clients agree with each other, but the server role of
  a library under test is checked against four clients and its client role
  against one deviating server.
- vitusdong is not independent of ARIEC103 (its test oracle is a copy of the
  ArIEC103 simulator), which matters for the ariec103 → vitusdong pairing.
- Measurands (ASDU 3/9 as class 2 data), broadcast clock synchronization,
  generic services and disturbance data: no server has them in this run.
- The images of the four IEC 103 adapters were built for linux/amd64 under
  emulation and answered an interrogation there (2026-11-11); the self-tests
  ran on linux/arm64 only.

### Other deviations seen on the wire

Not in known-bugs.tsv because no case fails on them against the reference
server, which accepts them; a strict station would not:

- vitusdong's client sends ASDU 6 with FUN 0 (the standard: 255):
  `68 0F 0F 68 53 01 06 81 08 01 00 00 39 30 23 0F 09 0A 1A AC 16`.
- The ArIEC103 slave simulator (not an image; run over TCP for the survey)
  answers a request of the status of link with ACK, ends every interrogation
  with SCN 0, sends measurands as ASDU 9 with one value and a stray octet, and
  identifies with cause 3, FUN 100, INF 2 and 24 characters
  (REFERENCE_STACKS.md).

## IEC 60870-5-104

The five adapters of iec104-interop v0.6.0, rebuilt here under the new
names (entrypoint, readiness path, `standard` in capabilities); protocol
code unchanged. Status in versions.yaml: experimental, because these
images have not been released from this repository yet.

| Run | Command | pass | fail | known bug | unsupported |
|-----|---------|-----:|-----:|----------:|------------:|
| smoke lib60870 | `scripts/smoke.sh iec104 lib60870` | 55 | 0 | 0 | 0 |
| smoke openmuc | `scripts/smoke.sh iec104 openmuc` | 55 | 0 | 0 | 0 |
| smoke wendy512 | `scripts/smoke.sh iec104 wendy512` | 51 | 0 | 0 | 0 |
| smoke cbsigma | `scripts/smoke.sh iec104 cbsigma` | 59 | 0 | 0 | 0 |
| smoke part5 | `scripts/smoke.sh iec104 part5` | 49 | 0 | 0 | 0 |
| cross-stack, 5 × 5 pairings | `scripts/interop.sh iec104 lib60870 openmuc wendy512 cbsigma part5` | 3071 | 0 | 44 | 583 |

The smoke counts differ per adapter because the checks of TLS, security and
redundancy run only for images that declare those features. "unsupported"
are cases a pairing skips because one side does not declare the feature;
they are not passes.

### Known upstream defects

| Stack | Version | Defect | Evidence |
|-------|---------|--------|----------|
| OpenMUC j60870 | 1.8.0 | Cannot decode C_RD_NA_1 and closes the connection when it receives one | reproduced in this run (44 known-difference outcomes, every pairing with openmuc on a read); `tests/iec104/known-bugs.tsv`; https://github.com/gythialy/j60870/pull/75 |

Carried over from iec104-interop, where it was first found; this run
confirms it still reproduces with the same pin.

# Fixtures

Fixtures live in `fixtures/<standard>/<name>/fixture.json`, one schema for
IEC 101 and 104 ([`fixtures/schema/iec60870-5-fixture.schema.json`](../fixtures/schema/iec60870-5-fixture.schema.json)),
one for IEC 103, whose objects have no address but a function type and an
information number ([`fixtures/schema/iec60870-5-103-fixture.schema.json`](../fixtures/schema/iec60870-5-103-fixture.schema.json), [below](#iec-103)),
and one for IEC 102, a meter's integration periods per record address
([`fixtures/schema/iec60870-5-102-fixture.schema.json`](../fixtures/schema/iec60870-5-102-fixture.schema.json), [below](#iec-102)).
An image bakes in the fixtures of its standard at `/fixtures/<name>/fixture.json`.

| Standard | Fixtures | Notes |
|----------|----------|-------|
| IEC 101 | `baseline` | The IEC 104 baseline without its files; every address fits a two-octet information object address (`--ioa-size 2`, the default); `scripts/validate-fixtures.py` enforces 65535 as the limit |
| IEC 102 | `baseline` | An electricity meter (REE profile) with hourly integrated totals and tariff information; [IEC 102](#iec-102) |
| IEC 103 | `baseline` | A protection device of function type 160; [IEC 103](#iec-103) |
| IEC 104 | `baseline`, `extended` | As in iec104-interop 0.6.0 |

The station behaviour below is the same for IEC 101, except that IEC 101 has
no CP56Time2a command types and no C_TS_TA_1 (the adapters are written to refuse them with cause 44; no self-test sends one), tests
with C_TS_NA_1, and its broadcast common address is 255 with a one-octet
common address field (65535 with two). File transfer, uploads, the directory
and redundancy groups are not part of the IEC 101 images.

A fixture describes a simulated IEC 60870-5-104 controlled station: its
points, their initial values and the commands that act on them. It contains
no assertions and no implementation detail. Every server adapter serves the
same fixture the same way; this document says what "the same way" is.

- Format: JSON, [schema](../fixtures/schema/iec60870-5-fixture.schema.json), `schemaVersion` `"1.0"`
- Location: `fixtures/<name>/fixture.json`, baked into every image under `/fixtures`
- Validation: `make validate` (schema plus the cross-field rules below); adapters apply the same rules at start and exit 4 on a violation

## Format

```json
{
  "schemaVersion": "1.0",
  "name": "baseline",
  "station": { "commonAddress": 1 },
  "points": [
    { "ioa": 100, "type": "M_SP_NA_1", "value": true },
    { "ioa": 110, "type": "M_SP_NA_1", "value": false, "quality": ["NT", "IV"] },
    { "ioa": 300, "type": "M_IT_NA_1", "value": 123456, "sequence": 1 }
  ],
  "commands": [
    { "ioa": 500, "type": "C_SC_NA_1", "target": 100, "reportCause": 11 },
    { "ioa": 510, "type": "C_SC_NA_1", "target": 100, "selectRequired": true }
  ],
  "files": [
    { "ioa": 30000, "name": 1, "size": 1000, "sectionSize": 1000 }
  ],
  "uploads": [
    { "ioa": 31000, "name": 1, "maxSize": 8192 }
  ]
}
```

### Points

| Field | Meaning |
|-------|---------|
| `ioa` | Information object address, 1..16777215, unique |
| `type` | The type without time tag: see the table |
| `value` | Initial value |
| `quality` | Optional array of flags; absent means good |
| `transient` | `M_ST_NA_1` only |
| `sequence` | `M_IT_NA_1` only, 0..31 |
| `groups` | Optional array of interrogation groups, 1..16, unique. Not on `M_IT_NA_1`, which an interrogation does not report |

| `type` | `value` | `quality` flags |
|--------|---------|-----------------|
| `M_SP_NA_1` | boolean | `BL` `SB` `NT` `IV` |
| `M_DP_NA_1` | 0..3 | `BL` `SB` `NT` `IV` |
| `M_ST_NA_1` | -64..63 | `OV` `BL` `SB` `NT` `IV` |
| `M_BO_NA_1` | 0..4294967295 | `OV` `BL` `SB` `NT` `IV`, with the feature `pointQualityOnBitstring` |
| `M_ME_NA_1` | raw NVA, -32768..32767 | `OV` `BL` `SB` `NT` `IV` |
| `M_ME_NB_1` | -32768..32767 | `OV` `BL` `SB` `NT` `IV` |
| `M_ME_NC_1` | number | `OV` `BL` `SB` `NT` `IV` |
| `M_IT_NA_1` | 32-bit signed | `CY` `CA` `IV` |

Use values a single-precision float represents exactly for `M_ME_NC_1`
(230.5, not 230.1), so that every stack prints the same number.

### Commands

| Field | Meaning |
|-------|---------|
| `ioa` | Address of the command object; unique, and not the address of a point |
| `type` | The type without time tag. The CP56Time2a variant is accepted for the same object |
| `target` | Address of the point the command acts on; its type must match |
| `reportCause` | Cause of transmission of the report after execution: 11 (return information, remote command; the default) or 3 (spontaneous) |
| `selectRequired` | When true, an execute is refused unless the object is selected. Default false |

| Command | Target | Effect of an execute |
|---------|--------|----------------------|
| `C_SC_NA_1` | `M_SP_NA_1` | value := command state |
| `C_DC_NA_1` | `M_DP_NA_1` | value := command state (1 or 2) |
| `C_RC_NA_1` | `M_ST_NA_1` | value := value - 1 (lower) or + 1 (higher) |
| `C_SE_NA_1` | `M_ME_NA_1` | value := raw NVA |
| `C_SE_NB_1` | `M_ME_NB_1` | value := scaled value |
| `C_SE_NC_1` | `M_ME_NC_1` | value := float value |

Quality flags of the target are not changed by a command.

### Files

Optional. Only a server with the feature `fileServer` serves them; every
adapter validates them.

| Field | Meaning |
|-------|---------|
| `ioa` | Information object address of the file, 1..16777215, unique among points, commands and files |
| `name` | Name of file (NOF), 1..255 |
| `size` | Length of the file in octets, 1..65536 |
| `sectionSize` | Length of every section but the last, 1..`size` |

The content is not stored: octet `i` of the file (counting from 0) is
`(i + ioa) mod 251`. A consumer computes the expected content, or its
SHA-256, from the fixture alone; 251 is prime, so no section or segment
boundary lines up with the pattern.

### Uploads

Optional. Where a controlling station may send a file. Only a server with the
feature `fileUploadServer` takes one; every adapter validates them.

| Field | Meaning |
|-------|---------|
| `ioa` | Information object address of the file, 1..16777215, unique among points, commands, files and uploads |
| `name` | Name of file (NOF), 1..255 |
| `maxSize` | The longest file taken, in octets, 1..65536 |

## Station behaviour

`CA` is `station.commonAddress`. A request is *for the station* when its
common address is `CA` or the broadcast address 65535. Answers always carry
`CA`.

### Refusals common to all requests

| Condition | Answer |
|-----------|--------|
| Common address is neither `CA` nor 65535 | Mirror with cause 46 and P/N = 1 |
| Type identification the station does not serve | Mirror with cause 44 and P/N = 1 |
| Process command with a cause other than 6 or 8 | Mirror with cause 45 and P/N = 1 |
| `server --secure --roles ...`: a request the roles do not permit | Mirror with cause 17 (not authorized) and P/N = 1 |

The role check comes right after the type check, before the common address:
which role permits what is IEC 62351-8's and the library's. Such a refusal,
like the others in this table, produces no server event.

What a station answers to a *system* command with an unexpected cause is left
to the adapter's library and is not part of the fixture behaviour.

### Interrogation (C_IC_NA_1)

| Request | Answer |
|---------|--------|
| QOI 20 | Activation confirmation; every point that is not an integrated total, with cause 20; activation termination |
| QOI 21..36, group `QOI - 20` has a point in `groups` | Activation confirmation; the points of that group, with cause `QOI`; activation termination. Only with the feature `groupInterrogation` |
| Any other QOI | Negative activation confirmation |

An adapter without `groupInterrogation` refuses a fixture with `groups` (exit 4).
A fixture without `groups` has no group to serve, so every adapter refuses
QOI 21..36 on it; the baseline fixture is such a fixture.

Points are sent without time tag, in ascending address order. Consecutive
points of the same type share an ASDU (SQ = 0); a change of type starts a new
one. The baseline therefore yields nine data ASDUs, two of them with two
objects.

### Counter interrogation (C_CI_NA_1)

| Request | Answer |
|---------|--------|
| QCC 5 (general request, read) | Activation confirmation; every integrated total with cause 37; activation termination |
| Any other QCC | Negative activation confirmation |

### Read (C_RD_NA_1, cause 5)

| Request | Answer |
|---------|--------|
| Address of a point (integrated totals included) | The point with cause 5, without time tag |
| Any other address | Mirror with cause 47 and P/N = 1 |

### Clock synchronization (C_CS_NA_1) and test command (C_TS_TA_1)

Positive activation confirmation. The station does not change its clock: time
tags it generates are the current UTC time.

### Process commands

For a command of type `T` (with or without time tag) to address `A`, cause 6
(activation) or 8 (deactivation):

| Condition, first match wins | Answer | Effect |
|-----------------------------|--------|--------|
| No command object at `A`, or its type is not `T` | Mirror with cause 47 and P/N = 1 | none |
| Cause 8 | Deactivation confirmation (cause 9) | selection cleared |
| Value not permitted (`C_DC`/`C_RC` state 0 or 3) | Negative activation confirmation | none |
| S/E = select | Activation confirmation | object selected |
| S/E = execute, `selectRequired`, object not selected | Negative activation confirmation | none |
| S/E = execute, step command at the limit (-64 or 63) | Negative activation confirmation | selection cleared |
| S/E = execute | Activation confirmation, **report**, activation termination, in this order | target updated, selection cleared |

The **report** is the target point with its CP56Time2a type (`M_SP_TB_1`,
...), the current time, cause `reportCause` and originator 0. It is sent on
the connection that issued the command, not to other connections.

A selection has no timeout and is shared by all connections. The time tag of
a time-tagged command is not evaluated.

### File transfer (F_SC_NA_1, cause 13)

Only with the feature `fileServer`. The procedure is the one of
[`file-get`](CONTAINER_CONTRACT.md#file-get); every ASDU has cause 13.

| Request | Answer |
|---------|--------|
| Select (SCQ 1) of a file address with its name | F_FR_NA_1 with the length of the file |
| Select of an unknown address, or of a file with another name | Mirror with cause 47 and P/N = 1 |
| Request file (SCQ 2) | F_SR_NA_1 for section 1 |
| Request section (SCQ 6) | The section in F_SG_NA_1 segments, then F_LS_NA_1 with LSQ 3 and the checksum of the section |
| Section acknowledged (AFQ 3) | F_SR_NA_1 for the next section, or after the last one F_LS_NA_1 with LSQ 1 and the checksum of the file |
| File acknowledged (AFQ 1) | Nothing; the transfer is complete |
| Deactivation (SCQ 3 or 7), of any file address and name | Nothing; a transfer of that file is abandoned |

A file of `size` octets has `ceil(size / sectionSize)` sections, numbered
from 1. The NOS of the last-section F_LS_NA_1 (LSQ 1) is left to the
library: lib60870 sends one more than the number of sections,
cB-Sigma/iec60870 the number of the last section. Clients accept either, and
the self-tests do not compare that field. A segment carries at most 236 octets, so that the ASDU fits an APDU
with three-octet addresses. A checksum is the sum of the octets modulo 256.

A file is sent on the connection that selected it.

### File upload (F_FR_NA_1, F_SR_NA_1, F_SG_NA_1, F_LS_NA_1, cause 13)

Only with the feature `fileUploadServer`. The procedure is the one of
[`file-put`](CONTAINER_CONTRACT.md#file-put); every ASDU has cause 13.

| Request | Answer |
|---------|--------|
| File ready with the address and name of an entry of `uploads`, and a length of 1..`maxSize` | F_SC_NA_1 with SCQ 2 |
| Any other file ready | Mirror with cause 47 and P/N = 1 |
| Section ready of the file being received | F_SC_NA_1 with SCQ 6 and the same NOS |
| F_SG_NA_1 of the section called | Nothing; the octets are kept |
| F_LS_NA_1 with LSQ 3 | F_AF_NA_1 with the NOS and AFQ 3, or AFQ 4 when the checksum or the length of the section does not match |
| F_LS_NA_1 with LSQ 1 | F_AF_NA_1 with AFQ 1, or AFQ 2 when the checksum or the length of the file does not match; the event `file-received` |

The NOS of the F_LS_NA_1 with LSQ 1 is left to the client, and the F_AF_NA_1
repeats it: go-iec104 sends one more than the number of sections,
cB-Sigma/iec60870 the number of the last section, as with a download.

A stored file stays for the life of the process, and a second upload to the same
address replaces it. With the feature `fileServer` it can be fetched like a file
of `files`, with its name, in sections of a size the station chooses. It is in
the directory.

### Directory (F_SC_NA_1, cause 5)

Only with the feature `fileDirectoryServer`.

| Request | Answer |
|---------|--------|
| SCQ 0 | F_DR_TA_1 with cause 5: an entry for every file of `files` and every stored upload, in ascending address order, the last one with the LFD bit (0x20) in SOF |

An entry has the address of the file, NOF, LOF, SOF (status 0, the LFD bit where
it applies) and a CP56Time2a time of the station's choosing, which a test must
not compare. Whether entries with consecutive addresses travel as a sequence
(SQ = 1) or as separate objects, and how many one ASDU holds, is left to the
library. Files with consecutive addresses arrive in one ASDU; the agreement check
of the self-tests does not compare SQ. What a station without any file answers is
left to the library as well.

### Redundancy group

Only with `server --redundancy-group`, and so with the feature
`redundancyServer`. All connections form one redundancy group in the sense of
IEC 60870-5-104 clause 10:

- At most one connection has data transfer started. A connection in STOPDT gets
  no ASDU.
- A STARTDT act on another connection is confirmed there, and from then on
  nothing is sent on the connection that was started. Whether that one is also
  closed is left to the stack: the lib60870, openmuc and cbsigma stations close
  it, the otfabric station keeps it open. A test must not rely on either.
  A connection already in STOPDT stays up either way.
- **Event queue.** When a connection in STARTDT gets a STOPDT act, the station
  queues one event: the point of type `M_SP_NA_1` with the lowest address, with
  its current value and quality, without time tag, with cause 3 and originator 0.
  It waits for the next connection to start, and goes out after that connection's
  STARTDT con. A takeover queues nothing, and neither does a connection that
  ends. This is the only thing a station sends unasked, and only in this mode.

Answers still go to the connection that asked; the group changes nothing else.

Without the flag there is no group and no queue: every connection is on its own.

### What a station never does

- Send anything unsolicited, apart from the event of a redundancy group: no
  cyclic or background data, no end of initialization. Every other ASDU is the
  answer to a request, which keeps tests free of timing assumptions.
- Change state without a command. Restart the container to return to the
  fixture.

## The baseline fixture

Station 1.

| IOA | Type | Value | Quality |
|----:|------|-------|---------|
| 100 | `M_SP_NA_1` | true | |
| 101 | `M_DP_NA_1` | 2 (on) | |
| 102 | `M_ST_NA_1` | 5 | |
| 103 | `M_BO_NA_1` | 2863311530 (0xAAAAAAAA) | |
| 110 | `M_SP_NA_1` | false | NT, IV |
| 111 | `M_SP_NA_1` | true | BL, SB |
| 200 | `M_ME_NA_1` | 16384 (0.5) | |
| 201 | `M_ME_NB_1` | 1234 | |
| 202 | `M_ME_NC_1` | 230.5 | |
| 210 | `M_ME_NB_1` | -32768 | OV |
| 211 | `M_ME_NB_1` | -1 | |
| 300 | `M_IT_NA_1` | 123456, sequence 1 | |

| IOA | Command | Target | Report cause | Select required |
|----:|---------|-------:|-------------:|-----------------|
| 500 | `C_SC_NA_1` | 100 | 11 | no |
| 501 | `C_DC_NA_1` | 101 | 3 | no |
| 502 | `C_SE_NC_1` | 202 | 11 | no |
| 503 | `C_SE_NA_1` | 200 | 11 | no |
| 504 | `C_SE_NB_1` | 201 | 11 | no |
| 505 | `C_RC_NA_1` | 102 | 11 | no |
| 510 | `C_SC_NA_1` | 100 | 11 | yes |

| IOA | File name | Size | Section size | Sections | SHA-256 |
|----:|----------:|-----:|-------------:|---------:|---------|
| 30000 | 1 | 1000 | 1000 | 1 | `85e6f8dae1836546d45697e6d939234bc7fe5f31862f863af71e52bc223adbde` |
| 30001 | 1 | 5000 | 2048 | 3 | `149c48d73a2f7159a1ad298d2ca582fa1773635c6ed071a8b25fc46e915f01c8` |

## The extended fixture

Station 1. What the baseline does not have and old images do not know: read it
from the image with `print-fixture extended`, after checking the features it
needs (`pointQualityOnBitstring`, `groupInterrogation`; `files` and `uploads` are
ignored by a station that does not serve them). Its station serves the requests of this document like the baseline's.

| IOA | Type | Value | Quality | Groups |
|----:|------|-------|---------|--------|
| 100 | `M_SP_NA_1` | true | | 1 |
| 101 | `M_DP_NA_1` | 2 (on) | | 1 |
| 103 | `M_BO_NA_1` | 2863311530 (0xAAAAAAAA) | | 2 |
| 104 | `M_BO_NA_1` | 1431655765 (0x55555555) | OV, IV | 2 |
| 105 | `M_BO_NA_1` | 4042322160 (0xF0F0F0F0) | BL, SB, NT | 2, 3 |
| 200 | `M_ME_NB_1` | 1234 | | 3, 16 |
| 202 | `M_ME_NC_1` | 230.5 | | |
| 300 | `M_IT_NA_1` | 123456, sequence 1 | | |

| IOA | Command | Target |
|----:|---------|-------:|
| 500 | `C_SC_NA_1` | 100 |

| IOA | File name | Size | Section size | Sections | SHA-256 |
|----:|----------:|-----:|-------------:|---------:|---------|
| 30000 | 1 | 1000 | 1000 | 1 | `85e6f8dae1836546d45697e6d939234bc7fe5f31862f863af71e52bc223adbde` |
| 30001 | 2 | 5000 | 2048 | 3 | `149c48d73a2f7159a1ad298d2ca582fa1773635c6ed071a8b25fc46e915f01c8` |

| IOA | Upload name | Largest file |
|----:|------------:|-------------:|
| 31000 | 1 | 8192 |

## IEC 103

An IEC 103 fixture describes a simulated protection device (the secondary
station): its identification, its indications, its measurands and the general
commands it takes. `scripts/validate-fixtures.py` checks it against the IEC 103
schema and the rules below; an adapter applies the same rules at start and
exits 4 on a violation.

```json
{
  "schemaVersion": "1.0",
  "name": "baseline",
  "station": {
    "commonAddress": 1,
    "identification": { "fun": 160, "col": 2, "ascii": "OTFAB103", "software": "01020304" }
  },
  "indications": [ { "fun": 160, "inf": 16, "dpi": 2 }, { "fun": 160, "inf": 68, "dpi": 1, "gi": false } ],
  "measurands": [ { "fun": 160, "inf": 148, "type": 9, "values": [ { "value": 100 }, { "value": 4095, "ov": true } ] } ],
  "commands": [ { "fun": 160, "inf": 16 } ]
}
```

| Field | Meaning |
|-------|---------|
| `station.commonAddress` | 0..254; normally the link address |
| `station.identification` | ASDU 5: `fun` the device's function type, `col` the compatibility level, `ascii` exactly eight printable characters, `software` four octets in hex |
| `indications[]` | `fun` 0..254 and `inf`, unique together; `dpi` 0..3 (1 off, 2 on); `gi` false keeps it out of the general interrogation (default true) |
| `measurands[]` | `fun`, `inf` (unique with the indications), `type` 3 (measurands I, at most four values) or 9 (measurands II, at most nine), `values[]`: `value` -4096..4095 (the 13-bit value), `ov`, `er` (default false) |
| `commands[]` | `fun`, `inf`: a general command the device takes, acting on the indication with the same function type and information number, which must exist. For a function type of a compatible range, INF 19 and 23..26 take ON only (table 17, COM) |
| `generic[]` | Contract 2.3. A generic entry (clause 7.4.8): `gin` `{group, entry}` (1..255 each, unique), `description` (the text of KOD 10, printable ASCII), `type` the GDD data type (1 ASCII, 3 unsigned integer, 4 integer, 7 R32.23), `size` its DATASIZE (the number of characters; 1, 2 or 4; 4), `value` (the text, an integer that fits, or a number exact in single precision), `writable` (default false) |
| `disturbances[]` | Contract 2.3. A recorded disturbance (clause 7.4.7), at most eight: `fan` (unique), `sof` `{tp, tm, test, otev}` (default false), `time` the fault (ASDU 23, `2026-01-02T03:04:05.678Z`), `nof`, `interval` (INT, µs), `start` the time of day of the first value (ASDU 26, `HH:MM:SS.mmm`), `tags[]` `{pos, fun, inf, dpi}` in ascending position, the initial states at position 0 first, `channels[]` `{acc, rpv, rsv, rfa, values}` in ascending ACC, every channel with the same number of values (NOE), each -32768..32767 |

### Station behaviour (IEC 103)

The station is unbalanced: it answers the primary station's requests and
signals class 1 data with ACD. Its process state (the DPI of the indications)
lives as long as the process; its link state and class 1 queue start afresh
with every connection.

| Request | Answer |
|---------|--------|
| Reset of the communication unit (function 0) | ACK; queues ASDU 5, cause 4, the identification's function type, INF 3, COL, the eight characters and the four octets |
| Reset of the frame count bit (function 7) | ACK; queues ASDU 5 with cause 3 and INF 2 |
| Request of the status of the link (function 9) | Status of link (function 11) |
| Class 1 (function 10) | The next queued ASDU (function 8, ACD while more are queued), or no data (function 9) |
| Class 2 (function 11) | One measurand of the fixture after the other, cyclically: ASDU 3 or 9, cause 2, the values in order; no data (function 9) when the fixture has none. ACD when class 1 data is queued |
| ASDU 7 (general interrogation, FUN 255, INF 0) | ACK; queues ASDU 1, cause 9, SIN the scan number, for every indication with `gi` not false, in the fixture's order, then ASDU 8, cause 10, the same scan number |
| ASDU 6 (clock synchronization), addressed | ACK; sets the station's clock; queues ASDU 6, cause 8, FUN 255, INF 0 with the new time |
| ASDU 6 to link address 255 (SEND/NO REPLY) | Sets the clock; no answer |
| ASDU 20 (general command) | ACK. A command of the fixture with DCO 1 (OFF) or 2 (ON) that the COM column of table 17 allows (for a function type of a compatible range, INF 19 and 23..26 take ON only; clause 7.2.6.4): the indication's DPI becomes the DCO, and the station queues ASDU 1, cause 20 (positive acknowledgement), the command's FUN and INF, SIN the RII; then, when the DPI changed, ASDU 1, cause 12 (remote operation), the FUN and INF, the new DPI, SIN 0 (clause 7.4.4, figure 7). Anything else (a FUN/INF that is no command of the fixture, DCO 0 or 3, OFF where table 17 allows only ON, a command before the previous one was acknowledged): nothing changes, ASDU 1, cause 21 (negative acknowledgement), the command's FUN and INF, SIN the RII, and no cause 12 report |

Contract 2.3 adds the generic services and the transmission of disturbance
data:

| Request | Answer |
|---------|--------|
| ASDU 21, cause 42, FUN 254, INF 244 (read a single entry), GIN and KOD | ACK; ASDU 10, cause 42, FUN 254, INF 244, the RII, CONT 0: KOD 1 the entry's value with its data type and size (NUMBER 1), KOD 10 its description (type 1). An entry the device lacks or another KOD: cause 43 with the GIN, the KOD and a generic reply code (type 22): 1 (GIN not defined), 2 (data do not exist) (clause 7.4.8.6.2) |
| ASDU 10, cause 40, FUN 254, INF 250 (write with execution) | ACK. When every data set is KOD 1 to a writable entry with its data type and size and NUMBER 1, the values are set and ASDU 10, cause 40, INF 250, the RII and no data sets follows; otherwise nothing is set and cause 41 follows with the GIN, KOD and a reply code of each failed set: 1 (GIN not defined), 8 (read only), 5 (another type, size or KOD) (clause 7.4.8.6.3, figure 17). The values live as long as the process |
| ASDU 24, cause 31, TOO 24 | ACK; ASDU 23, cause 31, the device's function type, VSQ with SQ 0 and the number of records, FAN, SOF and the time of each (clause 7.3.1.11) |
| ASDU 7 (general interrogation) | Also: after the termination, ASDU 23 as above (clause 7.4.7: the list is sent after the start of a general interrogation, with cause 31) |
| ASDU 24 TOO 1, 2, 16, 8 and ASDU 25 TOO 68, 66, 64, 65 | The procedure of clause 7.4.7 (figure 10) with the fixture's record: ASDU 26 (the not used octet 0, TOV 1, FAN, NOF, NOC, NOE, INT, `start`), ASDU 28, ASDUs 29 (the tags of one position each, at most 25), ASDU 31 TOO 38, each channel offered by ASDU 27 after the acknowledgement of the tags or of the previous channel, its values in ASDUs 30 of at most 25 (NFE the number of the first value of the ASDU, the first value of the channel being number 0), ASDU 31 TOO 35, and after the last channel ASDU 31 TOO 32; after the final acknowledgement the list again. The record stays. An order that does not fit the transfer in progress: ASDU 31 TOO 34. All with the device's function type and cause 31, as class 1 data |

The DPI of an acknowledgement (cause 20 or 21) is not significant (the notes
to clauses 7.3.1.1 and 7.3.1.2), and SIN only with causes 9, 20 and 21
(clause 7.2.6.23): checks compare neither the DPI of an acknowledgement nor
the SIN of a cause 12 report. The order is that of clause 7.4.4 and figure 7:
the acknowledgement ends the command procedure, the change of state follows,
so a client that collects after the acknowledgement (`--collect-ms`) receives
cause 20, then cause 12.

Time tags (CP32Time2a of ASDU 1) are the station's clock. Nothing else is
sent: no spontaneous ASDUs (cause 1).

### The IEC 103 baseline fixture

`fixtures/iec103/baseline`: an overcurrent protection device (function type
160) identified as `OTFAB103`, COL 2, software `01020304`.

| Indication | DPI | In the interrogation | Command (table 17, COM) |
|------------|-----|----------------------|-------------------------|
| 160/16 auto-recloser active | 2 (on) | yes | yes (ON/OFF) |
| 160/17 teleprotection active | 1 (off) | yes | yes (ON/OFF) |
| 160/18 protection active | 2 | yes | no: refused with cause 21 (a valid pair of table 17 the device does not take) |
| 160/19 LED reset | 1 | yes | yes (ON only) |
| 160/23 characteristic 1 | 2 | yes | no |
| 160/24 characteristic 2 | 1 | yes | no |
| 160/27 auxiliary input 1 | 1 | yes | no |
| 160/36 trip circuit supervision | 1 | yes | no |
| 160/68 general trip | 1 | no | no |

The commands are pairs a client that follows table 17 sends to an
overcurrent protection (FUN 160): INF 16..19 list FUN 160 as typical; INF
23..26 list only distance protection (FUN 128), and INF 32..239 are not used
in the control direction (clause 7.2.5.2).

Measurands, class 2: 160/144 (ASDU 3, one value 2048), 160/145 (ASDU 3, 1024
and -512 with OV), 160/148 (ASDU 9, nine values: 100, 200, 300, 3000, 3001,
2999, -4096, 4095 with OV, 0 with ER).

Generic data (group 1): 1.1 `RATED I` unsigned 2 octets 1000, read only;
1.2 `PICKUP I>` R32.23 1.5, writable; 1.3 `DELAY t>` unsigned 2 octets 300,
writable; 1.4 `FEEDER` ASCII 8 characters `FEEDER 1`, read only; 1.5
`OFFSET` integer 2 octets -20, read only.

Recorded disturbance FAN 7: TP (with trip), fault at
2026-10-09T15:30:00.012Z, NOF 3, INT 1000 µs, first value at 15:30:00.000;
tags 160/84 (general start) and 160/90 (trip I>) off at position 0, 160/84
on at 12, 160/90 on at 20; channels ACC 1 (IL1) and 2 (IL2), RPV 600, RSV
1, RFA 1, 40 values each (a sine of 20 values per period whose amplitude
grows from 6000 to 21000 at value 12), so every channel takes two ASDUs 30.

## IEC 102

An IEC 102 fixture describes one integrated total station (an electricity
meter with the REE profile): `fixtures/iec102/<name>/fixture.json`, schema
`fixtures/schema/iec60870-5-102-fixture.schema.json`. All times are the
station's local time in standard time (SU 0): time a `YYYY-MM-DDTHH:MM`, time
b `YYYY-MM-DDTHH:MM:SS.mmm` ([contract](CONTAINER_CONTRACT.md#iec-102-link)).

| Field | Meaning |
|-------|---------|
| `station.commonAddress` | The measuring point address (common address of ASDU) |
| `station.accessKey` | REE: the access key C_AC_NA_2 must carry (four octets) |
| `station.clock` | The station's clock (time b). It **stands still** at this time, so that reads are deterministic, until a C_CS_TA_2 sets it; the new time then lives as long as the station's process, across connections |
| `station.product` | P_MP_NA_2: `standard` (first octet), `manufacturer` (an octet), `product` (four octets) |
| `totals[]` | Integrated totals of one record address (`record`) and kind (`kind`: `absolute` served as M_IT_TG_2 to C_CI_NT_2, `incremental` as M_IT_TK_2 to C_CI_NU_2): `periods[]`, oldest first, each with its `end` (time a, the common time tag) and `totals[]` (`address` 1..255, `value` unsigned 32 bits, `quality` the octet as sent) |
| `tariffs[]` | REE: the tariff information of a contract register (`register` 134..139): `current[]` (served to C_TA_VC_2) and `stored[]` (to C_TA_VM_2), records with the fields of the [ASDU document](CONTAINER_CONTRACT.md#iec-102-asdu-documents) of M_TA_VC_2 |

`make validate` also checks: one list per record address and kind, period
ends strictly ascending, total addresses unique within a period, tariff
registers unique, no record that ends before it starts.

### Station behaviour (IEC 102)

The station is unbalanced, with a link address and a common address of two
octets unless the link flags say otherwise. Its link state and class 2 queue
start afresh with every connection; its clock lives as long as the process.
It never sets ACD: every answer is class 2 data. It answers every request,
in every state of the session: it does not make the reads depend on a login.

| Request | Answer |
|---------|--------|
| Request status of link (function 9) | Status of link (function 11) |
| Reset of remote link (function 0) | ACK (function 0); the queue is emptied |
| User data, confirm (function 3) | ACK (function 0); queues the answers below |
| Class 2 (function 11) | The next queued ASDU (function 8), or no data (function 9) |
| A repeated frame (FCV set, the FCB of the previous frame) | The previous answer again |
| C_AC_NA_2 (183) | Mirror with cause 7; with the P/N bit when the key is not `station.accessKey` |
| C_FS_NA_2 (187) | Mirror with cause 7 |
| C_RD_NA_2 (100) | P_MP_NA_2 (71), cause 5, one object: `station.product` |
| C_TI_NA_2 (103) | M_TI_TA_2 (72), cause 5, one object: the clock as time b (SU and IV as last set, 0 for the fixture's clock) |
| C_CS_TA_2 (181) | Sets the clock to the time b received (with its SU bit); mirror with cause 7 |
| C_CI_NT_2 (122), C_CI_NU_2 (123) | The periods of the record address and kind whose `end` lies within the two times a (inclusive, comparing date and time to the minute, ignoring the flags); of each, the totals whose address lies within the first and last address. Mirror with cause 7, then one ASDU per period that has a total left (M_IT_TG_2 or M_IT_TK_2, cause 5, the record address, the totals in the fixture's order, then the period's `end` as time a), then the mirror with cause 10 |
| C_TA_VC_2 (133) | Mirror with cause 7, one M_TA_VC_2 (135) per record of `current[]` (cause 5, record address the register, one object each, the reserved totals 0), mirror with cause 10 |
| C_TA_VM_2 (134) | As C_TA_VC_2 with the records of `stored[]` whose `periodEnd` lies within the two times a, as M_TA_VM_2 (136) |

Refusals: the request mirrored with the cause below and nothing else.

| Situation | Cause |
|-----------|-------|
| The common address is not the station's | 16 |
| C_CI_*: no totals on the record address | 16 |
| C_CI_*: totals on the record address, but none of the kind | 14 |
| C_CI_*: no period in the time range | 18 |
| C_CI_*: periods, but no total in the address range | 17 |
| C_TA_*: a register without tariff information | 16 |
| C_TA_*: no record (stored values in the range) | 18 |
| Any other type identification | 14 |

The causes are those of the Beckhoff TS650x manual (section 9.1.11: 14 type
not available, 16 unknown record address, 17 information object not
available, 18 integration period not available); which cause a station uses
in which situation is this contract's choice: no source we hold fixes it.

### The IEC 102 baseline fixture

`fixtures/iec102/baseline`: measuring point address 1, access key 7, the clock
at 2026-01-15T10:20:30.500, product `{standard: 42, manufacturer: 33,
product: 20261115}`.

| Record | Kind | Periods (end) | Totals |
|-------:|------|---------------|--------|
| 11 | absolute (M_IT_TG_2) | 2026-01-01T01:00, 02:00, 03:00 | addresses 1..6 (REE: active import, export, reactive quadrants 1..4); quality 16 (bit 4) on address 1 of 02:00, 128 (IV) on address 6 of 03:00 |
| 11 | incremental (M_IT_TK_2) | the same | the differences; 128 (IV) on address 3 of 02:00, 2 (bit 1) on address 1 of 03:00 |

Tariff information of register 134 (contract I): two records of current
values (addresses 1 and 2, period from 2026-01-01T00:00 to 2026-01-15T10:00)
and two of stored values (address 1, the billing periods of November and
December 2025; the active quality of November is 128).

## Adding a fixture

1. Create `fixtures/<name>/fixture.json`; `name` must equal the directory.
2. `make validate`.
3. Use only types and features every published adapter declares
   ([CAPABILITIES.md](CAPABILITIES.md)), or say in this document which features
   the fixture needs; a fixture must not need adapter-specific behaviour.
4. Rebuild the images (`make images`): fixtures are baked in.
5. If the fixture is meant for the self-tests, add cases for it; a fixture
   nothing exercises is not verified.

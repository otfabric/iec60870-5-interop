# Container contract

Version: **2.1** (`contract_version` in [versions.yaml](../versions.yaml))

Every adapter image exposes the same commands, flags, JSON documents and exit
codes, for each standard of the IEC 60870-5 family it implements. Consumers
depend on this document, not on any adapter's source. A change that breaks it
bumps the major version; an addition bumps the minor one.

Documents carry `schemaVersion` `"1.0"`. Version 2.0 changed the image names,
the entrypoint, the readiness path and the transport of the serial standards;
the shape of the documents is the one of contract 1.3 plus additive fields, so
a reader of 1.3 documents reads 2.0 documents unchanged.

| Version | Change |
|---------|--------|
| 2.1 | [IEC 103](#iec-103): unbalanced only, a one-octet link address, no information object addresses; the client operations `connect`, `interrogate` (`--scn`), `clock-sync` (`--broadcast`), `command` (`--fun`, `--inf`, `--dco`, `--rii`), `monitor`; ASDU documents of IEC 103 (`fun`, `inf`, `dpi`, `sin`, `time32`, `values`, ...), `asduTypes` and the IEC 103 features in capability documents; confirmations with causes 8, 20 and 21; server events of IEC 103. An image may be a client only (`roles.server` false): `server` is then a usage error |
| 2.0 | One repository for the IEC 60870-5 family (`iec60870-5-interop`, from `iec104-interop` 0.6.0, contract 1.3). Image `ghcr.io/otfabric/iec60870-5-interop-<standard>-<stack>`; entrypoint `/usr/local/bin/iec60870-5-interop`; readiness file `/run/iec60870-5-interop/ready`. `standard` in capability documents (and `transport`, `linkModes` for the serial standards). [IEC 101, 102 and 103](#iec-101-102-and-103): the FT 1.2 byte stream over one TCP connection, port 2401, the [link flags](#link-flags), the error `link-init-timeout`, results without `startdtConfirmed`; C_TS_NA_1 in ASDU documents (`fbp`). IEC 104 is contract 1.3 under the new names |
| 1.3 | (iec104-interop) Interrogation groups, `pointQualityOnBitstring`, the fixture `extended`; the transport security flags of IEC 62351-3; `file-put`, `file-dir`, uploads, F_DR_TA_1; redundancy groups |
| 1.2 | (iec104-interop) The security flags of the A-Profile of IEC 62351-5 |
| 1.1 | (iec104-interop) `file-get`, file transfer ASDU documents, `files` in fixtures, `dataTransferEvents` |
| 1.0 | (iec104-interop) First release |

Not every adapter has every operation or event. What an image lacks it says
in [`print-capabilities`](#print-capabilities), and an operation it does not
list in `clientOperations` is a usage error (exit 2).

What a *station* does with a request is defined by its fixture: see
[FIXTURES.md](FIXTURES.md).

## Contents

- [Standards and images](#standards-and-images)
- [IEC 101, 102 and 103](#iec-101-102-and-103)
- [IEC 103](#iec-103)
- [Image layout](#image-layout)
- [Commands](#commands)
- [Security flags](#security-flags)
- [Transport security flags](#transport-security-flags)
- [`server`](#server)
- [`client`](#client)
- [ASDU documents](#asdu-documents)
- [`print-capabilities`](#print-capabilities)
- [`print-fixture`](#print-fixture)
- [Exit codes](#exit-codes)
- [Output rules](#output-rules)

## Standards and images

| Standard | `<standard>` | `standard` in documents | Transport | Port |
|----------|--------------|-------------------------|-----------|------|
| IEC 60870-5-101 | `iec101` | `iec60870-5-101` | FT 1.2 over one TCP connection | 2401 |
| IEC 60870-5-102 | `iec102` | `iec60870-5-102` | FT 1.2 over one TCP connection | 2401 |
| IEC 60870-5-103 | `iec103` | `iec60870-5-103` | FT 1.2 over one TCP connection | 2401 |
| IEC 60870-5-104 | `iec104` | `iec60870-5-104` | TCP with the APCI of IEC 104 | 2404 |

An image implements one standard with one stack:
`ghcr.io/otfabric/iec60870-5-interop-<standard>-<stack>`, for example
`iec60870-5-interop-iec101-lib60870` and `iec60870-5-interop-iec104-lib60870`.
Images of this version: IEC 101 `lib60870`, `cbsigma`; IEC 103 `vitusdong`
(server and client), `goiecp5`, `ariec103`, `open103` (clients); IEC 104
`lib60870`, `openmuc`, `wendy512`, `cbsigma`, `part5`. There are no IEC 102
images yet ([ROADMAP.md](ROADMAP.md)); its row defines the names only.

Everything after the sections on IEC 101, 102 and 103 is written for IEC 104
(it is contract 1.3) and applies to the serial standards except where those
sections say otherwise. For IEC 103, [its own section](#iec-103) comes first.

## IEC 101, 102 and 103

### Transport

The serial byte stream of the standard, unchanged, over one TCP connection,
as a terminal server (serial-to-IP converter) carries it: FT 1.2 frames
(IEC 60870-5-1) with the link procedures of IEC 60870-5-2 as the companion
standard profiles them. There is no IEC 104 APCI, no STARTDT and no k/w
window. The server is the controlled station (in unbalanced mode the
secondary station), listening on port **2401**; the client is the controlling
station and connects with `--host` and `--port`.

- **One connection at a time.** While a connection is served, another one is
  accepted and closed at once: a line has one controlling station. The
  feature `multipleConnections` is false.
- **Every connection is a line that has just come up.** The station's link
  layer starts afresh on each connection (frame count bit, queued data, link
  state); the station's process state (point values, selections) lives as
  long as the process, as for IEC 104.
- An adapter whose library speaks only to a tty joins a pseudo terminal to
  the TCP connection inside the image (lib60870: `src/bridge.c`). The harness
  never sees it.
- Line speed, parity and character timing do not exist on the TCP stream.
  Link timeouts are the adapters' own (a few hundred ms, repeated), never the
  bit-time formulas of a serial line.

### Link flags

Accepted by `server` and by every `client` operation of a serial standard,
with the same meaning on both sides; both sides must be given the same
values.

| Flag | Default | Meaning |
|------|---------|---------|
| `--link-mode M` | `unbalanced` | `unbalanced` (the client polls the station, class 1 and 2) or `balanced` (both stations send as primaries). A mode not in the image's `linkModes` is a usage error |
| `--link-address N` | 1 | Link address of the controlled station. All ones (255, 65535) is the broadcast address and not allowed |
| `--remote-link-address N` | `--link-address` | Balanced only: the link address of the controlling station, which the controlled station puts in the frames it sends as a primary. A usage error in unbalanced mode. An image without the feature `distinctBalancedAddresses` accepts only the value of `--link-address` |
| `--link-address-size N` | 1 | Octets of the link address field: 1 or 2; 0 (no address field) in balanced mode only |
| `--cot-size N` | 1 | Octets of the cause of transmission: 1, or 2 with the originator address |
| `--ca-size N` | 1 | Octets of the common address of the ASDU: 1 or 2 |
| `--ioa-size N` | 2 | Octets of the information object address: 1, 2 or 3 |
| `--common-address N` | 1 | Client: common address of the requests (the broadcast address is 255 or 65535 by `--ca-size`). Server: replaces the fixture's `station.commonAddress` |

A fixture address that does not fit `--ioa-size` (or a common address that
does not fit `--ca-size`) stops the server with exit 4; a client `--ioa` that
does not fit is a usage error. `--originator-address` is sent only with
`--cot-size 2`; with one octet the result reports originator 0.

There are no APCI, TLS, security or redundancy flags for the serial
standards: they are unknown flags (exit 2).

> The flag is `--link-mode`, not `--mode`: the `command` operation has had
> `--mode direct|select|sbo|cancel` since contract 1.0, and a client takes the
> link flags and the operation flags on one command line.

### Server

Flags: `--fixture`, `--bind-address`, `--port` (2401), `--ready-file`, the
link flags. Lifecycle, readiness, SIGTERM and the station's behaviour are as
for IEC 104 ([`server`](#server), [FIXTURES.md](FIXTURES.md)), with these
differences:

- The `ready` event has `standard`, `mode` (the link mode) and `linkAddress`
  besides `adapter`, `address`, `fixture` and `commonAddress`.
- Events: `ready`, `connection-opened`, `connection-closed` (the TCP
  connection), `interrogation`, `counter-interrogation`, `read`, `clock-sync`,
  `command`, `stopped`. No `data-transfer-*` events (`dataTransferEvents` is
  false).
- The CP56Time2a command types 58..64 and C_TS_TA_1 do not exist in IEC 101:
  a station refuses them with cause 44 (unknown type; the adapters are written
  so, no self-test sends one), and `--with-time` is an unknown flag of
  `command`. The test command is C_TS_NA_1 (type 104) with
  the fixed test bit pattern 55AA; its positive confirmation is the mirror
  with cause 7. Spontaneous reports after a command keep their CP56Time2a
  types (M_SP_TB_1 and the other IEC 101 types 30..36).

### Client

Every operation: open the TCP connection, start the link (request status of
link, reset of remote link, both confirmed), run the operation, close the
connection. In unbalanced mode the client polls the station (class 2, and
class 1 while ACD is set) all the time the connection is open; in balanced
mode it receives the station's user data as a secondary.

Operations: `connect` (`--hold-ms N`: keeps the link up, polling or idle,
then succeeds if it is still up), `interrogate`, `counter-interrogate`,
`read`, `clock-sync`, `test-command` (C_TS_NA_1), `command` (`--mode
direct|select|sbo|cancel`, `--qualifier`; no `--with-time`), `monitor`. The
other operations of IEC 104 are not in `clientOperations` and are usage
errors. Common flags: `--host`, `--port` (2401), `--common-address`,
`--originator-address`, `--connect-timeout-ms`, `--timeout-ms`, `--collect-ms`
and the link flags.

The result document is the one of [IEC 104](#result-document) with these
differences:

| Field | IEC 101/102/103 |
|-------|-----------------|
| `connected` | The link is initialized: the reset of the remote link was confirmed. A TCP connection whose link never starts is `connected: false` |
| `startdtConfirmed`, `stopdtConfirmed`, `secured`, `tls`, `file`, `redundancy`, `directory` | Absent |
| `asdus[].originator` | 0 when the cause of transmission has one octet |

| `error.code` | Meaning | Exit |
|--------------|---------|------|
| `connect-failed` | No TCP connection | 3 |
| `link-init-timeout` | TCP connected, but the link start-up was not confirmed within `--timeout-ms` (replaces `startdt-timeout`) | 3 |
| `connection-lost` | The connection closed during the link start-up (exit 3) or the operation (exit 1) | 3 or 1 |
| `timeout`, `negative-confirmation`, `failed` | As for IEC 104 | 1 |

### Capabilities

`standard` (`iec60870-5-101`, ...), `transport` (`tcp-stream`) and
`linkModes` (the modes the self-tests exercise) are required for a serial
standard. `protocol` has the value of `standard`. The features `unbalancedMode`,
`balancedMode` and `distinctBalancedAddresses` are added; the IEC 104-only
features (`apciParameters`, `tls*`, `file*`, `redundancy*`,
`secureAuthentication`, `dataTransferEvents`, `multipleConnections`,
`timeTaggedCommands`) are present and false. See [CAPABILITIES.md](CAPABILITIES.md).

## IEC 103

IEC 60870-5-103, the companion standard for the informative interface of
protection equipment. The [transport of IEC 101](#transport) is that of IEC 101: the FT 1.2
byte stream over one TCP connection, port **2401**, one connection at a time,
every connection a line that has just come up. The **server is the protection
equipment** (the secondary station) and the **client the control system** (the
primary station); there is no other arrangement in IEC 103. What this section
does not change is as for IEC 101.

An IEC 103 image may be a client only (`roles.server` false): many open IEC 103
stacks are masters. For such an image `server` is a usage error (exit 2) and
the image has no health check or default server; `print-fixture` still prints
the fixtures it bakes in.

### IEC 103 link

Unbalanced transmission only, with a link address of one octet (IEC 60870-5-103
clause 6.2.3: the address field is one octet; 255 is the broadcast address).
The common address of the ASDU, also one octet, is normally the link address.

| Flag | Default | Meaning |
|------|---------|---------|
| `--link-mode M` | `unbalanced` | Only `unbalanced`; anything else is a usage error |
| `--link-address N` | 1 | The station's link address, 0..254 |
| `--common-address N` | 1 | Client: the common address of the requests, 0..255. Server: replaces the fixture's `station.commonAddress`, 0..254. An image whose library cannot use another common address than its link address says so with a usage error |

`--remote-link-address`, `--link-address-size`, `--cot-size`, `--ca-size`,
`--ioa-size` and `--originator-address` do not exist for IEC 103 (fixed field
sizes, no information object addresses): they are unknown flags (exit 2).

The client starts the link with a reset of the communication unit (function
0, confirmed), which the library may precede with a request of the status of
the link (function 9) or follow with a reset of the frame count bit (function
7); `connected` is true when the reset was confirmed. It then fetches class 1
data while the station sets ACD (the identification the reset queued) before
it sends the request of the operation, and afterwards polls class 1 while ACD
is set and class 2 otherwise, for as long as the connection is open. Every
request is SEND/CONFIRM (function 3) unless the operation says otherwise.

### IEC 103 server

Flags: `--fixture` (default `/fixtures/baseline/fixture.json`, the IEC 103
format of [FIXTURES.md](FIXTURES.md#iec-103)), `--bind-address`, `--port`
(2401), `--ready-file`, `--link-mode`, `--link-address`, `--common-address`.
The station's behaviour is the fixture's ([FIXTURES.md](FIXTURES.md#iec-103)).

| `event` | Other fields | When |
|---------|--------------|------|
| `ready` | `adapter`, `address`, `fixture`, `commonAddress`, `standard` (`iec60870-5-103`), `mode` (`unbalanced`), `linkAddress` | Once, when the station accepts connections |
| `connection-opened`, `connection-closed` | `peer` | The TCP connection |
| `interrogation` | `commonAddress`, `scn`, `accepted` | ASDU 7 received |
| `clock-sync` | `time`, `broadcast` (sent to link address 255) | ASDU 6 received |
| `command` | `fun`, `inf`, `dco` (the octet), `rii`, `outcome` (`executed` or `rejected`) | ASDU 20 received |
| `stopped` | | Once, after a clean stop |

### IEC 103 client

Common flags: `--host`, `--port` (2401), `--common-address`,
`--connect-timeout-ms`, `--timeout-ms`, `--collect-ms`, `--link-mode`,
`--link-address`. Every wait of an operation is bounded by `--timeout-ms`.

| Operation | Flags | Sends | Succeeds when |
|-----------|-------|-------|---------------|
| `connect` | `--hold-ms N` (0) | The link start-up only | The link is still up after the hold, the client polling all the time |
| `interrogate` | `--scn N` (0) | ASDU 7, cause 9, FUN 255, INF 0, the scan number | ASDU 8 (termination of general interrogation) with that scan number arrives: `terminated` |
| `clock-sync` | `--time T` (now), `--broadcast` | ASDU 6, cause 8, FUN 255, INF 0, CP56Time2a. With `--broadcast` SEND/NO REPLY (function 4) to link address 255 and common address 255; only with the feature `broadcastClockSync` | Addressed: ASDU 6 with cause 8 arrives (confirmation `{cot: 8}`). Broadcast: the frame was sent; nothing can answer |
| `command` | `--fun N`, `--inf N` (required), `--dco D` (required: 1 or `off`, 2 or `on`; 0 and 3 are sent as they are), `--rii N` (1) | ASDU 20 (general command), cause 20, the DCO and the return information identifier | ASDU 1 or 2 with the command's FUN and INF, cause 20 and the RII as SIN arrives (confirmation `{cot: 20}`). Cause 21 is the error `negative-confirmation` (confirmation `{cot: 21, negative: true}`). The change of state (ASDU 1, cause 12, clause 7.4.4) follows the confirmation: it is in `asdus` when `--collect-ms` covers it |
| `monitor` | `--duration-ms N` (1000), `--max-asdus N` (0) | Nothing but the polls | The duration passed or the number of ASDUs arrived, with the link still up |

The other operations of IEC 101 and 104 (`counter-interrogate`, `read`,
`test-command`, files, ...) are not in `clientOperations` and are usage errors.
An operation the library cannot carry out as defined (`--broadcast` without the
feature) is a usage error; a request the library refuses to send for a reason of
its own is the error `failed`.

The result document is that of IEC 101: `connected` is the confirmed reset of
the communication unit, and there is no `startdtConfirmed`. `confirmations`
has the causes 8, 20 and 21 of the table above, `terminated` is the
termination of the interrogation. `asdus` has every ASDU received, the
identification after the reset included; cyclic measurands (cause 2) arrive
whenever the client polls class 2, so their number in a result is not fixed.

### IEC 103 ASDU documents

The fields of [ASDU documents](#asdu-documents) with these differences:

- `type` is the name below, `typeId` the type identification; `cot` is the cause
  of transmission octet as received, 0..255 (IEC 103 has no P/N or T bit, so
  `negative` and `test` are false and `originator` is 0). `sequence` and
  `count` are the SQ bit and the number of the VSQ octet.
- `objects` has one entry: the information object, with `fun` (function type)
  and `inf` (information number) instead of `ioa`, and the fields of its type.
- An adapter decodes the information elements as the standard lays them out.
  When their length is not the standard's, or the adapter does not decode the
  type, the object has `data` (the elements after FUN and INF, lower-case hex)
  instead of the fields.

| `typeId` | `type` | Fields |
|---------:|--------|--------|
| 1 | `time-tagged-message` | `dpi` 0..3, `time32`, `sin` |
| 2 | `time-tagged-message-relative` | `dpi`, `ret` (ms), `fan`, `time32`, `sin` |
| 3 | `measurands-i` | `values` |
| 4 | `time-tagged-measurands` | `scl` (short-circuit location, a number), `ret`, `fan`, `time32` |
| 5 | `identification` | `col` (compatibility level), `ascii` (the eight characters, trailing spaces and NULs removed), `software` (four octets, hex) |
| 6 | `time-synchronization` | `time` (CP56Time2a) |
| 7 | `general-interrogation` | `scn` |
| 8 | `general-interrogation-termination` | `scn` |
| 9 | `measurands-ii` | `values` |
| 20 | `general-command` | `dco` (the octet), `rii` |
| 10, 11, 21, 23..31 | `generic-data`, `generic-identification`, `generic-command`, `disturbance-list`, `disturbance-order`, `disturbance-acknowledgement`, `disturbance-ready`, `channel-ready`, `tags-ready`, `tags`, `disturbance-values`, `disturbance-end` | `data` (not decoded in this version) |

`time32` is a CP32Time2a, `HH:MM:SS.mmm`: it has no date. `timeInvalid: true`
when its IV bit is set. `values` is one `{value, ov, er}` per measurand: the
signed 13-bit value -4096..4095 and the overflow and error bits, in the order of
the ASDU (the number of the VSQ octet).

### IEC 103 capabilities

`standard` `iec60870-5-103`, `transport` `tcp-stream`, `linkModes`
`["unbalanced"]`, and `asduTypes`: `{monitor, control}`, the type
identifications the image decodes (client) or sends (server) in each direction.
`pointTypes` and `commandTypes` are empty. The features of IEC 101/104 are
present and false except `generalInterrogation`, `clockSync` and
`directCommands` (the general command); `unbalancedMode` is true and
`balancedMode` false. IEC 103 adds:

| Feature | Meaning |
|---------|---------|
| `generalCommand` | ASDU 20 and its confirmation |
| `identification` | Server: ASDU 5 after a reset of the communication unit. Client: reports it |
| `measurands` | Server: the fixture's measurands as class 2 data. Client: decodes ASDU 3 and 9 |
| `broadcastClockSync` | `clock-sync --broadcast` (client) or a broadcast ASDU 6 is accepted (server) |
| `genericServices` | ASDU 10, 11 and 21 (no operation in this version) |
| `disturbanceData` | ASDU 23..31 (no operation in this version) |

## Image layout

| Item | Value |
|------|-------|
| Image | `ghcr.io/otfabric/iec60870-5-interop-<standard>-<stack>`; OT Fabric's own stack is the stack `otfabric` (`own/`, not in this version) |
| Entrypoint | `iec60870-5-interop` (`/usr/local/bin/iec60870-5-interop`) |
| Default command | `server` (`print-capabilities` for an IEC 103 image that is a client only) |
| Port | 2404/tcp (IEC 104), 2401/tcp (IEC 101, 102, 103); not exposed by a client-only image |
| User | non-root, uid 10001 |
| Fixtures | `/fixtures/<name>/fixture.json`, the fixtures of the image's standard (IEC 104: `baseline` and `extended`; IEC 101 and 103: `baseline`); mount over `/fixtures` to replace |
| Readiness file | `/run/iec60870-5-interop/ready` |
| Health check | built in: `test -f /run/iec60870-5-interop/ready`; none in a client-only image |
| Licences | `/licenses/` |

## Commands

```text
iec60870-5-interop server [flags]
iec60870-5-interop client <operation> --host HOST [flags]
iec60870-5-interop print-capabilities
iec60870-5-interop print-fixture [NAME]
iec60870-5-interop version
```

Flags are `--name value`; switches are bare `--name`. An unknown flag is a
usage error (exit 2), never ignored.

### APCI flags

Accepted by `server` and by every `client` operation. Times are in seconds.

| Flag | Default | Meaning |
|------|---------|---------|
| `--k N` | 12 | Maximum unacknowledged I frames sent |
| `--w N` | 8 | Acknowledge after this many received I frames |
| `--t0 S` | 30 | Connection establishment timeout |
| `--t1 S` | 15 | Acknowledgement / confirmation timeout |
| `--t2 S` | 10 | Acknowledgement delay, `t2 < t1` |
| `--t3 S` | 20 | Idle time before TESTFR |

`1 <= w <= k <= 32767`, `0 < t2 < t1`. Whether `--t3 0` (idle test off) is
accepted is adapter specific: lib60870 accepts it, OpenMUC requires
`t3 >= t1`, wendy512 requires `t3 >= 1` and every timer at most 255 seconds, cbsigma
requires `t3 >= 1`, part5 requires `t3 >= 1` and `t0`, `t1` and `t2` at most 255 seconds.
A value the library cannot run with is a usage error, never replaced by
another one. See [CAPABILITIES.md](CAPABILITIES.md).

## Security flags

The application-layer security of IEC 62351-5 in the A-Profile of
IEC TS 60870-5-7: an association between the controlling station and the
controlled station, authenticated with certificates, and every ASDU after it
protected by a MAC (and, with a data protection algorithm, encrypted). Only
an image with the feature `secureAuthentication` accepts these flags; for any
other they are unknown flags (exit 2).

Accepted by `server` and by every `client` operation.

| Flag | Default | Meaning |
|------|---------|---------|
| `--secure` | off | Run the A-Profile. Without it every other flag of this table is a usage error |
| `--security-cert PATH` | required | Own certificate, PEM |
| `--security-key PATH` | required | Own private key, PEM: PKCS #8 or SEC 1, on secp256r1 (P-256) or X25519 |
| `--security-trust PATH` | required | The remote stations this one associates with, PEM, one or more certificates. A CA certificate (basic constraints CA:TRUE) is an authority: a station certificate it signed is accepted. Any other certificate is pinned: that station's self-signed certificate is accepted, by fingerprint |
| `--association-id N` | 1 | The station's association identity, 1..65535: AIM of the controlling station (`client`), AIS of the controlled station (`server`) |

`client` only:

| Flag | Default | Meaning |
|------|---------|---------|
| `--mal N` | 4 | MAC algorithm the controlling station proposes, by its code |
| `--dpa N` | `--mal` | Data protection algorithm, by its code |

`server` only:

| Flag | Default | Meaning |
|------|---------|---------|
| `--roles R,...` | none | The roles every controlling station has: `viewer`, `operator`, `engineer`, `installer`, `secadm`, `secaud`, `rbacmnt`. Without the flag there is no access control: everything is permitted |

A key on X25519 cannot sign, so its certificate is signed by an authority;
only a secp256r1 certificate can be self-signed and pinned. Which algorithm
codes an image supports is the library's: an algorithm it does not support
is a usage error.

With `--secure`, the server accepts nothing but secured ASDUs; a client
without security gets no answer (`timeout`). A request the roles of
`--roles` do not permit is refused as [FIXTURES.md](FIXTURES.md) says. The
client sets up the association after STARTDT con and waits for it within
`--timeout-ms` before the operation starts; no association is the error
`security-failed`.

Key material is mounted into the container: the files must be readable by
uid 10001.

## Transport security flags

TLS with mutual authentication, as IEC 62351-3 profiles it for IEC 60870-5-104:
the IEC 104 connection runs inside the TLS session. `server` accepts these flags with the feature `tlsServer`, and every `client`
operation with the feature `tlsClient`; otherwise they are unknown flags (exit 2).
The feature `tls` is true when an image has both.

| Flag | Default | Meaning |
|------|---------|---------|
| `--tls` | off | Run the connection over TLS, version 1.2 or later. Without it every other flag of this table is a usage error |
| `--tls-cert PATH` | required | Own certificate, PEM |
| `--tls-key PATH` | required | Own private key, PEM, PKCS #8, on secp256r1 (P-256) |
| `--tls-trust PATH` | required | The authorities that sign the remote station's certificate, PEM, one or more certificates |

- Both sides present a certificate, and each refuses a certificate its
  `--tls-trust` authorities did not sign. A file with a key or certificate that
  cannot be loaded is a usage error, and nothing is sent.
- A host name or address is not checked: the certificate says who the station
  is, whatever the network calls it.
- `--tls` with `--secure` is a usage error in this version of the contract.
- The port does not change: `--port` still says it (IEC 62351-3's 19998 is for
  the caller to pass).

With `--tls`, the client's result document has `tls`. Everything that goes wrong
between the TCP connection and STARTDT con is the error `tls-failed`, whatever
the stack saw: a refused certificate, a station that does not speak TLS, a
handshake or STARTDT that took longer than `--timeout-ms`, or a connection the
station closed after a handshake (with TLS 1.3 a client learns only then that
its certificate was refused). A station that cannot be reached is still
`connect-failed`.

A station with `--tls` accepts nothing but TLS: a client without TLS gets no
STARTDT con. Key material is mounted into the container: the files must be
readable by uid 10001.

## `server`

Runs a controlled station that serves one fixture until it receives SIGTERM
or SIGINT.

| Flag | Default | Meaning |
|------|---------|---------|
| `--fixture PATH` | `/fixtures/baseline/fixture.json` | Fixture to serve |
| `--bind-address ADDR` | `0.0.0.0` | Listen address |
| `--port N` | 2404 | Listen port |
| `--ready-file PATH` | `/run/iec60870-5-interop/ready` | Readiness file |
| `--redundancy-group` | off | All connections form one [redundancy group](FIXTURES.md#redundancy-group). Only with the feature `redundancyServer`; for any other image it is an unknown flag (exit 2) |
| APCI flags | | |

Lifecycle:

1. The fixture is loaded and validated. An invalid fixture ends the process
   with exit 4 before anything listens.
2. The listener is opened. Failure ends the process with a non-zero exit.
3. The readiness file is written and the `ready` event is printed. From here
   on the station accepts IEC 60870-5-104 connections. **Readiness is the
   file (or the event), not the open port and not a delay.**
4. On SIGTERM/SIGINT the readiness file is removed, connections are closed,
   the `stopped` event is printed and the process exits 0.

Every connection is an independent session with its own STARTDT/STOPDT state,
unless `--redundancy-group` is given. Up to 16 simultaneous connections are
accepted. The station state (point
values, selections) is shared by all connections and lives as long as the
process: restart the container to return to the fixture.

### Server events

One JSON object per line on stdout.

| `event` | Other fields | When |
|---------|--------------|------|
| `ready` | `adapter`, `address`, `fixture`, `commonAddress`; `secure` (true) with `--secure`; `tls` (true) with `--tls`; `redundancyGroup` (true) with `--redundancy-group` | Once, when the station accepts connections |
| `connection-opened` | `peer` (`ip:port`) | A controlling station connected |
| `data-transfer-started` | `peer` | STARTDT confirmed. Only with the feature `dataTransferEvents` |
| `data-transfer-stopped` | `peer` | STOPDT confirmed. Only with the feature `dataTransferEvents` |
| `connection-closed` | `peer` | The connection ended |
| `interrogation` | `commonAddress`, `qoi`, `accepted` | C_IC_NA_1 received |
| `counter-interrogation` | `commonAddress`, `qcc`, `accepted` | C_CI_NA_1 received |
| `read` | `ioa`, `accepted` | C_RD_NA_1 received |
| `clock-sync` | `time` | C_CS_NA_1 received |
| `command` | `type`, `ioa`, `cot`, `value`, `select`, `outcome` | A process command for the station was received |
| `file-transfer` | `ioa`, `name`, `success` | A file transfer ended: acknowledged by the controlling station (`success` true) or not. Only with the feature `fileServer` |
| `file-received` | `ioa`, `name`, `length`, `sha256`, `success` | A file sent by a controlling station arrived, complete (`success` true: every length and checksum matched, and it is stored) or not. `length` and `sha256` (lower-case hex) are of what arrived, which on a failure may be less than was announced. Only with the feature `fileUploadServer` |
| `stopped` | | Once, after a clean stop |

`command.type` is the type identification on the wire (`C_SC_TA_1` for a
time-tagged single command). `command.outcome` is one of `executed`,
`selected`, `deactivated`, `rejected` (negative confirmation) and
`unknown-ioa` (no `value`/`select` in that case). `command.value` has the
representation of [ASDU documents](#asdu-documents).

Requests that are refused before they reach the station model (unknown
common address, unknown type, invalid cause) produce no event.

The order of events of one connection follows the protocol. No event is
emitted for test frames, acknowledgements or the test command.

## `client`

Runs one bounded operation as a controlling station and prints one result
document.

```text
iec60870-5-interop client <operation> --host HOST [--port N] [common flags] [operation flags]
```

Every operation: connect, send STARTDT act and wait for the confirmation, run
the operation, close the connection.

### Common flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--host HOST` | required | Station to connect to |
| `--port N` | 2404 | |
| `--common-address N` | 1 | Common address of the requests |
| `--originator-address N` | 0 | Originator address of the requests |
| `--connect-timeout-ms N` | 5000 | TCP connect timeout |
| `--timeout-ms N` | 5000 | Timeout of each wait for the station (STARTDT con, confirmation, termination, read answer) |
| `--collect-ms N` | 0 | After a successful operation, keep receiving for this long before closing |
| APCI flags | | |
| [Security flags](#security-flags) | | |
| [Transport security flags](#transport-security-flags) | | |

### Operations

| Operation | Flags | Sends | Succeeds when |
|-----------|-------|-------|---------------|
| `connect` | `--hold-ms N` (0) | STARTDT act; after the hold, STOPDT act | STOPDT con is received. During the hold the connection is idle, so `t3` and TESTFR run |
| `interrogate` | `--qoi N` (20) | C_IC_NA_1 activation. QOI 21..36 are the interrogation groups 1..16; only with the feature `groupInterrogation` are they answered by anything but a refusal | Positive activation confirmation, then activation termination |
| `counter-interrogate` | `--qcc N` (5) | C_CI_NA_1 activation; `qcc` is the raw qualifier octet | Positive activation confirmation, then activation termination |
| `read` | `--ioa N` (required) | C_RD_NA_1 | An ASDU with cause 5 (request) containing the address arrives |
| `clock-sync` | `--time T` (now) | C_CS_NA_1 activation; `T` like `2026-01-02T03:04:05.678Z` | Positive activation confirmation |
| `test-command` | | C_TS_TA_1 activation | Positive activation confirmation |
| `command` | see below | A process command | See below |
| `monitor` | `--duration-ms N` (1000), `--max-asdus N` (0: no limit) | Nothing | The duration passed or the number of ASDUs was received, with the connection still up |
| `file-get` | `--ioa N` (required), `--name N` (1) | The file transfer procedure in monitor direction: see [below](#file-get) | The whole file arrived, every checksum and length matched and the file was acknowledged. Only with the feature `fileClient` |
| `file-put` | `--ioa N` (required), `--name N` (1), `--size N` (required), `--section-size N` (`--size`) | The file transfer procedure in control direction: see [below](#file-put) | The station called the file and every section, and acknowledged every section and the file. Only with the feature `fileUploadClient` |
| `redundancy` | `--switchover M` (`stopdt`) | Two connections to one station, the second taking over: see [below](#redundancy) | Every step of the sequence below. Only with the feature `redundancyClient` |
| `file-dir` | | The call of the directory: see [below](#file-dir) | The entry marked as the last one arrived. Only with the feature `fileDirectoryClient` |

`command` flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `--type T` | required | `C_SC_NA_1`, `C_DC_NA_1`, `C_RC_NA_1`, `C_SE_NA_1`, `C_SE_NB_1` or `C_SE_NC_1` |
| `--ioa N` | required | Information object address |
| `--value V` | required | `C_SC`: `true`/`false` (also `1`/`0`, `on`/`off`). `C_DC`, `C_RC`: the two-bit state 0..3. `C_SE_NA`: raw NVA -32768..32767. `C_SE_NB`: -32768..32767. `C_SE_NC`: a decimal number |
| `--mode M` | `direct` | See below |
| `--qualifier N` | 0 | QU of `C_SC`/`C_DC`/`C_RC`, QL of the set points |
| `--with-time` | off | Send the CP56Time2a variant (`C_SC_TA_1`, ...) with the current time |

| Mode | Sequence | Succeeds when |
|------|----------|---------------|
| `direct` | execute | Positive confirmation, then activation termination |
| `select` | select | Positive confirmation |
| `sbo` | select, wait for its confirmation, execute | Both confirmations positive, then activation termination |
| `cancel` | select, wait for its confirmation, deactivate | Both confirmations positive (causes 7 and 9) |

A value that is syntactically valid but not permitted by the protocol (a
double command state of 0 or 3) is sent as is: refusing it is the station's
job.

### `file-get`

Downloads one file with the procedure of IEC 60870-5-101 7.4.11. Every ASDU
has cause 13 (file transfer); `--ioa` is the address of the file and `--name`
its name of file.

| Step | Controlling station | Controlled station |
|-----:|---------------------|--------------------|
| 1 | F_SC_NA_1, SCQ 1 (select file) | F_FR_NA_1 (file ready) with the length of the file |
| 2 | F_SC_NA_1, SCQ 2 (request file) | F_SR_NA_1 (section ready) with the length of section 1 |
| 3 | F_SC_NA_1, SCQ 6 (request section) | F_SG_NA_1 segments, then F_LS_NA_1 with LSQ 3 (last segment) and the checksum of the section |
| 4 | F_AF_NA_1, AFQ 3 (section acknowledged) | F_SR_NA_1 for the next section: back to step 3. After the last section F_LS_NA_1 with LSQ 1 (last section) and the checksum of the file |
| 5 | F_AF_NA_1, AFQ 1 (file acknowledged) | |

A checksum is the sum of the octets modulo 256. The client checks the
checksum and the length of every section and of the file and answers a
mismatch with AFQ 4 or 2 (not acknowledged) and the error `failed`.

A station that refuses the selection (unknown address, wrong name) answers
with the mirrored F_SC_NA_1 and cause 47, or with a file ready whose FRQ has
the negative bit: the error is `negative-confirmation`. The refusal is in
`asdus`; `confirmations` stays empty, since it is not a mirror with cause 7
or 9.

### `file-put`

Sends one file with the procedure of IEC 60870-5-101 7.4.11, the station doing
the calling. Every ASDU has cause 13. `--ioa` is the address of the file and
`--name` its name of file; `--size` (1..65536) is its length and `--section-size`
(1..`--size`) the length of every section but the last. NOS is one octet, so more
than 255 sections are a usage error. The content is defined
as in a fixture: octet `i`, counting from 0, is `(i + ioa) mod 251`.

| Step | Controlling station | Controlled station |
|-----:|---------------------|--------------------|
| 1 | F_FR_NA_1 (file ready) with the length of the file | F_SC_NA_1, SCQ 2 (request file) |
| 2 | F_SR_NA_1 (section ready), NOS 1, with the length of the section | F_SC_NA_1, SCQ 6 (request section), the same NOS |
| 3 | F_SG_NA_1 segments, then F_LS_NA_1 with LSQ 3 and the checksum of the section | F_AF_NA_1, AFQ 3 (section acknowledged). Then back to step 2 for the next section |
| 4 | After the last section, F_LS_NA_1 with LSQ 1 and the checksum of the file | F_AF_NA_1, AFQ 1 (file acknowledged) |

A segment carries at most 236 octets. The NOS of the F_LS_NA_1 of step 4 is the
client's and the station's F_AF_NA_1 repeats it; [FIXTURES.md](FIXTURES.md) says
why it is not compared.

A station that refuses the file answers step 1 with the mirrored F_FR_NA_1 and
cause 47, or with F_AF_NA_1 and a negative AFQ (2 or 4 in the low four bits; the high four
may name the error) at step 3 or 4: the error is `negative-confirmation`. The refusal is in `asdus`; `confirmations` stays
empty.

### `file-dir`

Sends F_SC_NA_1 with cause 5 (request), address 0, NOF 0, NOS 0 and SCQ 0, and
collects the F_DR_TA_1 answers with cause 5 up to the entry with the LFD bit
(0x20) in its SOF. A mirrored F_SC_NA_1 with cause 44..47 is the error
`negative-confirmation`; it is in `asdus`, and `confirmations` stays empty.

### `redundancy`

A controlling station with two connections to one station, IEC 60870-5-104
clause 10. The first is the primary and the second the standby. Both are
established at the start, and the standby stays in STOPDT until step 4.

| Step | `--switchover stopdt` | `--switchover takeover` |
|-----:|-----------------------|-------------------------|
| 1 | Connect the primary and the standby. STARTDT on the primary | The same |
| 2 | C_IC_NA_1 (QOI 20) on the primary, until its activation termination | The same |
| 3 | STOPDT on the primary, until STOPDT con; the connection stays up | Nothing: the primary is still started |
| 4 | STARTDT on the standby, until STARTDT con | The same |
| 5 | Wait for one ASDU with cause 3 on the standby: the [event](FIXTURES.md#redundancy-group) the station queued | Nothing |
| 6 | C_IC_NA_1 (QOI 20) on the standby, until its activation termination | The same |
| 7 | STOPDT on the standby, until STOPDT con; close both | The same |

An I frame on a connection outside the time it is started is a stray: on the
standby before its STARTDT con, or on the primary after STOPDT con (`stopdt`) or
after the standby's STARTDT con (`takeover`). Any stray is the error `failed`.
Without an event at step 5 the error is `timeout`. If the primary closes before
step 4 ends with `stopdt`, the error is `connection-lost`; with `takeover` it may
close, which [FIXTURES.md](FIXTURES.md#redundancy-group) leaves to the library.

`connected` is true when both connections were established. `startdtConfirmed` is
the primary's, `stopdtConfirmed` the standby's at step 7. No STOPDT con at step 3
or 7 is the error `stopdt-timeout`, no STARTDT con at step 4 `startdt-timeout`.
`confirmations` has one entry per C_IC_NA_1, `terminated` says that the last one
ended, and `asdus` has what arrived on either connection in the order it arrived.

### Result document

Exactly one JSON object on one line of stdout, for every invocation that got
past argument parsing, including failures.

| Field | Type | Meaning |
|-------|------|---------|
| `schemaVersion` | string | `"1.0"` |
| `adapter` | string | Adapter that ran the operation |
| `operation` | string | As given on the command line |
| `ok` | boolean | The operation succeeded as defined above |
| `error` | object or null | `{ "code", "message" }` when `ok` is false |
| `connected` | boolean | The TCP connection was established |
| `startdtConfirmed` | boolean | STARTDT con was received |
| `stopdtConfirmed` | boolean | `connect` and `redundancy` only: STOPDT con was received |
| `confirmations` | array | `{ "cot", "negative" }` for each mirror of the request that confirms or refuses it, in order |
| `terminated` | boolean | An activation termination of the request was received |
| `secured` | boolean | With `--secure` only: the association was established |
| `tls` | boolean | With `--tls` only: STARTDT con arrived over the TLS session |
| `file` | object | `file-get` and `file-put` only: `ioa`, `name`, `length` (announced by file ready), `received` (octets that arrived; for `file-put`, octets sent in sections the station called), `sections` (sections called) and `sha256` (lower-case hex of those octets) |
| `redundancy` | object | `redundancy` only: `switchover`; `primaryAsdus` and `standbyAsdus` (ASDUs received on each while it was started); `strayAsdus` (I frames outside that time); `queuedEvent` (step 5: the event arrived; false with `takeover`) |
| `directory` | array | `file-dir` only: one `{ioa, nof, lof, sof}` per entry received, in order |
| `asdus` | array | Every ASDU received, in order: see [ASDU documents](#asdu-documents) |
| `elapsedMs` | number | Wall-clock duration |

A mirror counts as a confirmation when it has the type of the request and
cause 7, 9, 17 or 44..47 (IEC 103: see [IEC 103 client](#iec-103-client)). `negative` is true when the P/N bit is set or the
cause is 17 or 44..47. Cause 17 is the refusal of IEC 62351-5 (not
authorized).

`error.code`:

| Code | Meaning | Exit |
|------|---------|------|
| `connect-failed` | No TCP connection | 3 |
| `startdt-timeout` | No STARTDT con within `--timeout-ms` | 1 |
| `stopdt-timeout` | `connect`, `redundancy`: no STOPDT con | 1 |
| `timeout` | The station did not answer in time | 1 |
| `negative-confirmation` | The station refused: negative confirmation or cause 17 or 44..47 | 1 |
| `security-failed` | `--secure`: no association with the station within `--timeout-ms` | 1 |
| `tls-failed` | `--tls`: no TLS session with the station that got as far as STARTDT con | 1 |
| `connection-lost` | The connection closed during the operation | 1 |
| `failed` | Anything else; see `message` | 1 |

`message` is for people and not part of the contract.

## ASDU documents

```json
{
  "type": "M_ME_TF_1", "typeId": 36,
  "cot": 11, "negative": false, "test": false,
  "originator": 0, "commonAddress": 1,
  "sequence": false, "count": 1,
  "objects": [ { "ioa": 202, "value": 49.5, "quality": [], "time": "2026-10-09T15:35:12.345Z" } ]
}
```

| Field | Meaning |
|-------|---------|
| `type` | Mnemonic of the type identification, or `null` for a type the adapter does not model |
| `typeId` | Type identification as a number |
| `cot` | Cause of transmission (6 bits) |
| `negative`, `test` | P/N and T bits |
| `originator`, `commonAddress` | |
| `sequence` | SQ bit |
| `count` | Number of objects or elements announced |
| `objects` | One entry per information object; with SQ = 1 one entry per element, addresses expanded. Empty when `type` is `null` |

Every object has `ioa`. The other fields depend on the type; a type with
CP56Time2a adds `time`, and `timeInvalid: true` when the IV bit of the time
tag is set.

| Types | Fields |
|-------|--------|
| `M_SP_NA_1`, `M_SP_TB_1` | `value` boolean, `quality` |
| `M_DP_NA_1`, `M_DP_TB_1` | `value` 0..3, `quality` |
| `M_ST_NA_1`, `M_ST_TB_1` | `value` -64..63, `transient` boolean, `quality` |
| `M_BO_NA_1`, `M_BO_TB_1` | `value` 0..4294967295, `quality` |
| `M_ME_NA_1`, `M_ME_TD_1` | `value` raw NVA -32768..32767, `normalized` = `value` / 32768, `quality` |
| `M_ME_NB_1`, `M_ME_TE_1` | `value` -32768..32767, `quality` |
| `M_ME_NC_1`, `M_ME_TF_1` | `value` number (IEEE 754 single precision), `quality` |
| `M_IT_NA_1`, `M_IT_TB_1` | `value` 32-bit signed, `sequence` 0..31, `quality` |
| `C_SC_NA_1`, `C_SC_TA_1` | `value` boolean, `select`, `qualifier` |
| `C_DC_NA_1`, `C_DC_TA_1`, `C_RC_NA_1`, `C_RC_TA_1` | `value` 0..3, `select`, `qualifier` |
| `C_SE_NA_1`, `C_SE_TA_1` | `value` raw NVA, `normalized`, `select`, `qualifier` |
| `C_SE_NB_1`, `C_SE_TB_1` | `value`, `select`, `qualifier` |
| `C_SE_NC_1`, `C_SE_TC_1` | `value` number, `select`, `qualifier` |
| `C_IC_NA_1` | `qoi` |
| `C_CI_NA_1` | `qcc` raw qualifier octet |
| `C_RD_NA_1` | |
| `C_CS_NA_1` | `time` |
| `C_TS_TA_1` | `counter`, `time` |
| `C_TS_NA_1` | `fbp`: the fixed test bit pattern, 21930 (55AA); `null` when the library only reports that it is not that pattern |
| `M_EI_NA_1` | `coi` |
| `F_FR_NA_1` | `nof`, `lof`, `frq` |
| `F_SR_NA_1` | `nof`, `nos`, `lof`, `srq` |
| `F_SC_NA_1` | `nof`, `nos`, `scq` |
| `F_LS_NA_1` | `nof`, `nos`, `lsq`, `chs` |
| `F_AF_NA_1` | `nof`, `nos`, `afq` |
| `F_SG_NA_1` | `nof`, `nos`, `data` (the segment, lower-case hex) |
| `F_DR_TA_1` | `nof`, `lof`, `sof`, `time` |

`nof` and `nos` are the name of file and of section, `lof` the length of the
file or section; the qualifiers (`frq`, `srq`, `scq`, `lsq`, `afq`) and the
status of file (`sof`) are the raw octets and `chs` is the checksum.

An adapter reports what its library hands it. A library that drops a type on
reception leaves no trace of it in `asdus`: wendy512 cannot receive the
CP56Time2a command types or F_SG_NA_1 and declares so in its capabilities: it
has no `fileTransfer`, `fileClient` or `fileUploadServer`, since each of those
needs a segment to arrive.

`quality` is an array of flag abbreviations in this order: `OV`, `BL`, `SB`,
`NT`, `IV`; for integrated totals `CY`, `CA`, `IV`. An empty array means
good. A received `M_BO_NA_1` reports its quality like any other point, whether
or not the adapter can serve one (`pointQualityOnBitstring`).

`time` is UTC, `YYYY-MM-DDTHH:MM:SS.mmmZ`. Adapters put UTC on the wire.

Normalized values are exchanged as the raw 16-bit number because the
libraries disagree on the scaling of the floating-point form (division by
32768 or by 32767.5). `normalized` is a convenience and always `value / 32768`.

Numbers are JSON numbers; a single-precision value is printed with the digits
of its double-precision conversion, so compare numerically.

## `print-capabilities`

Prints one JSON object on one line and exits 0. See
[CAPABILITIES.md](CAPABILITIES.md) for the fields and the values of each
adapter. Needs no network and no fixture.

## `print-fixture`

`print-fixture [NAME]` (default `baseline`) writes the fixture baked into the
image to stdout, byte for byte, and exits 0. An unknown name exits 1.

A consumer reads the expected values from the image it tests against, so it
needs no checkout of this repository and cannot drift from the image.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success. `server`: clean stop after SIGTERM/SIGINT |
| 1 | The operation failed (see `error.code`); `server`: could not listen |
| 2 | Usage error: unknown command, operation or flag, missing or invalid value. Nothing was sent |
| 3 | `client`: the connection could not be established |
| 4 | `server`: the fixture is missing or invalid |

## Output rules

- **stdout carries JSON only**: server events as JSON Lines, one client
  result, one capability document. `print-fixture` and `version` are the
  exceptions and print their own format.
- **stderr carries diagnostics** for people. Nothing on stderr is part of the
  contract. Library logging goes there too.
- Field names and exit codes are stable within a major contract version.
  Fields may be added in a minor version; consumers ignore fields they do not
  know.
- Every wait is bounded: by `--timeout-ms`, `--connect-timeout-ms`, the
  duration flags or the APCI timers. No client operation runs indefinitely.

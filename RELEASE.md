# iec60870-5-interop Releases

## How a release is made

1. Write the notes of the version below and set `interop_version` in `versions.yaml` to it, in the same change. `make validate` (and CI) fails when RELEASE.md has no notes for `interop_version`.
2. `make ci` on the commit to release.
3. Push the tag `vX.Y.Z`, or run the Release workflow (bump from the latest tag). It builds every image natively on amd64 and arm64, runs the smoke and cross-stack self-tests on each, pushes `<image>:vX.Y.Z-<arch>`, joins them into the multi-arch `<image>:vX.Y.Z` and publishes the GitHub release with `manifest.json` (image digests), `versions.yaml` and `checksums.txt`.

The notes are everything after the heading `## vX.Y.Z` up to the next line
that is exactly `---` (`scripts/release-notes.sh vX.Y.Z` prints it): one
section per version, newest first, each ending with a `---` line.

| What changed | Version |
|--------------|---------|
| Contract or fixture format changed incompatibly | major |
| New standard, operation, behaviour, fixture, adapter or document field | minor |
| Upstream pin, base image or bug fix with unchanged contract | patch |

---

## v0.4.0

**Date:** not released (prepared 2026-11-11)
**Previous release:** v0.3.0

### Added

- Image `iec101-digitalpetri`: digitalpetri/iec60870 at commit `33e52b36` (no release, 0.1.0-SNAPSHOT; EPL-2.0, Java), the third IEC 101 reference and the first not debugged against lib60870-C. The library's own TcpIec101Client and TcpIec101Server (its FT 1.2 link over Netty, no bridge), built from the pinned commit in a multi-stage Dockerfile; unbalanced and balanced, client and server, every field size. The station answers the fixture in the library's raw-ASDU hook. Status validated.
- Container contract 2.3 (docs/CONTAINER_CONTRACT.md, "Field sizes"): IEC 101 capability documents list the field sizes the image takes (`linkAddressSizes`, `cotSizes`, `caSizes`, `ioaSizes`; another is a usage error), the server's `ready` event reports them (`linkAddressSize`, `cotSize`, `caSize`, `ioaSize`), and the feature `unbalancedCommonAddress` says whether an unbalanced client sends a request with a common address other than its link address. lib60870 and cbsigma take every size and have the feature; digitalpetri takes every size and has not.
- 33 IEC 101 cases with field sizes other than the defaults, `case.fields-<sizes>.*` in `tests/iec101/cases.tsv`: 2/2/2/3, 1/2/1/3 and 2/1/2/2 (link address, cause, common address, object address) in both link modes and 0/1/1/2 (no link address field) in balanced mode, including the broadcast common address 65535, 255 as an ordinary address, a link address of 300 and an object address of 70000. `scripts/interop.sh` gives a case's link flags to its server as well and skips a pairing that does not declare a size (or `unbalancedCommonAddress` where needed) as unsupported.
- `tests/iec101/known-bugs.tsv`: an entry may name a role (`<adapter>@server`) and, for the serial standards, match the link mode (the regex sees the operation followed by ` --link-mode <mode>`). First entry: a balanced digitalpetri station sends user data without initializing its own primary direction (docs/COMPATIBILITY.md).
- The smoke test of an IEC 101 image checks the size lists, a size the image does not list (usage error), and the sizes in the `ready` event.
- IEC 103 generic services and disturbance data (contract 2.3, docs/CONTAINER_CONTRACT.md, "IEC 103 generic services and disturbance data"): the client operations `generic-read` (ASDU 21, INF 244), `generic-write` (ASDU 10, INF 250), `disturbance-list` (TOO 24) and `disturbance-transfer` (the whole procedure of clause 7.4.7); `generic`, `disturbances` and `disturbance` in results; confirmations with causes 40 to 43; the features `genericServices`, `disturbanceList`, `disturbanceData`, `disturbanceDataServer`; `generic` entries and a disturbance record (FAN 7, four tags, two channels of 40 values) in the IEC 103 fixture. Seven cases in `tests/iec103/cases.tsv`, five value checks. Per pinned source: vitusdong implements both as master and slave (its server generates its own samples, so it does not declare `disturbanceDataServer`); Open103 implements the disturbance transfer as master (through a shim, `DisturbanceData` has no return for types 23 to 30); ARIEC103 and go-iecp5 cs103 implement neither (declared unsupported).
- `tests/iec103/known-bugs-consumer.tsv`: six upstream bugs (vitusdong generic INF, ASDU 23 VSQ/FUN/SOF, SOF bits, disturbance procedure; Open103 ASDU 30 NFE) that no pairing of this harness shows, seen on the wire against a consumer's station or control system (docs/COMPATIBILITY.md). `scripts/interop.sh` does not read it; consumers apply it with `known-bugs.tsv`.

### Changed

- `adapters/iec101/digitalpetri/` is EPL-2.0, the library's licence, not GPL-3.0-or-later like the other adapters: digitalpetri/iec60870 has no Secondary License notice, so it cannot be combined with GPL code (AGENTS.md rule 6 names the exception, THIRD_PARTY_NOTICES.md). `licenses/EPL-2.0.txt` added.
- `schemas/capabilities.schema.json`: `cotSizes` and `ioaSizes` added, `linkAddressSizes` and `caSizes` extended to IEC 101 (`linkAddressSizes` 0 for IEC 101 only); an IEC 101 document requires the four size lists and the features `unbalancedMode`, `balancedMode`, `distinctBalancedAddresses` and `unbalancedCommonAddress`.
- `schemas/client-result.schema.json`: the `generic`, `disturbances` and `disturbance` parts of IEC 103 results.
- `fixtures/schema/iec60870-5-103-fixture.schema.json`: the optional `generic` entries and `disturbances` records; `scripts/validate-fixtures.py` checks them (unique GIN, value of the declared type and size; unique FAN, tags in order with the initial states at position 0, channels in ascending ACC with one NOE). `schemaVersion` stays `"1.0"`: the parts are optional, so every fixture of 1.0 is still valid.
- `scripts/interop-iec103.sh`: the value checks of contract 2.3 (generic read and write, disturbance list, transfer and values).
- `scripts/check-versions.sh`: the pins of `iec101-digitalpetri` (version, revision, Maven and JRE digests, gson and slf4j), and that both stages of its Dockerfile pin the same revision.

### Verified

See docs/COMPATIBILITY.md (IEC 60870-5-101 and IEC 60870-5-103). Locally on linux/arm64 (Docker on macOS), 2026-11-11. Nothing ran on amd64 or in CI.

- IEC 101, contract 2.3, three stacks: smoke 48 pass per image and link mode; cross-stack unbalanced 1064 pass, 0 fail, 45 unsupported; balanced 913 pass, 0 fail, 64 known-difference (cbsigma client against digitalpetri server).
- IEC 103, contract 2.3: smoke 53 (vitusdong), 34 each (goiecp5, ariec103, open103); cross-stack 36 pass, 0 fail, 45 known-difference, 39 unsupported. The generic and disturbance-list cases pass only for vitusdong against itself, which is not evidence; no harness server serves the fixture's disturbance values.

---

## v0.3.0

**Date:** 2026-11-11
**Previous release:** v0.2.1

### Added

- Container contract 2.2 (docs/CONTAINER_CONTRACT.md, section IEC 102): IEC 60870-5-102 over the FT 1.2 stream on one TCP connection, port 2401, unbalanced only, the integrated total station (meter) as server and the controlling station as client. Link address and common address (measuring point address) of two octets by default (`--link-address-size`, `--ca-size`; `linkAddressSizes`, `caSizes` in capability documents), `--profile standard|ree` and `--access-key`: with the REE profile a session (C_AC_NA_2, C_FS_NA_2) around every operation, reported as `session: {login, logout}`, and the error `login-refused`. Client operations `connect`, `read-totals` (C_CI_NT_2 absolute / C_CI_NU_2 incremental: `--kind`, `--record`, `--from`, `--to`, `--first`, `--last`), `read-time` (C_TI_NA_2), `read-product` (C_RD_NA_2), and of the REE profile `set-time` (C_CS_TA_2, `--time`) and `read-tariff` (C_TA_VC_2 / C_TA_VM_2, `--register`, `--from`, `--to`). ASDU documents of IEC 102: `record`, the station's local times a and b with `su` and `timeInvalid`, integrated totals with the quality octet, product identification, tariff records; confirmations with the refusal causes 13..18; the server events of IEC 102; the features `integratedTotals`, `readTime`, `setTime`, `productIdentification`, `tariffInformation`, `reeSession`.
- `fixtures/iec102/baseline` (an electricity meter: measuring point address 1, access key 7, a clock that stands still until set, three hourly periods of six integrated totals on record 11 as absolute values and as increments with quality bits set, tariff information of register 134: two records of current and two of stored values) with its own schema, `fixtures/schema/iec60870-5-102-fixture.schema.json`, and rules in `scripts/validate-fixtures.py`; docs/FIXTURES.md, "IEC 102", defines the station behaviour and the causes of refusal.
- Image `iec102-gisce`: gisce/iec870ree v0.14.1 (`993242da`, AGPL-3.0, Python), the controlling station of the Spanish REE profile, client only. The library is fetched by commit and shipped as source; its dependencies (bitstring 3.1.5, six, pytz, python-dateutil) are installed by version and hash; the adapter gives it a TCP transport of its own. Status experimental: there is no open IEC 102 controlled station, so there is no cross-stack run.
- `scripts/smoke-iec102.sh` and `make smoke-iec102`: the contract surface and every operation against `tests/iec102/meter.py`, a scripted meter written in this repository from the contract and the fixture. **It is not an independent implementation of IEC 102**: a pass shows that the image keeps the contract and reads the fixture's values, not that either is right about IEC 102. `tests/iec102/known-bugs.tsv` (empty).
- The client result and capability schemas take the IEC 102 fields.

### Changed

- `make` exports `BASE_REGISTRY` to the scripts (the scripted meter's Python image comes from it, as the base images of the builds do).

### Verified

Locally on linux/arm64 (Docker on macOS), 2026-11-11: smoke of `iec102-gisce` against the scripted meter, 61 pass, 0 fail; its 18 client result documents and its capability document validate against `schemas/`. IEC 101 (both link modes) and IEC 103 smoke unchanged. Nothing ran on amd64 or in CI.

---

## v0.2.1

**Date:** 2026-11-11
**Previous release:** v0.2.0

- Publishes `iec103-goiecp5` (riclolsen/go-iecp5 v0.4.7 cs103, client),
  validated in v0.2.0 but held back then: its LICENSE file says LGPL-3.0,
  its README and file headers GPL-3.0; either allows the GPL-3.0-or-later
  image, and the maintainer decided to publish it. `UNPUBLISHED` is empty.

---

## v0.2.0

**Date:** not released (prepared 2026-11-11)
**Previous release:** v0.1.0

### Added

- Container contract 2.1 (docs/CONTAINER_CONTRACT.md, section IEC 103): IEC 60870-5-103 over the FT 1.2 stream on one TCP connection, port 2401, unbalanced only, one-octet link address, the protection equipment as server and the control system as client. Client operations `connect`, `interrogate` (`--scn`), `clock-sync` (`--time`, `--broadcast`), `command` (`--fun`, `--inf`, `--dco`, `--rii`), `monitor`; confirmations with causes 8, 20 and 21; ASDU documents by function type and information number (`fun`, `inf`, `dpi`, `sin`, `time32`, `values`, `col`, `ascii`, `software`, `scn`, `dco`, `rii`, `data`); server events `interrogation`, `clock-sync`, `command`; `asduTypes` and the features `generalCommand`, `identification`, `measurands`, `broadcastClockSync`, `genericServices`, `disturbanceData` in capability documents. An image may be a client only.
- `fixtures/iec103/baseline` (an overcurrent protection device, function type 160: identification, nine indications, three measurands, three general commands of table 17, 160/16, 17 and 19, with the change of state after an executed command sent with cause 12 as clause 7.4.4 has it) with its own schema, `fixtures/schema/iec60870-5-103-fixture.schema.json`, and rules in `scripts/validate-fixtures.py`.
- IEC 103 images: `iec103-vitusdong` (vitusdong/iec103-dotnet v0.1.0, server and client), `iec103-ariec103` (masarray/ARIEC103 `ec08dd73`, client, on a TCP transport of its own since its serial port fails on a pty), `iec103-open103` (XVincentX/Open103 `887ff5a5`, client, compiled for Linux with a shim), `iec103-goiecp5` (riclolsen/go-iecp5 v0.4.7 cs103, client; built and tested, not published until its licence is settled).
- `scripts/smoke-iec103.sh`, `scripts/interop-iec103.sh`, `tests/iec103/cases.tsv` (9 cases; the commands use pairs of table 17 a client that follows the table sends, and collect the cause 12 report), `tests/iec103/known-bugs.tsv` (6 upstream defects with wire evidence; `ariec103-command-no-rii` and `open103-command-typical-fun` were seen against the server of go-iec60870-5's consumer suite), `make smoke-iec103`, `make interop-iec103`; `scripts/interop.sh` pairs every client with every server by their roles.
- The client result and capability schemas take the IEC 103 fields.

### Changed

- `make publish`, `publish-arch`, `publish-manifest` and `digests` leave out the images of `UNPUBLISHED` (`iec103-goiecp5`).
- `scripts/interop.sh`: when known bugs of both adapters of a pairing match an operation, the client's is counted (its request is the first frame of the exchange).

### Verified

Locally on linux/arm64 (Docker on macOS), 2026-11-11: smoke of the four IEC 103 images and the IEC 103 cross-stack run, counts in docs/COMPATIBILITY.md; the four IEC 103 images also build and answer for linux/amd64 under emulation. IEC 101 smoke unchanged.

---

## v0.1.0

**Date:** 2026-11-11
**Previous release:** none (successor of otfabric/iec104-interop v0.6.0 for the IEC 60870-5 family)

### Added

- Container contract 2.0 (docs/CONTAINER_CONTRACT.md): images `ghcr.io/otfabric/iec60870-5-interop-<standard>-<stack>`, entrypoint `/usr/local/bin/iec60870-5-interop`, readiness file `/run/iec60870-5-interop/ready`, `standard` in capability documents. IEC 104 is contract 1.3 under these names.
- IEC 60870-5-101 over a TCP byte stream (FT 1.2, IEC 60870-5-2 link procedures, no APCI), port 2401, one connection at a time; the link flags `--link-mode unbalanced|balanced`, `--link-address`, `--remote-link-address`, `--link-address-size`, `--cot-size`, `--ca-size`, `--ioa-size`, `--common-address`; the error `link-init-timeout`; C_TS_NA_1 (`fbp`).
- IEC 101 images `iec101-lib60870` (lib60870-C v2.4.1, CS101 master and slave on a pty bridged to TCP inside the driver) and `iec101-cbsigma` (cB-Sigma/iec60870 v0.3.0, package cs101 on the socket), both modes.
- `fixtures/iec101/baseline`, `tests/iec101/cases.tsv` (31 cases), smoke checks of the serial transport (`scripts/smoke-serial.sh`).
- The five IEC 104 adapters of iec104-interop v0.6.0 under `adapters/iec104/`, with their fixtures and case tables under `fixtures/iec104/` and `tests/iec104/`.
- Release workflow (native amd64/arm64 builds and self-tests, multi-arch GHCR images, `manifest.json` with digests), `make publish-arch`, `publish-manifest`, `digests`; Policy workflow.
- `scripts/smoke.sh` and `scripts/interop.sh` take the standard as their first argument and the link mode from `LINK_MODE`; results ids are `<standard>[.<link mode>].<case>`, implementations `<standard>-<stack>`.

### Verified

Locally on linux/arm64 (Docker on macOS), 2026-11-11; counts in docs/COMPATIBILITY.md.

---

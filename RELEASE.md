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

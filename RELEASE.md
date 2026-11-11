# iec60870-5-interop Releases

## How a release is made

1. Write the notes of the version below and set `interop_version` in `versions.yaml` to it, in the same change. `make validate` (and CI) fails when RELEASE.md has no notes for `interop_version`.
2. `make ci` on the commit to release.
3. Tag `vX.Y.Z`. The release workflow of iec104-interop (native amd64 and arm64 builds, self-tests, GHCR push, `manifest.json` with digests) is not carried over yet ([docs/ROADMAP.md](docs/ROADMAP.md)); until it is, nothing is published.

The notes are everything after the heading `## vX.Y.Z` up to the next line
that is exactly `---` (`scripts/release-notes.sh vX.Y.Z` prints it): one
section per version, newest first, each ending with a `---` line.

| What changed | Version |
|--------------|---------|
| Contract or fixture format changed incompatibly | major |
| New standard, operation, behaviour, fixture, adapter or document field | minor |
| Upstream pin, base image or bug fix with unchanged contract | patch |

---

## v0.1.0

**Date:** prepared 2026-11-11, not released
**Previous release:** none (successor of otfabric/iec104-interop v0.6.0 for the IEC 60870-5 family)

### Added

- Container contract 2.0 (docs/CONTAINER_CONTRACT.md): images `ghcr.io/otfabric/iec60870-5-interop-<standard>-<stack>`, entrypoint `/usr/local/bin/iec60870-5-interop`, readiness file `/run/iec60870-5-interop/ready`, `standard` in capability documents. IEC 104 is contract 1.3 under these names.
- IEC 60870-5-101 over a TCP byte stream (FT 1.2, IEC 60870-5-2 link procedures, no APCI), port 2401, one connection at a time; the link flags `--link-mode unbalanced|balanced`, `--link-address`, `--remote-link-address`, `--link-address-size`, `--cot-size`, `--ca-size`, `--ioa-size`, `--common-address`; the error `link-init-timeout`; C_TS_NA_1 (`fbp`).
- IEC 101 images `iec101-lib60870` (lib60870-C v2.4.1, CS101 master and slave on a pty bridged to TCP inside the driver) and `iec101-cbsigma` (cB-Sigma/iec60870 v0.3.0, package cs101 on the socket), both modes.
- `fixtures/iec101/baseline`, `tests/iec101/cases.tsv` (31 cases), smoke checks of the serial transport (`scripts/smoke-serial.sh`).
- The five IEC 104 adapters of iec104-interop v0.6.0 under `adapters/iec104/`, with their fixtures and case tables under `fixtures/iec104/` and `tests/iec104/`.
- `scripts/smoke.sh` and `scripts/interop.sh` take the standard as their first argument and the link mode from `LINK_MODE`; results ids are `<standard>[.<link mode>].<case>`, implementations `<standard>-<stack>`.

### Verified

Locally on linux/arm64 (Docker on macOS), 2026-11-11; counts in docs/COMPATIBILITY.md.

---

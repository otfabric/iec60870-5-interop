# iec60870-5-interop

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/iec60870-5-interop/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/iec60870-5-interop/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/iec60870-5-interop?label=release)](https://github.com/otfabric/iec60870-5-interop/releases)

Independent implementations of the IEC 60870-5 protocol family in
containers, behind one command-line contract, with deterministic fixtures.
A library or tool that implements IEC 101 or IEC 104 tests against these
images; nothing in this repository knows about any such consumer.

**Status: v0.1.0 prepared, not released.** No image has been published.

| Standard | Images (`ghcr.io/otfabric/iec60870-5-interop-<standard>-<stack>`) | Status |
|----------|-------------------------------------------------------------------|--------|
| IEC 60870-5-101 | `iec101-lib60870` (lib60870-C v2.4.1), `iec101-cbsigma` (cB-Sigma/iec60870 v0.3.0) | validated locally: smoke and the cross-stack run, unbalanced and balanced ([COMPATIBILITY](docs/COMPATIBILITY.md)) |
| IEC 60870-5-104 | `iec104-lib60870`, `iec104-openmuc`, `iec104-wendy512`, `iec104-cbsigma`, `iec104-part5` (from iec104-interop 0.6.0) | experimental: built and self-tested here ([COMPATIBILITY](docs/COMPATIBILITY.md)), not yet released from this repository |
| IEC 60870-5-102 | none | survey only ([REFERENCE_STACKS](docs/REFERENCE_STACKS.md)) |
| IEC 60870-5-103 | none | survey only ([REFERENCE_STACKS](docs/REFERENCE_STACKS.md)) |

## How the serial standards are carried

IEC 101 (and later 102 and 103) runs over a serial line. Here the serial
byte stream (FT 1.2 frames, IEC 60870-5-2 link procedures) crosses one TCP
connection unchanged, as through a terminal server. The server is the
controlled station on port 2401; the client is the controlling station. No
IEC 104 APCI is involved. An adapter whose library only talks to a tty
bridges a pseudo terminal to the socket inside the image.

## Quick start

```bash
make images                      # build every image for this machine
make smoke interop               # contract smoke tests and cross-stack self-tests

# One IEC 101 station (balanced) and a controlling station of another stack
docker network create iec101-demo
docker run -d --rm --name station --network iec101-demo \
  ghcr.io/otfabric/iec60870-5-interop-iec101-lib60870:dev server --link-mode balanced
docker run --rm --network iec101-demo ghcr.io/otfabric/iec60870-5-interop-iec101-cbsigma:dev \
  client interrogate --host station --link-mode balanced
docker stop station && docker network rm iec101-demo
```

`docker compose up -d --wait` starts every server on loopback ports
(`compose.yaml`).

## Documents

| Topic | Where |
|-------|-------|
| Container contract 2.0: commands, link flags, documents, exit codes | [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) |
| What each image can do | [docs/CAPABILITIES.md](docs/CAPABILITIES.md) |
| Fixtures and station behaviour | [docs/FIXTURES.md](docs/FIXTURES.md) |
| Self-test results, exact counts | [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) |
| Candidate reference stacks per standard | [docs/REFERENCE_STACKS.md](docs/REFERENCE_STACKS.md) |
| What comes next | [docs/ROADMAP.md](docs/ROADMAP.md) |
| Pins | [versions.yaml](versions.yaml) |
| Licences | [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) |

## Layout

```text
adapters/<standard>/<stack>/   one image each (GPL-3.0-or-later, adapters/LICENSE)
fixtures/<standard>/<name>/    fixture.json, baked into the images of that standard
fixtures/schema/               the fixture schema (all standards)
schemas/                       capability, client result and interop-result-v1 schemas
scripts/                       smoke.sh, interop.sh <standard> ..., results writer, validators
tests/<standard>/              cases.tsv, known-bugs.tsv (IEC 104 also feature- and secure-cases)
reports/                       interop-result-v1 results.json and the documents of the last kept run
```

## Licence

The repository is MIT ([LICENSE](LICENSE)). The adapter programs under
`adapters/` link GPL-3.0 libraries and are GPL-3.0-or-later
([adapters/LICENSE](adapters/LICENSE), [licenses/GPL-3.0.txt](licenses/GPL-3.0.txt)).
A consumer talks to the images over TCP and is not affected by their licence.

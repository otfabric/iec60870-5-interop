# iec60870-5-interop

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/iec60870-5-interop/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/iec60870-5-interop/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/iec60870-5-interop?label=release)](https://github.com/otfabric/iec60870-5-interop/releases)

Independent implementations of the IEC 60870-5 protocol family in
containers, behind one command-line contract, with deterministic fixtures.
A library or tool that implements IEC 101, 102, 103 or 104 tests against these
images; nothing in this repository knows about any such consumer.

**Status: v0.5.0 (IEC 101, 102, 103, 104; contract 2.3; OT Fabric's own images for all four standards, the first IEC 102 station).**

| Standard | Images (`ghcr.io/otfabric/iec60870-5-interop-<standard>-<stack>`) | Status |
|----------|-------------------------------------------------------------------|--------|
| IEC 60870-5-101 | `iec101-lib60870` (lib60870-C v2.4.1), `iec101-cbsigma` (cB-Sigma/iec60870 v0.3.0), `iec101-digitalpetri` (digitalpetri/iec60870 33e52b3, EPL-2.0) | validated locally: smoke and the cross-stack run, unbalanced and balanced, default and four other sets of field sizes (contract 2.3) ([COMPATIBILITY](docs/COMPATIBILITY.md)) |
| IEC 60870-5-104 | `iec104-lib60870`, `iec104-openmuc`, `iec104-wendy512`, `iec104-cbsigma`, `iec104-part5` (from iec104-interop 0.6.0) | experimental: built and self-tested here ([COMPATIBILITY](docs/COMPATIBILITY.md)), not yet released from this repository |
| IEC 60870-5-102 | `iec102-gisce` (gisce/iec870ree v0.14.1, REE profile, client) | experimental: smoke-tested against a scripted meter of this repository, which is not an implementation of IEC 102; there is no open controlled station ([COMPATIBILITY](docs/COMPATIBILITY.md#iec-60870-5-102)) |
| IEC 60870-5-103 | `iec103-vitusdong` (vitusdong/iec103-dotnet v0.1.0, server and client), `iec103-ariec103` (masarray/ARIEC103, client), `iec103-open103` (XVincentX/Open103, client), `iec103-goiecp5` (riclolsen/go-iecp5 v0.4.7 cs103, client) | validated locally, unbalanced, every client against the one server; the server deviates from the standard in every checked reply (known bugs, [COMPATIBILITY](docs/COMPATIBILITY.md#iec-60870-5-103)) |

## How the serial standards are carried

IEC 101, 102 and 103 run over a serial line. Here the serial
byte stream (FT 1.2 frames, IEC 60870-5-2 link procedures) crosses one TCP
connection unchanged, as through a terminal server. The server is the
controlled station on port 2401; the client is the controlling station. No
IEC 104 APCI is involved. An adapter whose library only talks to a tty
bridges a pseudo terminal to the socket inside the image, or gives the
library a transport of its own over the socket (ARIEC103, Open103). IEC 103 is
unbalanced only: the server is the protection equipment, the client the
control system. IEC 102 is unbalanced only too: the server is the meter, the
client the controlling station; every open IEC 102 stack is a client.

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

## OT Fabric's own stack

`ghcr.io/otfabric/iec60870-5-interop-<standard>-otfabric` (`iec101`, `iec102`,
`iec103`, `iec104`) is go-iec60870-5 behind the same contract, both roles, so
you can test your stack against ours the way we test ours against yours. The
IEC 102 image is the first IEC 102 controlled station here. Despite the name
it shares with the reference images, it is not an independent implementation
and not part of a release of this repository, so its tags are go-iec60870-5
versions and `manifest.json` does not list it: see
[own/go-iec60870-5/README.md](own/go-iec60870-5/README.md).

## Documents

| Topic | Where |
|-------|-------|
| Container contract 2.3: commands, link flags, documents, exit codes | [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) |
| What each image can do | [docs/CAPABILITIES.md](docs/CAPABILITIES.md) |
| Fixtures and station behaviour | [docs/FIXTURES.md](docs/FIXTURES.md) |
| Self-test results, exact counts | [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) |
| Candidate reference stacks per standard | [docs/REFERENCE_STACKS.md](docs/REFERENCE_STACKS.md) |
| What comes next | [docs/ROADMAP.md](docs/ROADMAP.md) |
| Pins | [versions.yaml](versions.yaml) |
| Licences | [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) |

## Layout

```text
adapters/<standard>/<stack>/   one image each (GPL-3.0-or-later, adapters/LICENSE; except
                               adapters/iec101/digitalpetri/, EPL-2.0)
fixtures/<standard>/<name>/    fixture.json, baked into the images of that standard
fixtures/schema/               the fixture schemas (IEC 101/104, IEC 102, IEC 103)
schemas/                       capability, client result and interop-result-v1 schemas
scripts/                       smoke.sh, interop.sh <standard> ..., results writer, validators
tests/<standard>/              cases.tsv, known-bugs.tsv (IEC 104 also feature- and secure-cases;
                               IEC 102 the scripted meter meter.py, test-only)
own/go-iec60870-5/             OT Fabric's own stack (MIT, ADR-0012), not a reference: own/go-iec60870-5/README.md
reports/                       interop-result-v1 results.json and the documents of the last kept run
```

## Licence

The repository is MIT ([LICENSE](LICENSE)). The adapter programs under
`adapters/` link GPL-3.0 libraries (or, for IEC 102, import the AGPL-3.0
gisce/iec870ree) and are GPL-3.0-or-later
([adapters/LICENSE](adapters/LICENSE), [licenses/GPL-3.0.txt](licenses/GPL-3.0.txt))
(except `adapters/iec101/digitalpetri/`, EPL-2.0;
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)).
A consumer talks to the images over TCP and is not affected by their licence.

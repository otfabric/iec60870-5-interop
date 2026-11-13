# Testing

What is tested here is the harness itself: the fixtures, the scripts, and every adapter
image against the container contract ([docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md))
and against the other images of its standard. Consumers (go-iec60870-5, iec60870ctl) run
their own suites against the published images; their results are not part of this
repository's tests.

## Locally

Docker is needed for everything but `validate`. Images are built for the host
architecture and tagged `:dev`.

```sh
make ci                        # validate lint images documents: the pre-push gate
make validate                  # fixtures, versions.yaml, RELEASE.md, scripts/test_*.py
make lint                      # shellcheck scripts/*.sh
make images                    # every adapter image (image-<standard>-<adapter> for one)
make smoke                     # contract smoke test of every image
make smoke-iec104-openmuc      # one image; IEC 101: LINK_MODE=balanced
make interop                   # cross-stack self-tests of IEC 101 (both link modes), 103 and 104
make interop-iec101 IEC101_ADAPTERS="lib60870 cbsigma"   # a subset of one standard
make documents                 # smoke and interop keeping every document in reports/, validated against schemas/
```

`make documents` is what CI runs. `scripts/smoke.sh <standard> <adapter>` and
`scripts/interop.sh <standard> <adapter>...` are the scripts behind the targets
(`VERBOSE=1`, `REPORT_DIR=<dir>`). OT Fabric's own image is tested apart:
`make -f own/go-iec60870-5/Makefile images smoke-all interop-all`
([own/go-iec60870-5/README.md](own/go-iec60870-5/README.md)).

## What runs

| Layer | Command | Checks |
|-------|---------|--------|
| Fixtures and manifest | `make validate` | every fixture against its JSON Schema and the cross-field rules (`scripts/validate-fixtures.py`); the Dockerfiles and lock files against `versions.yaml` (`scripts/check-versions.sh`); the notes of `interop_version` in RELEASE.md; unit tests of the result writers (`scripts/test_*.py`) |
| Scripts | `make lint` | shellcheck at severity warning |
| Contract smoke | `make smoke` | per image: the command surface, capabilities, the baked fixture, readiness, the server event stream, exit codes, a clean stop on SIGTERM; IEC 101/102/103 also the FT 1.2 link (`scripts/smoke-serial.sh`) |
| Cross-stack | `make interop` | every client against every server of a standard: each case of `tests/<standard>/cases.tsv` (IEC 104 also `feature-cases.tsv` and `secure-cases.tsv` where a pairing has what they need) must have its expected outcome, and all pairings must return the same result document; then checks on values (interrogation returns the fixture, a command changes what a later interrogation reports) and on connection handling |
| Documents | `make documents` | every capability and result document of the run against `schemas/` |

IEC 102 has no open controlled station: its client (gisce) is smoke-tested against
`tests/iec102/meter.py`, a scripted meter of this repository that is not an
implementation of IEC 60870-5-102, so a pass shows only that the image keeps the
contract ([docs/COMPATIBILITY.md](docs/COMPATIBILITY.md)).

## Known bugs and results

`tests/<standard>/known-bugs.tsv` lists bugs of specific upstream versions, each with
wire evidence from a run; a case they explain is counted as a known bug, never as a pass.
In the serial standards an entry may name a role (`<adapter>@client`,
`<adapter>@server`). `tests/iec103/known-bugs-consumer.tsv` holds bugs seen only against
a consumer's station; the harness applies them but never reports them stale.

Every outcome goes to `results.json` (interop-result-v1, `schemas/`) in `REPORT_DIR`. A
run with no pass, or with a known bug that no longer reproduces, fails. The results of
the reference stacks are summarised in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

## CI

- `ci.yml` (push, pull request, daily, manual): fixtures, versions, script tests and
  shellcheck; vet and race tests of the own stack module (`own/go-iec60870-5`); then on an
  amd64 and an arm64 runner every image built natively, its architecture checked,
  `make capabilities` and `make documents`, with the documents uploaded as an artifact.
- `release.yml` repeats the build and self-tests for a release and publishes what it
  tested, by digest.
- `upstream.yml` (daily, manual) compares every pin of `versions.yaml`
  (`scripts/latest-upstream.py`) and every base image digest with the latest upstream and
  keeps the issue "Upstream releases: pins behind the latest" open while one is behind. It
  never changes a pin.
- `own-go-iec60870-5.yml` (manual) builds and tests OT Fabric's own images.
- `policy.yml` checks the repository against the otfabric/engineering policy.

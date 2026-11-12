# iec60870-5-interop-&lt;standard&gt;-otfabric

[go-iec60870-5](https://github.com/otfabric/go-iec60870-5), OT Fabric's own
IEC 60870-5 stack, behind the [container contract](../../docs/CONTAINER_CONTRACT.md),
so that other stacks can test against it. One image per standard, each with
both roles:

| Image | Standard | Roles |
|-------|----------|-------|
| `ghcr.io/otfabric/iec60870-5-interop-iec101-otfabric` | IEC 60870-5-101, unbalanced and balanced, every field size | server, client |
| `ghcr.io/otfabric/iec60870-5-interop-iec102-otfabric` | IEC 60870-5-102, standard and REE profile | server (integrated total station), client |
| `ghcr.io/otfabric/iec60870-5-interop-iec103-otfabric` | IEC 60870-5-103, with generic services and disturbance data | server (protection equipment), client |
| `ghcr.io/otfabric/iec60870-5-interop-iec104-otfabric` | IEC 60870-5-104, with TLS, IEC 62351-5, files and redundancy | server, client |

Anything that uses an `iec60870-5-interop-<standard>-<adapter>` image can use
the `otfabric` one of that standard in its place. Unlike those, it is not a
reference: its tags are go-iec60870-5 versions, and no harness release or
`manifest.json` contains it. The IEC 102 image is the only IEC 102 controlled
station here: no open implementation of one exists
([REFERENCE_STACKS.md](../../docs/REFERENCE_STACKS.md)).

**For testing only.** The stations are unauthenticated unless `--secure` or
`--tls` is given (IEC 104). Do not expose them to a network you do not
control. Old images stay available, so check the
[go-iec60870-5 advisories](https://github.com/otfabric/go-iec60870-5/security/advisories)
for the version you pull.

**Not an independent implementation.** Each image declares
`upstream.independentEngine: false`, and `results.json` records
`independent: false`. A result against it says nothing independent about
go-iec60870-5 when OT Fabric produces it. A result you publish, naming the
implementation and version, is independent evidence, and we would like to hear
about it.

## Use

```sh
docker run -d --name go-iec101 -p 127.0.0.1:2401:2401 ghcr.io/otfabric/iec60870-5-interop-iec101-otfabric:<tag> server --link-mode balanced
until docker exec go-iec101 test -f /run/iec60870-5-interop/ready; do sleep 0.1; done
```

`<tag>` is a go-iec60870-5 version such as `v0.5.1`, or `latest`, the newest
one published. A rebuild of a version replaces its tag, so pin the digest if
you need an exact build. The digest is in the summary of the
[workflow run](https://github.com/otfabric/iec60870-5-interop/actions/workflows/own-go-iec60870-5.yml),
and `docker run --rm <image> version` prints the versions.

| | |
|---|---|
| Upstream | go-iec60870-5, the latest release when the image was built |
| Licence | MIT; every module's licence is in the image under `/licenses` |
| Adapter name | `otfabric` (the name in documents, as the schema allows) |
| Language | Go |
| Protocol engine | OT Fabric's own |

## Why it is apart from the other adapters

AGENTS.md rule 1: nothing under `adapters/`, `fixtures/`, `scripts/`,
`schemas/`, `tests/` or the top-level `Makefile` knows a library under test,
and CI enforces that. This directory and
`.github/workflows/own-go-iec60870-5.yml` are the one exception. The scripts
take the image through `ADAPTER_IMAGE_<standard>_otfabric`, and `GATE=otfabric`
runs only the pairings with it.

No release loop can form (`otfabric/engineering` ADR-0012). The images are
built by hand, from the latest go-iec60870-5 release, and nothing is
committed: `go.mod` here is only a starting point for local work, and this
repository has no pin of go-iec60870-5 to bump. No release or required check
of go-iec60870-5 needs the images, and go-iec60870-5 commits no pin of them
(its manual Self-interop workflow runs against `:latest`). They are not in a
harness release or `manifest.json`.

## Publishing

Run the **Own stack (go-iec60870-5)** workflow by hand, from `main`. It finds
the latest go-iec60870-5 release and the latest harness release, and prints
them. For each standard it builds the image, runs the smoke test and the
cross-stack self-test against that release's references on both
architectures, then publishes `:<go-iec60870-5 version>` and `:latest`. Run
from a branch, it stops before publishing. Nothing runs it automatically, so
after a go-iec60870-5 release the images stay on the old version until someone
does.

```sh
make -f own/go-iec60870-5/Makefile images smoke-all interop-all    # from the repository root
make -f own/go-iec60870-5/Makefile image smoke interop STANDARD=iec103
```

The module has one main package per standard (`iec101/` .. `iec104/`); the
Dockerfile builds the one of `STANDARD`. Each package carries a copy of its
standard's baseline fixture in `testdata/` for its unit tests (the build
context of the module has no `fixtures/`); keep it in step with
`fixtures/<standard>/baseline`.

## What the images cannot do, and how they differ

Everything each image declares passed the smoke test and the cross-stack
self-test. Features declared false are those the contract reserves for other
standards.

**IEC 101.** The client sends each request with `Client.Send` and watches
every ASDU through the handler, not through the library's request
procedures, which consume the mirrors the result document lists. A connection
that arrives while the previous line is still being taken down waits up to
250 ms before it is refused as a second controlling station.

**IEC 102.**

- The library's station answers C_RD_NA_2, C_TI_NA_2 and C_CI_NT_2/NU_2 itself
  and has no hook for them, so the server events are taken from the frames the
  station sends: an event comes when the controlling station fetches the answer
  (class 2), not when the request arrives, and carries the outcome sent.
- The station's clock is a `time.Time` in UTC, so the SU bit of a C_CS_TA_2 is
  not kept: M_TI_TA_2 reports SU 0 after it.
- The library has no tariff store: the adapter answers C_TA_VC_2 and C_TA_VM_2
  with the library's server and `ree` bodies. The other requests the library
  would serve from its store (C_CI_NA_2 .. C_CI_NS_2, C_SP_NA_2/NB_2) are
  refused with cause 14, as the fixture's station must.
- `server --profile` is validated and has no other effect: the station serves
  the fixture's REE requests under both profiles.

**IEC 103.**

- `interrogate`, `command` and `clock-sync` build their ASDU and send it with
  `Client.Send`: the library's procedures choose their own scan number and
  RII, refuse DCO 0 and 3, and shift the time by the queueing delay, while
  the contract has `--scn`, `--rii`, every DCO and an exact time.
- `Client.Broadcast` returns once the frame is queued and `Client.Close`
  drops a queued frame, so `clock-sync --broadcast` waits until the link has
  written it before closing.
- The station keeps handling for 100 ms after its line closes, and a new
  connection waits up to 300 ms for the previous line to end before it is
  refused.
- Every connection gets the identification (ASDU 5, cause 4) and, since the
  station's server is new per connection, the power-on ASDU 5 (cause 6).

**IEC 104.** go-iec60870-5 takes W independently of K (IEC 104 ed. 1 only
recommends W ≤ 2K/3); the adapter keeps the contract's rule `w <= k` and
answers `--w` above `--k` with exit 2. With `server --redundancy-group`, a
STARTDT that takes over from a started connection leaves the old link started
inside the library: only queued events stop going to it, and they go back to
it when the connection that took over stops. The adapter therefore keeps the
queued event until a connection starts and ignores the requests of such a
connection, so nothing is sent on it except an answer already being sent.
`client redundancy` runs each step itself with two clients, not with
`client.Group`.

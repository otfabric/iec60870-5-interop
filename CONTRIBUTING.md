# Contributing to iec60870-5-interop

## Ground rules

- **Nothing here may know about a library under test.** No imports, no build steps, no expectations, no names from `go-iec60870-5`, `go-iec104` or any other consumer ([AGENTS.md](AGENTS.md) hard rule 1; a future `own/` is the one exception).
- **The contract is the product.** Adapters differ inside; their commands, documents and exit codes are identical. A change to [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) or [docs/FIXTURES.md](docs/FIXTURES.md) lands in every published adapter in the same change.
- **Declare only what is tested.** A capability is `true` when the self-tests exercise it for that adapter.
- **Pin everything.** Upstreams by commit or exact version, base images by digest, all mirrored in [versions.yaml](versions.yaml).

## Setup

Docker (with buildx), `jq`, GNU make and Python 3. Nothing else: compilers, Maven and the JDK run inside the image builds.

```sh
make ci     # validate, lint, build, smoke, cross-stack self-test, document schemas
```

Run `make` for all targets.

## Changing an adapter

1. Edit under `adapters/<adapter>/`.
2. `make image-<adapter> smoke-<adapter>`.
3. `make interop`: the result documents of all pairings must stay identical. If two stacks disagree, find out which one is right by the standard before touching the expectation.
4. If behaviour visible to consumers changed, update the contract or fixture documentation in the same change.

Adapter sources link GPL-3.0 libraries: keep the `SPDX-License-Identifier: GPL-3.0-or-later` header.

## Moving an upstream pin

1. Change the pin in the Dockerfile (and `pom.xml`, or `go.mod` with `go mod tidy`) and in `versions.yaml`; `scripts/check-versions.sh` must pass.
2. `make ci`.
3. Record the new version, and anything it changed, in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md), [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and [RELEASE.md](RELEASE.md).

The rule is the latest upstream release, also when it has a bug: list the bug in `tests/<standard>/known-bugs.tsv` (the matching cases then count as known bugs, not failures) and describe it in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md), rather than pinning an older release. Stay behind only when the bug costs the coverage of a whole stack, and then build the latest release as a candidate image next to the pinned one. When a run reports that a known bug no longer reproduces, remove its entry.

The daily Upstream workflow and the `candidate-*` targets of iec104-interop are not carried over yet ([docs/ROADMAP.md](docs/ROADMAP.md)).

## Registries and retries

CI does not pull from Docker Hub: its anonymous rate limit and its token service fail too often on shared runners. The workflows set `BASE_REGISTRY=mirror.gcr.io/library` (base images, which the Dockerfiles pin by digest, so the mirror cannot change what is built) and `TOOLS_REGISTRY=mirror.gcr.io` (BuildKit, QEMU handlers, shellcheck). Locally both default to Docker Hub.

Every command that talks to a registry runs through `scripts/retry.sh`, which repeats it with a growing delay when the output shows a rate limit, a 5xx or a timeout, and returns any other failure at once. Use it for new registry or download steps instead of an action that cannot retry.

Both architectures are built and self-tested natively, on an amd64 and an arm64 runner; nothing in CI runs under emulation. `make buildx-setup` and `make buildx` remain for checking the other architecture from one machine.

## Adding a client operation or a server behaviour

1. Specify it first: the operation, its flags, when it succeeds and its result fields in the contract; the station behaviour in [docs/FIXTURES.md](docs/FIXTURES.md).
2. Implement it in every published adapter.
3. Add cases to `tests/<standard>/cases.tsv` (IEC 104: for the security flags `tests/iec104/secure-cases.tsv`, for the features that need the fixture `extended` or their own server flags `tests/iec104/feature-cases.tsv`) with the expected outcome, and a value check in `scripts/interop.sh` if the outcome line cannot show what matters.
4. Add the feature to the capability documents and to [docs/CAPABILITIES.md](docs/CAPABILITIES.md).
5. Extend the schemas in `schemas/` if documents gained fields.

Additive changes bump the contract's minor version; anything a consumer could break on bumps the major version.

## Adding a fixture

See [docs/FIXTURES.md](docs/FIXTURES.md#adding-a-fixture).

## Adding an adapter

1. Evaluate the upstream: does it build from a pinned source on both architectures, which roles does it provide, what is its licence, and is its protocol engine its own? Write the answers into [docs/REFERENCE_STACKS.md](docs/REFERENCE_STACKS.md) before writing code.
2. Create `adapters/<standard>/<name>/` with a Dockerfile built from the repository root, following the existing ones: pinned build stage, minimal runtime stage, non-root user, `/fixtures`, `/licenses`, the readiness file, the health check, `ENTRYPOINT ["/usr/local/bin/iec60870-5-interop"]`, and `COPY fixtures/<standard> /fixtures`.
3. Implement the contract. For a role the upstream does not support, the command exits 2 with a message and `roles` says `false`. What the upstream cannot do within a role is a feature declared `false`, and the cases that need it name it in their `needs` column: never fake a behaviour the library does not have.
4. `scripts/smoke.sh <standard> <name>` must pass, then `scripts/interop.sh <standard> <the other adapters> <name>` (IEC 101: with `LINK_MODE=unbalanced` and `LINK_MODE=balanced`). Set its status in versions.yaml: `experimental` until both pass, `validated` once they pass and the counts are in docs/COMPATIBILITY.md.
5. Add it to `IEC101_ADAPTERS` or `IEC104_ADAPTERS` in the Makefile, to the workflows, `versions.yaml`, `scripts/check-versions.sh`, the notices and the documentation tables.

Do not build a shared adapter framework. The adapters share a contract, not code.

## Style

- Shell: `bash`, `set -uo pipefail`, shellcheck-clean (`make lint`). No associative arrays or other bash 4+ features in code paths macOS's `/bin/bash` could run.
- C: C11, `-Wall -Wextra -Werror`.
- Java: 25, `-Xlint:all -Werror`.
- Documentation states what was verified. If something is assumed or untested, say so.

## License

By contributing you agree that your contributions are licensed as the part of the repository they touch: MIT, or GPL-3.0-or-later under `adapters/`.

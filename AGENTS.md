# AGENTS.md — iec60870-5-interop

## What this is

Independent implementations of the IEC 60870-5 family in containers behind one command-line contract (2.3), with deterministic fixtures, used as oracles by libraries and tools that implement the standards. IEC 101: lib60870-C, cB-Sigma/iec60870 and digitalpetri/iec60870, FT 1.2 over one TCP connection. IEC 103 (contract 2.1, unbalanced): vitusdong/iec103-dotnet (server and client), ARIEC103, Open103 and riclolsen/go-iecp5 cs103 (clients). IEC 104: lib60870-C, OpenMUC j60870, wendy512/iec104, cB-Sigma/iec60870, pascaldekloe/part5 (from iec104-interop 0.6.0). IEC 102 (contract 2.2, unbalanced, REE profile): gisce/iec870ree (client only), smoke-tested against the scripted meter tests/iec102/meter.py, which is not an implementation of IEC 102 (no open controlled station exists). Profile: **interoperability**.

## Organisation standards

This repository follows **otfabric/engineering policy 1.2**, profile **interoperability** (`.otfabric/policy.yaml`, mode `enforce`). Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Container commands, link flags, documents, exit codes | docs/CONTAINER_CONTRACT.md (`contract_version` in versions.yaml) |
| What each image can do | docs/CAPABILITIES.md |
| Pins and the status of each implementation (proposed, experimental, validated) | versions.yaml, checked by `make validate` |
| Fixtures | fixtures/<standard>/, docs/FIXTURES.md |
| Expected deviations of the stacks | tests/<standard>/known-bugs.tsv |
| Results | docs/COMPATIBILITY.md, reports/results.json |
| Candidate stacks per standard | docs/REFERENCE_STACKS.md |
| What is next | docs/ROADMAP.md |

## Hard rules

1. Nothing in `adapters/`, `fixtures/`, `scripts/`, `schemas/`, `tests/`, `Makefile` or `compose.yaml` imports or names a library under test (go-iec60870-5, go-iec104). The only exception is a future `own/` directory for the own-stack image (ADR-0012). CI greps for it.
2. Nothing floats: upstream sources and base images stay pinned, and versions.yaml mirrors the pins. `make validate` must pass.
3. Skips and known differences are never counted as passes. `validated` in versions.yaml only with smoke and cross-stack runs that passed and are recorded in docs/COMPATIBILITY.md.
4. An adapter answers an unsupported operation or flag with exit 2, never with a fake success.
5. Contract changes bump `contract_version` and reach every adapter and docs/CONTAINER_CONTRACT.md in the same change.
6. Licences: the repository is MIT, `adapters/` GPL-3.0-or-later except `adapters/iec101/digitalpetri/`, which is EPL-2.0 like the library it links (EPL-2.0 without a Secondary License does not combine with the GPL); every redistributed component in THIRD_PARTY_NOTICES.md and `licenses/`.
7. Upstream defects found on the wire go into tests/<standard>/known-bugs.tsv with evidence from a run; issues upstream are filed by the maintainer, not by an agent.
8. The release tag equals `interop_version`; bump versions.yaml before tagging.

## Commands

| When you change… | Run |
|---|---|
| anything | `make validate lint` |
| an adapter | `make image-<standard>-<stack> smoke-<standard>-<stack>` (IEC 101: also `LINK_MODE=balanced`; IEC 102: against the scripted meter) |
| cross-stack behaviour | `make interop-iec101`, `make interop-iec103`, `make interop-iec104` |
| documents or the contract | `make documents` |

## Git and releases

Branches and PRs. Do not push to `main`, push tags or publish images unless the maintainer asks in the session.

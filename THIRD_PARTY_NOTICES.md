# Third-party notices

The images published from this repository contain third-party software.
This file is copied into every image as `/licenses/THIRD_PARTY_NOTICES.md`,
next to the licence texts.

## What is licensed how

| Part | Licence |
|------|---------|
| Repository tooling, fixtures, schemas, scripts, documentation | MIT ([LICENSE](LICENSE)) |
| `adapters/iec104/lib60870/` (links lib60870-C) | GPL-3.0-or-later |
| `adapters/iec104/openmuc/` (links j60870) | GPL-3.0-or-later |
| `adapters/iec104/wendy512/` (links wendy512/iec104 and go-iecp5) | GPL-3.0-or-later |
| `adapters/iec104/cbsigma/` (links cB-Sigma/iec60870 and the modules it needs) | GPL-3.0-or-later |
| `adapters/iec104/part5/` (links pascaldekloe/part5) | GPL-3.0-or-later |
| `adapters/iec101/lib60870/` (links lib60870-C) | GPL-3.0-or-later |
| `adapters/iec101/cbsigma/` (links cB-Sigma/iec60870 and the modules it needs) | GPL-3.0-or-later |
| `adapters/iec103/common/dotnet/`, `adapters/iec103/vitusdong/`, `adapters/iec103/ariec103/` (link vitusdong/iec103-dotnet, ARIEC103) | GPL-3.0-or-later |
| `adapters/iec103/open103/` (compiles Open103's headers with its shim) | GPL-3.0-or-later |
| `adapters/iec103/goiecp5/` (links riclolsen/go-iecp5) | GPL-3.0-or-later |
| The published reference images | GPL-3.0 (they contain the libraries below) |

The lib60870 and openmuc adapter programs are derivative works of GPL-3.0
libraries. The wendy512 adapter links an Apache-2.0 and an LGPL-3.0 library,
both of which may be combined into a GPL-3.0 work, and the cbsigma adapter
Apache-2.0, BSD-3-Clause and ISC libraries, which may be as well; the part5
adapter links a library dedicated to the public domain (CC0-1.0). All three are
under the same licence as the other two so that one licence covers
`adapters/`. All carry
`SPDX-License-Identifier: GPL-3.0-or-later`. Nothing in this repository is
linked into a consumer: a library that tests against the images talks to them
over TCP and reads their output, and is not affected by their licence.

## Image `iec60870-5-interop-iec104-lib60870`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| lib60870-C (MZ Automation GmbH) | v2.4.1, commit `7a388e3e133999e1ca77ba7521d55d074b7cd2bc` | GPL-3.0 | https://github.com/mz-automation/lib60870 |
| cJSON (Debian `libcjson1`) | Debian trixie | MIT | https://github.com/DaveGamble/cJSON |
| Mbed TLS, compiled into lib60870-C | 3.6.7, release tarball, SHA-256 `a7e8bcbec0e6f761b4af24f25677626b35f762f68eef79c08677a363212d11f6` | Apache-2.0 OR GPL-2.0-or-later; used under Apache-2.0, which may be combined into a GPL-3.0 work | https://github.com/Mbed-TLS/mbedtls |
| Adapter | this repository, `adapters/iec104/lib60870/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence texts in the image: `/licenses/lib60870/COPYING` and
`/licenses/mbedtls/LICENSE`.

lib60870-C is also available from MZ Automation under a commercial licence.
This repository uses the GPL-3.0 release only.

## Image `iec60870-5-interop-iec104-openmuc`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| j60870 (Fraunhofer ISE, OpenMUC) | 1.8.0 | GPL-3.0 | https://www.openmuc.org/iec-60870-5-104/ ; sources on Maven Central: `org.openmuc:j60870:1.8.0:sources` |
| Gson (Google) | 2.14.0 | Apache-2.0 | https://github.com/google/gson |
| Adapter | this repository, `adapters/iec104/openmuc/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |
| Eclipse Temurin JRE 25 and base system | Ubuntu noble | GPL-2.0 with Classpath Exception; various | https://adoptium.net |

Licence text in the image: `/licenses/j60870/COPYING`.

## Image `iec60870-5-interop-iec104-wendy512`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| wendy512/iec104 | v1.0.4, commit `8fb65c83865c94cf37a89d12c9c42dd8c1807cc4` | Apache-2.0 | https://github.com/wendy512/iec104 |
| wendy512/go-iecp5 (a descendant of thinkgos/go-iecp5) | v1.2.6, commit `2269ee79a5e137847bbdc811be71a1064d303560` | LGPL-3.0 | https://github.com/wendy512/go-iecp5 |
| Go standard library and runtime | 1.27.2 | BSD-3-Clause | https://go.dev |
| Adapter | this repository, `adapters/iec104/wendy512/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

Licence texts in the image: `/licenses/iec104/LICENSE`,
`/licenses/go-iecp5/LICENSE` and `/licenses/GPL-3.0.txt`.

go-iecp5 is linked statically into the adapter binary. The LGPL asks that a
recipient can relink the work against a modified library: the adapter's
source and build instructions are in this repository, and
`docker build -f adapters/iec104/wendy512/Dockerfile .` with another
`GOIECP5_VERSION`, or with a `replace` directive in `go.mod`, does that.

## Image `iec60870-5-interop-iec104-cbsigma`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| cB-Sigma/iec60870 | v0.3.0, commit `d0cad7ab6b8905d21dca12f4f7364e788c80ae17` | Apache-2.0 | https://github.com/cB-Sigma/iec60870 |
| cB-Sigma/iotsec | v0.1.0 | Apache-2.0 | https://github.com/cB-Sigma/iotsec |
| cB-Sigma/iotcrypto | v0.1.0 | Apache-2.0; its brainpool, ccm and bigmod packages BSD-3-Clause (see its `NOTICE`) | https://github.com/cB-Sigma/iotcrypto |
| cloudflare/circl | v1.6.3 | BSD-3-Clause | https://github.com/cloudflare/circl |
| decred secp256k1 (`github.com/decred/dcrd/dcrec/secp256k1/v4`) | v4.4.0 | ISC | https://github.com/decred/dcrd |
| golang.org/x/crypto | v0.57.0 | BSD-3-Clause | https://go.googlesource.com/crypto |
| Go standard library and runtime | 1.27.2 | BSD-3-Clause | https://go.dev |
| Adapter | this repository, `adapters/iec104/cbsigma/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

These are the modules linked into the adapter binary; `adapters/iec104/cbsigma/go.mod`
lists more, which the build does not link. The build copies the licence and
notice files of every linked module to `/licenses/<module path>/` (the
Apache-2.0 modules have a `NOTICE` as well) and fails when a module has
none; `/licenses/GPL-3.0.txt` is the adapter's.

## Image `iec60870-5-interop-iec104-part5`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| pascaldekloe/part5 | v0.3.0, commit `1aed6b914f5ce5fd070c0f37d57fc72fe7280a02` | CC0-1.0 (public domain dedication) | https://github.com/pascaldekloe/part5 |
| Go standard library and runtime | 1.27.2 | BSD-3-Clause | https://go.dev |
| Adapter | this repository, `adapters/iec104/part5/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |
| Base system | Debian trixie-slim | various; see `/usr/share/doc/*/copyright` in the image | https://www.debian.org |

part5 has no dependencies. The build copies its `LICENSE` (the CC0 waiver) to
`/licenses/github.com/pascaldekloe/part5/` and fails when it has none;
`/licenses/GPL-3.0.txt` is the adapter's.

## Image `iec60870-5-interop-iec101-lib60870`

The same components as `iec60870-5-interop-iec104-lib60870` (lib60870-C
v2.4.1 at commit `7a388e3e133999e1ca77ba7521d55d074b7cd2bc`, GPL-3.0; Mbed
TLS 3.6.7, compiled into the library, Apache-2.0 OR GPL-2.0-or-later; cJSON
from Debian trixie, MIT), with the IEC 101 driver of this repository,
`adapters/iec101/lib60870/` (GPL-3.0-or-later). Licence texts in the image:
`/licenses/lib60870/COPYING`, `/licenses/mbedtls/LICENSE`.

## Image `iec60870-5-interop-iec101-cbsigma`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| cB-Sigma/iec60870 (package cs101) | v0.3.0, commit `d0cad7ab6b8905d21dca12f4f7364e788c80ae17` | Apache-2.0 | https://github.com/cB-Sigma/iec60870 |
| goburrow/serial | v0.1.0 | MIT | https://github.com/goburrow/serial |
| cB-Sigma/iotsec, cB-Sigma/iotcrypto, golang.org/x/crypto, cloudflare/circl, decred secp256k1 | as in `adapters/iec101/cbsigma/go.mod` | Apache-2.0, BSD-3-Clause, ISC | Go module proxy |
| Adapter | this repository, `adapters/iec101/cbsigma/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |

The modules linked into the adapter binary are those `go list -deps` names
at build time; the Dockerfile copies the licence (and NOTICE) file of every
one of them to `/licenses/<module path>/` and fails when one has none.

## Image `iec60870-5-interop-iec103-vitusdong`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| vitusdong/iec103-dotnet (Iec103) | v0.1.0, commit `38aa54ce52b0451fd073d9865c5a5ea52f975018` | MIT | https://github.com/vitusdong/iec103-dotnet |
| System.IO.Ports, System.Text.Encoding.CodePages and their runtime packages (NuGet) | 8.0.0, content hashes in `adapters/iec103/vitusdong/packages.lock.json` | MIT | https://github.com/dotnet/runtime |
| .NET 8 runtime and base system | `mcr.microsoft.com/dotnet/runtime:8.0-bookworm-slim`, by digest | MIT; various (Debian bookworm) | https://github.com/dotnet/dotnet-docker |
| Adapter | this repository, `adapters/iec103/vitusdong/`, `adapters/iec103/common/dotnet/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |

Licence texts in the image: `/licenses/vitusdong-iec103-dotnet/` (LICENSE and
NOTICE; the NOTICE names the copy of ArIEC103 in the library's tests, which is
not in the image), `/licenses/nuget/<package>/<version>/` and
`/licenses/dotnet-runtime-*`. Microsoft.SourceLink is a build tool of the
library and not in the image; its `.nuspec` with the licence expression (MIT)
is kept with the others.

## Image `iec60870-5-interop-iec103-ariec103`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| masarray/ARIEC103 (ArIEC103.Core, ArIEC103.Master) | commit `ec08dd73991be646864aef55e6a5538b78129db8` (v1.2.31-3, Directory.Build.props 1.3.0) | Apache-2.0 | https://github.com/masarray/ARIEC103 |
| System.IO.Ports and its runtime packages (NuGet) | 8.0.0, content hashes in `adapters/iec103/ariec103/packages.lock.json` | MIT | https://github.com/dotnet/runtime |
| .NET 8 runtime and base system | `mcr.microsoft.com/dotnet/runtime:8.0-bookworm-slim`, by digest | MIT; various (Debian bookworm) | https://github.com/dotnet/dotnet-docker |
| Adapter | this repository, `adapters/iec103/ariec103/`, `adapters/iec103/common/dotnet/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |

Licence texts in the image: `/licenses/masarray-ARIEC103/` (LICENSE, NOTICE,
THIRD_PARTY_NOTICES.md), `/licenses/nuget/` and `/licenses/dotnet-runtime-*`.

## Image `iec60870-5-interop-iec103-open103`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| XVincentX/Open103 (Open103 headers, compiled into `libopen103.so`) | commit `887ff5a5d909e9a23f94dafa2a5c4bdbb20f30ee` | Apache-2.0 | https://github.com/XVincentX/Open103 |
| .NET 8 runtime and base system | `mcr.microsoft.com/dotnet/runtime:8.0-bookworm-slim`, by digest | MIT; various (Debian bookworm, including libstdc++) | https://github.com/dotnet/dotnet-docker |
| Adapter and Linux shim | this repository, `adapters/iec103/open103/`, `adapters/iec103/common/dotnet/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |

Licence texts in the image: `/licenses/XVincentX-Open103/LICENSE` and
`/licenses/dotnet-runtime-*`.

## Image `iec60870-5-interop-iec103-goiecp5`

| Component | Version | Licence | Source |
|-----------|---------|---------|--------|
| riclolsen/go-iecp5 (packages cs103, cs101, asdu, clog) | v0.4.7, commit `fbe6719d9acc9b5d90eed3b9468e7937b8ed4711` | LGPL-3.0 by its LICENSE file; its README and the cs103 file headers say GPL-3.0 | https://github.com/riclolsen/go-iecp5 |
| go.bug.st/serial, creack/goselect, golang.org/x/sys | as in `adapters/iec103/goiecp5/go.mod` | BSD-3-Clause | Go module proxy |
| Adapter | this repository, `adapters/iec103/goiecp5/` | GPL-3.0-or-later | https://github.com/otfabric/iec60870-5-interop |

Either reading of go-iecp5's licence allows it in a GPL-3.0-or-later work; the
image is still not published until the maintainer has settled which one
applies (versions.yaml). The licence files of the linked modules are copied as
for the other Go adapters; `/licenses/GPL-3.0.txt` has the GPL text.

## Corresponding source

For every published image the complete corresponding source is:

1. this repository at the release tag named by the image's
   `org.opencontainers.image.version` label and by `print-capabilities`
   (`adapterVersion`), which contains the adapter source and the build
   instructions (the Dockerfile); and
2. the upstream sources at exactly the versions pinned by that Dockerfile,
   `adapters/iec104/openmuc/pom.xml`, the `go.mod` of `adapters/iec104/wendy512`, `adapters/iec104/cbsigma`,
   `adapters/iec104/part5`, `adapters/iec101/cbsigma` and `adapters/iec103/goiecp5`, the
   `packages.lock.json` of the .NET adapters of `adapters/iec103/`, recorded in [versions.yaml](versions.yaml) and
   in the `manifest.json` of the release.

`docker build -f adapters/<standard>/<adapter>/Dockerfile .` at the release tag
reproduces the image from those sources.

Each GitHub release of this repository records the upstream revisions and
image digests it was built from.

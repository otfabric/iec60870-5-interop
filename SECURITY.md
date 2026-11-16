# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `iec60870-5-interop`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/iec60870-5-interop/security) tab → **Advisories** → **Report a vulnerability**), with a description, steps to reproduce and the affected image tag or digest.

Vulnerabilities in the bundled implementations themselves (lib60870-C, cB-Sigma/iec60870 and its modules, digitalpetri/iec60870, gisce/iec870ree, vitusdong/iec103-dotnet, riclolsen/go-iecp5, masarray/ARIEC103, XVincentX/Open103, OpenMUC j60870, wendy512/iec104 and wendy512/go-iecp5, pascaldekloe/part5; [versions.yaml](versions.yaml) names the repository of each) belong upstream; report them there, and tell us so that the pin can be moved.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | No; pull the latest images |

## Security Considerations

### These are test fixtures, not products

The images run **unauthenticated, unencrypted IEC 60870-5-101 and -104 stations** that accept interrogations and commands from anyone who can reach them, and clients that send commands to whatever address they are given. That is their purpose.

- **Never attach a server to a plant, control or other production network**
- **Never point a client at a real station**: `command`, `clock-sync` and the other operations act on what they are sent to
- `compose.yaml` publishes ports on `127.0.0.1` only. Keep it that way; the self-tests use private Docker networks and publish nothing
- There is no TLS, and none is planned beyond what a future fixture needs to test a consumer's TLS support
- The IEC 104 cbsigma image has IEC 62351-5 application-layer security (`--secure`) so that a consumer can test its own. The self-tests make throw-away key material with openssl, valid for two days; never mount real station keys into an image

### Image hardening

- Processes run as a non-root user (uid 10001)
- Base images are pinned by digest; upstream sources by commit or exact version ([versions.yaml](versions.yaml))
- The runtime images contain the adapter, its runtime libraries and the fixtures; build tools stay in the build stages
- The images are rebuilt, not patched: a base image or upstream fix arrives with a new release

### Supply chain

- Upstream sources are fetched during the image build at pinned versions ([versions.yaml](versions.yaml) records each one's `published` and the hash of its source):
  - lib60870-C, digitalpetri/iec60870, gisce/iec870ree, vitusdong/iec103-dotnet, ARIEC103 and Open103 from GitHub with git, at the pinned commit, which the build checks (`git rev-parse HEAD`); `source_sha256` records the SHA-256 of the GitHub archive of that commit
  - cB-Sigma/iec60870, riclolsen/go-iecp5, wendy512/iec104 and wendy512/go-iecp5, pascaldekloe/part5 and the modules they need from the Go module proxy, verified against `go.sum` (`go mod verify`; `module_sum`)
  - j60870 and Gson from Maven Central by version; `artifact_sha256` and `gson_sha256` record the SHA-256 of the jars, which the build does not check yet
  - the Python packages of gisce from PyPI with hash-pinned requirements (`pip --require-hashes`), the NuGet packages of the IEC 103 adapters with `packages.lock.json` (`dotnet restore --locked-mode`)
- `scripts/check-versions.sh` (CI and the release workflow) fails when `versions.yaml` disagrees with the Dockerfiles, `go.mod`, `go.sum`, `pom.xml`, the published images of the Makefile or the tool images
- Base images are pinned by digest. CI and the release workflow pull them from `mirror.gcr.io/library`, a mirror of Docker Hub's official images, because anonymous Docker Hub pulls are rate limited; the digest makes the source irrelevant to the content (`BASE_REGISTRY`, default `docker.io/library`). The tooling images (BuildKit, QEMU handlers, shellcheck, Python) come from the same mirror (`TOOLS_REGISTRY`), pinned by version and digest (`versions.yaml` `tooling`); the Python packages of the validators are hash-pinned in `scripts/requirements.txt`
- CI runs govulncheck, at the version of `versions.yaml` and with the Go of the golang base image, on the Go adapters and the own-stack adapters
- Consumers should pin images by digest, taken from the `manifest.json` of a release

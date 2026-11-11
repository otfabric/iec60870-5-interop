# iec60870-5-interop IEC 60870-5-103 adapter for riclolsen/go-iecp5 (package cs103).
#
# Build from the repository root:
#   docker build -f adapters/iec103/goiecp5/Dockerfile -t iec60870-5-interop-iec103-goiecp5:dev .
#
# cs103 is an IEC 103 primary station (control system, master) only, with a
# TCP transport for the FT 1.2 stream (cs101.TransportTCPClient), so the image
# is a client and needs no pseudo terminal. The upstream LICENSE file is
# LGPL-3.0; its README and the cs103 file headers say GPL-3.0. The adapter is
# GPL-3.0-or-later and links the module statically, which satisfies either;
# both licence texts are copied to /licenses. go.bug.st/serial (BSD-3-Clause)
# and its dependencies are linked through cs101. See THIRD_PARTY_NOTICES.md.
# The upstream pin is below, in go.mod (with its checksum in go.sum) and in
# versions.yaml.

ARG BASE_REGISTRY=docker.io/library
ARG GOLANG_DIGEST=sha256:e58d6f83b3416618d8bcac2b3dde1b7f7e3c4a77d25e88637f8bbae81536c48d
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

# ---------------------------------------------------------------------------
# Stage 1: build the adapter (the upstream module linked in)
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/golang:1.27.2-trixie@${GOLANG_DIGEST} AS build

ARG GOIECP5_VERSION=v0.4.7
ARG ADAPTER_VERSION=dev

ENV CGO_ENABLED=0 GOFLAGS=-mod=mod
WORKDIR /src
COPY adapters/iec103/goiecp5/go.mod adapters/iec103/goiecp5/go.sum ./

RUN if [ "$(go list -m -f '{{.Version}}' github.com/riclolsen/go-iecp5)" = "${GOIECP5_VERSION}" ]; then \
         GOFLAGS=-mod=readonly go mod download && go mod verify; \
       else \
         go get "github.com/riclolsen/go-iecp5@${GOIECP5_VERSION}"; \
       fi

COPY adapters/iec103/goiecp5/*.go ./
RUN test -z "$(gofmt -l .)" \
    && go vet ./... \
    && go build -trimpath -ldflags "-s -w \
         -X main.adapterVersion=${ADAPTER_VERSION} \
         -X main.upstreamVersion=$(go list -m -f '{{.Version}}' github.com/riclolsen/go-iecp5)" \
         -o /out/iec60870-5-interop . \
    && for m in $(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' . | sort -u); do \
         dir="$(go list -m -f '{{.Dir}}' "$m")" && mkdir -p "/out/licenses/$m" \
         && cp "$dir"/LICENSE* "$dir"/NOTICE* "$dir"/COPYING* "/out/licenses/$m/" 2>/dev/null; \
         ls "/out/licenses/$m" | grep -q . || { echo "no licence in $m"; exit 1; }; \
       done

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/iec60870-5-interop \
    && chown interop /run/iec60870-5-interop

COPY --from=build /out/iec60870-5-interop /usr/local/bin/iec60870-5-interop
COPY --from=build /out/licenses /licenses
COPY licenses/GPL-3.0.txt /licenses/GPL-3.0.txt
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY fixtures/iec103 /fixtures

LABEL org.opencontainers.image.title="iec60870-5-interop-iec103-goiecp5" \
      org.opencontainers.image.description="IEC 60870-5-103 control system (primary station) built on riclolsen/go-iecp5 cs103 (FT 1.2 over TCP)" \
      org.opencontainers.image.source="https://github.com/otfabric/iec60870-5-interop" \
      org.opencontainers.image.licenses="GPL-3.0-or-later" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

USER interop

ENTRYPOINT ["/usr/local/bin/iec60870-5-interop"]
CMD ["print-capabilities"]

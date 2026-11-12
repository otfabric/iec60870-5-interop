# SPDX-License-Identifier: MIT
#
# iec60870-5-interop-<standard>-otfabric: OT Fabric's own IEC 60870-5 stack
# (go-iec60870-5) behind the container contract, one image per standard.
# Not an independent implementation: see README.md.
#
# Build from the repository root:
#   make -f own/go-iec60870-5/Makefile image STANDARD=iec101
#
# STANDARD is iec101, iec102, iec103 or iec104: the main package
# own/go-iec60870-5/<standard> and the fixtures fixtures/<standard>.
#
# The build takes the go-iec60870-5 release as UPSTREAM_VERSION (the released
# git tag, which is the Go module version) and its commit as UPSTREAM_SHA.
# There is no pin in this repository: the workflow passes the latest release.
# A build never uses main. go.mod and go.sum here are only a starting point for
# local work.

ARG BASE_REGISTRY=docker.io/library
# golang:1.27.2-trixie and debian:trixie-slim, multi-arch indexes
# (linux/amd64 and linux/arm64), as in adapters/iec101/cbsigma/Dockerfile.
ARG GOLANG_DIGEST=sha256:e58d6f83b3416618d8bcac2b3dde1b7f7e3c4a77d25e88637f8bbae81536c48d
ARG DEBIAN_DIGEST=sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

FROM ${BASE_REGISTRY}/golang:1.27.2-trixie@${GOLANG_DIGEST} AS build

ARG STANDARD
ARG UPSTREAM_VERSION
ARG UPSTREAM_SHA
ARG ADAPTER_VERSION=dev
ARG GOPROXY=https://proxy.golang.org

ENV CGO_ENABLED=0 GOPROXY=${GOPROXY} GOSUMDB=sum.golang.org GOTOOLCHAIN=local
WORKDIR /src
COPY own/go-iec60870-5/go.mod own/go-iec60870-5/go.sum ./

RUN set -eu; \
    case "${STANDARD}" in iec101|iec102|iec103|iec104) ;; *) echo "STANDARD must be iec101, iec102, iec103 or iec104" >&2; exit 1 ;; esac; \
    test -n "${UPSTREAM_VERSION}" || { echo "UPSTREAM_VERSION is required" >&2; exit 1; }; \
    echo "${UPSTREAM_SHA}" | grep -Eq '^[0-9a-f]{40}$'; \
    go mod edit -require="github.com/otfabric/go-iec60870-5@${UPSTREAM_VERSION}"; \
    go mod download github.com/otfabric/go-iec60870-5; \
    origin="$(go mod download -json "github.com/otfabric/go-iec60870-5@${UPSTREAM_VERSION}" \
        | sed -nE 's/^[[:space:]]*"Hash": "([0-9a-f]{40})".*/\1/p')"; \
    if [ -n "${origin}" ] && [ "${origin}" != "${UPSTREAM_SHA}" ]; then \
        echo "go-iec60870-5 ${UPSTREAM_VERSION} is commit ${origin}, not ${UPSTREAM_SHA}" >&2; exit 1; \
    fi

COPY own/go-iec60870-5/ ./
RUN set -eu; \
    go mod edit -require="github.com/otfabric/go-iec60870-5@${UPSTREAM_VERSION}"; \
    go mod tidy; \
    built="$(go list -m -f '{{.Version}}' github.com/otfabric/go-iec60870-5)"; \
    test "${built}" = "${UPSTREAM_VERSION}" || { echo "go.mod has go-iec60870-5 ${built}, want ${UPSTREAM_VERSION}" >&2; exit 1; }; \
    go mod verify; \
    test -z "$(gofmt -l "${STANDARD}")"; \
    go vet "./${STANDARD}"; \
    go test "./${STANDARD}"; \
    go build -trimpath -ldflags "-s -w \
        -X main.adapterVersion=${ADAPTER_VERSION} \
        -X main.upstreamVersion=${UPSTREAM_VERSION} \
        -X main.upstreamRevision=${UPSTREAM_SHA}" \
        -o /out/iec60870-5-interop "./${STANDARD}"; \
    mkdir -p /out/licenses/go; \
    cp "$(go env GOROOT)/LICENSE" /out/licenses/go/LICENSE; \
    go version -m /out/iec60870-5-interop | awk '$1 == "dep" { print $2, $3 }' > /out/licenses/BUNDLED-MODULES.txt; \
    test -s /out/licenses/BUNDLED-MODULES.txt; \
    while read -r module modversion; do \
        dir="$(go list -m -f '{{.Dir}}' "${module}")"; \
        mkdir -p "/out/licenses/${module}"; \
        for file in "${dir}"/LICENSE* "${dir}"/LICENCE* "${dir}"/COPYING* "${dir}"/NOTICE*; do \
            if [ -f "${file}" ]; then cp "${file}" "/out/licenses/${module}/"; fi; \
        done; \
        test -n "$(ls "/out/licenses/${module}")" || { echo "no licence file in ${module} ${modversion}" >&2; exit 1; }; \
    done < /out/licenses/BUNDLED-MODULES.txt

FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST} AS runtime

ARG STANDARD
ARG ADAPTER_VERSION=dev
ARG UPSTREAM_VERSION
# 2404 for IEC 104, 2401 for the serial standards (FT 1.2 over TCP).
ARG PORT=2401

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/iec60870-5-interop \
    && chown interop /run/iec60870-5-interop

COPY --from=build /out/iec60870-5-interop /usr/local/bin/iec60870-5-interop
COPY --from=build /out/licenses /licenses
COPY LICENSE /licenses/iec60870-5-interop/LICENSE
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY fixtures/${STANDARD} /fixtures

LABEL org.opencontainers.image.title="iec60870-5-interop-${STANDARD}-otfabric" \
      org.opencontainers.image.description="OT Fabric go-iec60870-5 (${STANDARD}) behind the iec60870-5-interop container contract, for testing. Not an independent implementation." \
      org.opencontainers.image.source="https://github.com/otfabric/iec60870-5-interop" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${UPSTREAM_VERSION}"

USER interop
EXPOSE ${PORT}

HEALTHCHECK --interval=2s --timeout=2s --start-period=10s --retries=15 \
  CMD test -f /run/iec60870-5-interop/ready

ENTRYPOINT ["/usr/local/bin/iec60870-5-interop"]
CMD ["server"]

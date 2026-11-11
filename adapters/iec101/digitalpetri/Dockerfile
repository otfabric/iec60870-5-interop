# iec60870-5-interop IEC 60870-5-101 adapter for digitalpetri/iec60870.
#
# Build from the repository root:
#   docker build -f adapters/iec101/digitalpetri/Dockerfile -t iec60870-5-interop-iec101-digitalpetri:dev .
#
# digitalpetri/iec60870 runs IEC 101 over TCP itself (TcpIec101Client and
# TcpIec101Server: its FT 1.2 link layer on a Netty connection), so the FT 1.2
# stream of the contract is the library's own, with no bridge.
#
# The library is EPL-2.0 without a Secondary License notice, which does not
# combine with the GPL: this adapter and its image are EPL-2.0, unlike the
# other adapters (THIRD_PARTY_NOTICES.md). It is not published to Maven
# Central, so it is built here from the pinned commit; its Maven dependencies
# (Netty, SLF4J, JSpecify, jOOU) are the versions its pom.xml pins at that
# commit. Upstream pins are mirrored in versions.yaml.

# Where the base images come from. They are pinned by digest, so a mirror of
# Docker Hub serves exactly the same images; CI uses one to stay clear of
# Docker Hub's pull rate limit (see the Makefile).
ARG BASE_REGISTRY=docker.io/library
ARG MAVEN_DIGEST=sha256:93b8a14ea2f412782e4e842651273b4d903e35cc496284f178fbbe2d67d00976
ARG JRE_DIGEST=sha256:d9a39a23634650173f1e2bbc176227af9728587ecf0f4b62d53e9355cd7a19ab

# ---------------------------------------------------------------------------
# Stage 1: digitalpetri/iec60870 from a pinned commit, then the adapter jar
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/maven:3.9-eclipse-temurin-25@${MAVEN_DIGEST} AS build

ARG DIGITALPETRI_REPO=https://github.com/digitalpetri/iec60870.git
# No release yet: the version is the abbreviated commit, as for print-capabilities.
ARG DIGITALPETRI_VERSION=33e52b3
ARG DIGITALPETRI_SHA=33e52b360c0fa9bc6099eb0b0aa19eed1eff9ad0

# Fetched by commit, so the pin is the hash and not a branch that moves.
WORKDIR /src/iec60870
RUN git init -q . \
    && git remote add origin "${DIGITALPETRI_REPO}" \
    && git fetch -q --depth 1 origin "${DIGITALPETRI_SHA}" \
    && git checkout -q --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${DIGITALPETRI_SHA}"

# The modules of 101 over TCP and what they need; upstream's tests, formatting
# and enforcer checks are its own CI's, not this build's.
RUN mvn -B -q -pl iec60870-tcp -am install \
      -DskipTests -Dmaven.test.skip=true -Dspotless.check.skip=true -Denforcer.skip=true

WORKDIR /build
COPY adapters/iec101/digitalpetri/pom.xml .
RUN mvn -B -q dependency:go-offline

COPY adapters/iec101/digitalpetri/src ./src
RUN mvn -B -q -o package \
    && mvn -B -q -o dependency:copy-dependencies -DincludeScope=runtime -DoutputDirectory=/build/deps

# Every redistributed jar's own licence and notice files, by artifact.
RUN mkdir -p /out/licenses/digitalpetri-iec60870 \
    && cp /src/iec60870/LICENSE.md /out/licenses/digitalpetri-iec60870/LICENSE.md \
    && for jar in /build/deps/*.jar; do \
         name="$(basename "${jar}" .jar)"; \
         case "${name}" in iec60870-*) continue ;; esac; \
         mkdir -p "/tmp/x/${name}" "/out/licenses/${name}" \
         && (cd "/tmp/x/${name}" && jar xf "${jar}" META-INF) \
         && find "/tmp/x/${name}/META-INF" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'NOTICE*' \) \
              -exec cp {} "/out/licenses/${name}/" \; ; \
       done \
    && ls /build/deps > /out/licenses/JARS.txt

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/eclipse-temurin:25-jre-noble@${JRE_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev
ARG DIGITALPETRI_VERSION=33e52b3
ARG DIGITALPETRI_SHA=33e52b360c0fa9bc6099eb0b0aa19eed1eff9ad0

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/iec60870-5-interop /app \
    && chown interop /run/iec60870-5-interop \
    && printf '%s\n' '#!/bin/sh' \
       'exec java -XX:+UseSerialGC -XX:TieredStopAtLevel=1 -Xshare:auto -jar /app/iec60870-5-interop-iec101-digitalpetri.jar "$@"' \
       > /usr/local/bin/iec60870-5-interop \
    && chmod +x /usr/local/bin/iec60870-5-interop \
    && cp /usr/share/common-licenses/Apache-2.0 /licenses-apache-2.0.txt

COPY --from=build /build/target/iec60870-5-interop-iec101-digitalpetri.jar /app/iec60870-5-interop-iec101-digitalpetri.jar
COPY --from=build /out/licenses /licenses
COPY licenses/EPL-2.0.txt /licenses/EPL-2.0.txt
# Netty, Gson, JSpecify and Error Prone's annotations are Apache-2.0 and their
# jars carry no licence text: the base system's copy of it.
RUN mv /licenses-apache-2.0.txt /licenses/Apache-2.0.txt
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY fixtures/iec101 /fixtures

ENV ADAPTER_VERSION=${ADAPTER_VERSION} \
    UPSTREAM_VERSION=${DIGITALPETRI_VERSION} \
    UPSTREAM_REVISION=${DIGITALPETRI_SHA}

LABEL org.opencontainers.image.title="iec60870-5-interop-iec101-digitalpetri" \
      org.opencontainers.image.description="IEC 60870-5-101 controlling and controlled station built on digitalpetri/iec60870 (FT 1.2 over TCP)" \
      org.opencontainers.image.source="https://github.com/otfabric/iec60870-5-interop" \
      org.opencontainers.image.licenses="EPL-2.0" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

USER interop
EXPOSE 2401

HEALTHCHECK --interval=2s --timeout=2s --start-period=20s --retries=15 \
  CMD test -f /run/iec60870-5-interop/ready

ENTRYPOINT ["/usr/local/bin/iec60870-5-interop"]
CMD ["server"]

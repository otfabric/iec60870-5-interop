# iec60870-5-interop IEC 60870-5-103 adapter for masarray/ARIEC103 (client only).
#
# Build from the repository root:
#   docker build -f adapters/iec103/ariec103/Dockerfile -t iec60870-5-interop-iec103-ariec103:dev .
#
# masarray/ARIEC103 (Apache-2.0) is a .NET 8 IEC 103 master tester
# (Iec103MasterSession) whose serial transport fails on a pseudo terminal
# (System.IO.Ports RtsEnable), so the driver gives it a TCP IByteTransport:
# ArIEC103's own transport interface, the FT 1.2 stream unchanged. The driver
# (Program.cs and adapters/iec103/common/dotnet) is GPL-3.0-or-later like the
# other adapters.
# The NuGet packages are pinned by packages.lock.json (versions and content
# hashes, restored with --locked-mode); their licences (or, for the
# SourceLink build tools that are not in the image, the .nuspec with the
# licence expression), the library's and the .NET runtime's are in
# /licenses. See THIRD_PARTY_NOTICES.md. The upstream pin is mirrored in
# versions.yaml. ARIEC103_VERSION is the name of the commit (git describe;
# Directory.Build.props says 1.3.0).

ARG SDK_DIGEST=sha256:1b955f179b02df4f0eb5fadedaa898cbe44d620a64aab52eec72c8e91d1355b5
ARG RUNTIME_DIGEST=sha256:21b0f8ba67bef2f121781d17a8e21956aeaa2d8da8e4b5db0da1ea8f91e2b6a1

# ---------------------------------------------------------------------------
# Stage 1: the library from a pinned commit, and the driver
# ---------------------------------------------------------------------------
FROM mcr.microsoft.com/dotnet/sdk:8.0-bookworm-slim@${SDK_DIGEST} AS build

ARG ARIEC103_REPO=https://github.com/masarray/ARIEC103.git
ARG ARIEC103_VERSION=v1.2.31-3-gec08dd7
ARG ARIEC103_SHA=ec08dd73991be646864aef55e6a5538b78129db8
ARG ADAPTER_VERSION=dev

ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1

RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*

# Fetched by commit, so the pin is the hash and not a tag that could move.
WORKDIR /src/upstream
RUN git init -q . \
    && git remote add origin "${ARIEC103_REPO}" \
    && git fetch -q --depth 1 origin "${ARIEC103_SHA}" \
    && git checkout -q --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${ARIEC103_SHA}"

COPY adapters/iec103/common/dotnet/*.cs /src/common/
COPY adapters/iec103/ariec103/Adapter.csproj adapters/iec103/ariec103/packages.lock.json /src/adapter/
WORKDIR /src/adapter
RUN dotnet restore --locked-mode
COPY adapters/iec103/ariec103/Program.cs ./
RUN dotnet publish -c Release --no-restore -o /out/app \
      -p:AdapterVersion="${ADAPTER_VERSION}" -p:UpstreamVersion="${ARIEC103_VERSION}" -p:UpstreamRevision="${ARIEC103_SHA}" \
    && mkdir -p /out/licenses/masarray-ARIEC103 \
    && cp /src/upstream/LICENSE /src/upstream/NOTICE /src/upstream/THIRD_PARTY_NOTICES.md /out/licenses/masarray-ARIEC103/ \
    && for d in "${HOME}"/.nuget/packages/*/*/; do \
         p="$(basename "$(dirname "$d")")/$(basename "$d")"; \
         mkdir -p "/out/licenses/nuget/$p"; \
         cp "$d"LICENSE* "$d"license* "$d"THIRD-PARTY-NOTICES* "$d"*.nuspec "/out/licenses/nuget/$p/" 2>/dev/null; \
         ls "/out/licenses/nuget/$p" | grep -q . || { echo "no licence file in $p"; exit 1; }; \
       done

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM mcr.microsoft.com/dotnet/runtime:8.0-bookworm-slim@${RUNTIME_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/iec60870-5-interop \
    && chown interop /run/iec60870-5-interop \
    && printf '#!/bin/sh\nexec /usr/bin/dotnet /opt/adapter/iec60870-5-interop.dll "$@"\n' > /usr/local/bin/iec60870-5-interop \
    && chmod 755 /usr/local/bin/iec60870-5-interop

COPY --from=build /out/app /opt/adapter
COPY --from=build /out/licenses /licenses
RUN cp /usr/share/dotnet/LICENSE.txt /licenses/dotnet-runtime-LICENSE.txt \
    && cp /usr/share/dotnet/ThirdPartyNotices.txt /licenses/dotnet-runtime-ThirdPartyNotices.txt
COPY licenses/GPL-3.0.txt /licenses/GPL-3.0.txt
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY fixtures/iec103 /fixtures

LABEL org.opencontainers.image.title="iec60870-5-interop-iec103-ariec103" \
      org.opencontainers.image.description="IEC 60870-5-103 control system built on masarray/ARIEC103 (FT 1.2 over TCP)" \
      org.opencontainers.image.source="https://github.com/otfabric/iec60870-5-interop" \
      org.opencontainers.image.licenses="GPL-3.0-or-later" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1
USER interop

ENTRYPOINT ["/usr/local/bin/iec60870-5-interop"]
CMD ["print-capabilities"]

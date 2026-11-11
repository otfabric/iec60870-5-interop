# iec60870-5-interop IEC 60870-5-103 adapter for XVincentX/Open103 (client only).
#
# Build from the repository root:
#   docker build -f adapters/iec103/open103/Dockerfile -t iec60870-5-interop-iec103-open103:dev .
#
# Open103 (Apache-2.0) is a header-only C++ IEC 103 master written for
# Windows. Its headers are compiled for Linux unchanged: shim/linux.h stands
# in for the Windows names they use (OutputDebugStringA, localtime_s, the
# Windows-only gettimeofday.h) and for Logger::ToString, which Open103 calls
# but never defines; shim/shim.cpp is the CommunicationPort over TCP and a C
# interface. Program.cs drives it through P/Invoke with the shared .NET code
# of adapters/iec103/common/dotnet. The driver is GPL-3.0-or-later like the
# other adapters; Open103's licence and the .NET runtime's are in /licenses.
# See THIRD_PARTY_NOTICES.md. The upstream pin is mirrored in versions.yaml.
# The shared library is built on the Debian release of the runtime image.

ARG SDK_DIGEST=sha256:1b955f179b02df4f0eb5fadedaa898cbe44d620a64aab52eec72c8e91d1355b5
ARG RUNTIME_DIGEST=sha256:21b0f8ba67bef2f121781d17a8e21956aeaa2d8da8e4b5db0da1ea8f91e2b6a1

# ---------------------------------------------------------------------------
# Stage 1: Open103 from a pinned commit, the shim and the driver
# ---------------------------------------------------------------------------
FROM mcr.microsoft.com/dotnet/sdk:8.0-bookworm-slim@${SDK_DIGEST} AS build

ARG OPEN103_REPO=https://github.com/XVincentX/Open103.git
ARG OPEN103_VERSION=887ff5a5
ARG OPEN103_SHA=887ff5a5d909e9a23f94dafa2a5c4bdbb20f30ee
ARG ADAPTER_VERSION=dev

ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1

RUN apt-get update && apt-get install -y --no-install-recommends git g++ && rm -rf /var/lib/apt/lists/*

WORKDIR /src/upstream
RUN git init -q . \
    && git remote add origin "${OPEN103_REPO}" \
    && git fetch -q --depth 1 origin "${OPEN103_SHA}" \
    && git checkout -q --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${OPEN103_SHA}"

COPY adapters/iec103/open103/shim /src/shim
RUN mkdir -p /out/native \
    && g++ -std=c++17 -O2 -fPIC -shared -I/src/upstream/Open103 -include /src/shim/linux.h \
         /src/shim/shim.cpp -o /out/native/libopen103.so

COPY adapters/iec103/common/dotnet/*.cs /src/common/
COPY adapters/iec103/open103/Adapter.csproj adapters/iec103/open103/packages.lock.json /src/adapter/
WORKDIR /src/adapter
RUN dotnet restore --locked-mode
COPY adapters/iec103/open103/Program.cs ./
RUN dotnet publish -c Release --no-restore -o /out/app \
      -p:AdapterVersion="${ADAPTER_VERSION}" -p:UpstreamVersion="${OPEN103_VERSION}" -p:UpstreamRevision="${OPEN103_SHA}" \
    && cp /out/native/libopen103.so /out/app/ \
    && mkdir -p /out/licenses/XVincentX-Open103 \
    && cp /src/upstream/LICENSE /out/licenses/XVincentX-Open103/

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

LABEL org.opencontainers.image.title="iec60870-5-interop-iec103-open103" \
      org.opencontainers.image.description="IEC 60870-5-103 control system built on XVincentX/Open103 (FT 1.2 over TCP)" \
      org.opencontainers.image.source="https://github.com/otfabric/iec60870-5-interop" \
      org.opencontainers.image.licenses="GPL-3.0-or-later" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1
USER interop

ENTRYPOINT ["/usr/local/bin/iec60870-5-interop"]
CMD ["print-capabilities"]

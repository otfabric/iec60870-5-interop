# iec60870-5-interop IEC 60870-5-102 adapter for gisce/iec870ree.
#
# Build from the repository root:
#   docker build -f adapters/iec102/gisce/Dockerfile -t iec60870-5-interop-iec102-gisce:dev .
#
# gisce/iec870ree (AGPL-3.0) is a Python master (controlling station) of the
# Spanish REE electricity meter profile of IEC 60870-5-102, with an IP
# transport; it has no controlled station, so the image is a client only.
# The package is fetched by commit and copied as it is (Python source) into
# /opt/iec870ree; its runtime dependencies are installed from PyPI with
# pinned versions and hashes (requirements.txt). The driver (adapter.py) is
# GPL-3.0-or-later like the other adapters and imports the library, which
# stays AGPL-3.0. The image carries the complete source of both (AGPL-3.0
# section 13 and GPL-3.0 section 6: this Dockerfile, the pinned upstream and
# adapter.py are the corresponding source). Licences in /licenses, see
# THIRD_PARTY_NOTICES.md. The upstream pin is mirrored in versions.yaml.

ARG BASE_REGISTRY=docker.io/library
ARG PYTHON_DIGEST=sha256:935c3c5b12357a7e10caa9c1e6ca5692344d97cf229832ef584be2cec9fc03df

# ---------------------------------------------------------------------------
# Stage 1: the library from a pinned commit, its dependencies by hash
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/python:3.12-slim-bookworm@${PYTHON_DIGEST} AS build

ARG IEC870REE_REPO=https://github.com/gisce/iec870ree.git
ARG IEC870REE_VERSION=v0.14.1
ARG IEC870REE_SHA=993242daa3e998c9d86948881d97835438d8bef3

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 PIP_NO_CACHE_DIR=1 PIP_ROOT_USER_ACTION=ignore

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*

# Fetched by commit, so the pin is the hash and not a tag that could move.
WORKDIR /src/upstream
RUN git init -q . \
    && git remote add origin "${IEC870REE_REPO}" \
    && git fetch -q --depth 1 origin "${IEC870REE_SHA}" \
    && git checkout -q --detach FETCH_HEAD \
    && test "$(git rev-parse HEAD)" = "${IEC870REE_SHA}" \
    && grep -q "version=\"${IEC870REE_VERSION#v}\"" setup.py

COPY adapters/iec102/gisce/requirements.txt adapters/iec102/gisce/build-requirements.txt /src/
RUN pip install -q --require-hashes --no-deps -r /src/build-requirements.txt \
    && pip install -q --require-hashes --no-deps --no-build-isolation --target /out/site -r /src/requirements.txt \
    && mkdir -p /out/opt/iec870ree /out/licenses/gisce-iec870ree \
    && cp -r /src/upstream/iec870ree /out/opt/iec870ree/ \
    && cp /src/upstream/LICENSE /src/upstream/README.rst /src/upstream/setup.py /src/upstream/requirements.txt /out/opt/iec870ree/ \
    && cp /src/upstream/LICENSE /out/licenses/gisce-iec870ree/LICENSE \
    && for d in /out/site/*.dist-info; do \
         p="$(basename "$d" .dist-info)"; mkdir -p "/out/licenses/pypi/$p"; \
         find "$d" -maxdepth 2 -type f \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' \) -exec cp {} "/out/licenses/pypi/$p/" \; ; \
         ls "/out/licenses/pypi/$p" | grep -q . || cp "$d/METADATA" "/out/licenses/pypi/$p/"; \
       done \
    && find /out -name '__pycache__' -prune -exec rm -rf {} +

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/python:3.12-slim-bookworm@${PYTHON_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev
ARG IEC870REE_VERSION=v0.14.1
ARG IEC870REE_SHA=993242daa3e998c9d86948881d97835438d8bef3

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/iec60870-5-interop \
    && chown interop /run/iec60870-5-interop \
    && printf '#!/bin/sh\nexec /usr/local/bin/python3 -I /opt/adapter/adapter.py "$@"\n' > /usr/local/bin/iec60870-5-interop \
    && chmod 755 /usr/local/bin/iec60870-5-interop \
    && printf '/opt/site\n/opt/iec870ree\n' > "$(python3 -c 'import site; print(site.getsitepackages()[0])')/iec60870-5-interop.pth"

COPY --from=build /out/site /opt/site
COPY --from=build /out/opt/iec870ree /opt/iec870ree
COPY --from=build /out/licenses /licenses
COPY adapters/iec102/gisce/adapter.py /opt/adapter/adapter.py
COPY licenses/GPL-3.0.txt /licenses/GPL-3.0.txt
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY fixtures/iec102 /fixtures
RUN cp /usr/local/lib/python3.12/LICENSE.txt /licenses/python-LICENSE.txt \
    && python3 -I -c 'import iec870ree.protocol, iec870ree.app_asdu, iec870ree.base_asdu'

LABEL org.opencontainers.image.title="iec60870-5-interop-iec102-gisce" \
      org.opencontainers.image.description="IEC 60870-5-102 controlling station (REE profile) built on gisce/iec870ree (FT 1.2 over TCP)" \
      org.opencontainers.image.source="https://github.com/otfabric/iec60870-5-interop" \
      org.opencontainers.image.licenses="GPL-3.0-or-later AND AGPL-3.0-only" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

ENV ADAPTER_VERSION=${ADAPTER_VERSION} IEC870REE_VERSION=${IEC870REE_VERSION} IEC870REE_SHA=${IEC870REE_SHA} \
    PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
USER interop

ENTRYPOINT ["/usr/local/bin/iec60870-5-interop"]
CMD ["print-capabilities"]

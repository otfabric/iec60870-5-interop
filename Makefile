# Self-documented Makefile (https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html)
# Run 'make' or 'make help' to list targets.
#
# Pre-push gate:  make ci
# Quick check:    make images smoke
#
# An image is <standard>-<adapter> (iec101-lib60870, iec104-cbsigma, ...):
# adapters/<standard>/<adapter>/Dockerfile, ghcr.io/otfabric/iec60870-5-interop-<standard>-<adapter>.

.DEFAULT_GOAL := help

REGISTRY  ?= ghcr.io/otfabric
VERSION   ?= dev
PLATFORMS ?= linux/amd64,linux/arm64

# Registry of the base images (debian, maven, eclipse-temurin, golang, python). They
# are pinned by digest in the Dockerfiles, so any mirror of Docker Hub's
# official images gives the same build (also the python image of the IEC 102
# adapter and of the scripted meter, scripts/smoke-iec102.sh). The .NET images of the IEC 103
# adapters come from mcr.microsoft.com, also pinned by digest. CI sets
#   BASE_REGISTRY=mirror.gcr.io/library
# because anonymous pulls from Docker Hub are rate limited per runner address.
BASE_REGISTRY ?= docker.io/library

# Registry of the images the build tooling itself runs: BuildKit, the QEMU
# handlers, shellcheck, Python. CI sets TOOLS_REGISTRY=mirror.gcr.io.
TOOLS_REGISTRY ?= docker.io

# Repeats a registry command that failed on a rate limit, a 5xx or a timeout.
RETRY := scripts/retry.sh

BUILDER ?= iec60870-5-interop

# Adapters per standard, in priority order. Override to work on one:
#   make smoke-iec101 IEC101_ADAPTERS=cbsigma
IEC101_ADAPTERS ?= lib60870 cbsigma digitalpetri
IEC104_ADAPTERS ?= lib60870 openmuc wendy512 cbsigma part5
# IEC 103: vitusdong has both roles; the others are clients (control systems).
IEC103_ADAPTERS ?= vitusdong goiecp5 ariec103 open103
# IEC 102: clients only (controlling stations); smoke-tested against the
# scripted meter tests/iec102/meter.py, which is not an implementation of IEC 102.
IEC102_ADAPTERS ?= gisce
# IEC 101/102/103 self-tests run once per link mode.
LINK_MODES ?= unbalanced balanced

IMAGES := $(addprefix iec101-,$(IEC101_ADAPTERS)) $(addprefix iec102-,$(IEC102_ADAPTERS)) $(addprefix iec103-,$(IEC103_ADAPTERS)) $(addprefix iec104-,$(IEC104_ADAPTERS))
# Images that are built and tested but not published (none: iec103-goiecp5 is
# published since v0.2.1, docs/REFERENCE_STACKS.md).
UNPUBLISHED ?=
PUBLISHED := $(filter-out $(UNPUBLISHED),$(IMAGES))

image = $(REGISTRY)/iec60870-5-interop-$(1)
std_of = $(word 1,$(subst -, ,$(1)))
adapter_of = $(word 2,$(subst -, ,$(1)))

export REGISTRY VERSION BASE_REGISTRY

.PHONY: publish-arch publish-manifest digests help all ci validate documents lint images smoke smoke-iec101 smoke-iec102 smoke-iec103 smoke-iec104 \
        interop interop-iec101 interop-iec103 interop-iec104 buildx buildx-setup publish capabilities clean FORCE
FORCE:

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_%-]+:.*?## / {printf "\033[36m%-24s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: validate images ## Validate fixtures and build the local images

ci: validate lint images documents ## Full local CI: validate, lint, build, smoke and cross-stack self-tests, document schemas
	@echo ""
	@echo "All local CI checks passed."

# ── Validation ───────────────────────────────────────────────────────────────

validate: ## Validate every fixture against the schema and the cross-field rules; check versions.yaml and RELEASE.md
	@scripts/check-versions.sh
	@scripts/release-notes.sh > /dev/null && echo "ok   RELEASE.md has the notes of interop_version"
	@python3 -m unittest discover -q -s scripts -p 'test_*.py'
	@echo "Validating fixtures..."
	@if python3 -c 'import jsonschema' 2>/dev/null; then \
		python3 scripts/validate-fixtures.py --require-schema; \
	else \
		echo "(jsonschema not installed locally: running the validator in a container)"; \
		$(RETRY) docker run --rm -v "$(CURDIR):/work:ro" -w /work $(TOOLS_REGISTRY)/library/python:3.12-slim \
			sh -c 'pip install -q --root-user-action=ignore jsonschema && python scripts/validate-fixtures.py --require-schema'; \
	fi

documents: ## Run the self-tests keeping every document in reports/, and validate them against schemas/
	@rm -rf reports && mkdir -p reports
	@REPORT_DIR=reports $(MAKE) --no-print-directory smoke interop
	@echo "Validating documents..."
	@if python3 -c 'import jsonschema' 2>/dev/null; then \
		python3 scripts/validate-documents.py reports; \
	else \
		$(RETRY) docker run --rm -v "$(CURDIR):/work:ro" -w /work $(TOOLS_REGISTRY)/library/python:3.12-slim \
			sh -c 'pip install -q --root-user-action=ignore jsonschema && python scripts/validate-documents.py reports'; \
	fi

lint: ## Shellcheck the scripts (the installed shellcheck, or a container when there is none)
	@echo "Linting scripts..."
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck --severity=warning -x -P scripts scripts/*.sh; \
	else \
		$(RETRY) docker run --rm -v "$(CURDIR):/mnt:ro" $(TOOLS_REGISTRY)/koalaman/shellcheck:stable --severity=warning -x -P scripts scripts/*.sh; \
	fi

# ── Local single-arch images (loaded into Docker; used by smoke and interop) ──

images: $(addprefix image-,$(IMAGES)) ## Build every adapter image for the host architecture

image-%: FORCE ## Build one image, e.g. image-iec101-lib60870
	@echo "Building $* image ($(VERSION))..."
	@$(RETRY) docker build \
		--build-arg BASE_REGISTRY=$(BASE_REGISTRY) --build-arg ADAPTER_VERSION=$(VERSION) \
		--tag $(call image,$*):$(VERSION) \
		--file adapters/$(call std_of,$*)/$(call adapter_of,$*)/Dockerfile \
		.

# ── Self-tests (images must be built) ─────────────────────────────────────────

smoke: smoke-iec101 smoke-iec102 smoke-iec103 smoke-iec104 ## Container-contract smoke test of every image

smoke-iec101: ## Smoke test every IEC 101 image in every link mode
	@for m in $(LINK_MODES); do for a in $(IEC101_ADAPTERS); do \
		LINK_MODE=$$m scripts/smoke.sh iec101 $$a || exit 1; done; done

smoke-iec102: ## Smoke test every IEC 102 image (clients, against the scripted meter of tests/iec102)
	@for a in $(IEC102_ADAPTERS); do scripts/smoke.sh iec102 $$a || exit 1; done

smoke-iec103: ## Smoke test every IEC 103 image (unbalanced only; a client-only image runs against vitusdong's server)
	@for a in $(IEC103_ADAPTERS); do scripts/smoke.sh iec103 $$a || exit 1; done

smoke-iec104: ## Smoke test every IEC 104 image
	@for a in $(IEC104_ADAPTERS); do scripts/smoke.sh iec104 $$a || exit 1; done

smoke-%: FORCE ## Smoke test one image, e.g. smoke-iec104-openmuc (IEC 101: LINK_MODE=balanced)
	@scripts/smoke.sh $(call std_of,$*) $(call adapter_of,$*)

interop: interop-iec101 interop-iec103 interop-iec104 ## Cross-stack self-tests: every client against every server

interop-iec101: ## IEC 101 cross-stack self-test, once per link mode
	@for m in $(LINK_MODES); do LINK_MODE=$$m scripts/interop.sh iec101 $(IEC101_ADAPTERS) || exit 1; done

interop-iec103: ## IEC 103 cross-stack self-test: every client against every server (unbalanced)
	@scripts/interop.sh iec103 $(IEC103_ADAPTERS)

interop-iec104: ## IEC 104 cross-stack self-test
	@scripts/interop.sh iec104 $(IEC104_ADAPTERS)

capabilities: ## Print the capability document of every image
	@for i in $(IMAGES); do docker run --rm $(call image,$$i):$(VERSION) print-capabilities; done

# ── Multi-arch build and publish ──────────────────────────────────────────────

buildx-setup: ## Prepare multi-arch builds: QEMU handlers and a docker-container builder
	@$(RETRY) docker run --privileged --rm $(TOOLS_REGISTRY)/tonistiigi/binfmt:latest --install arm64,amd64 >/dev/null
	@docker buildx rm $(BUILDER) >/dev/null 2>&1 || true
	@$(RETRY) docker buildx create --name $(BUILDER) --driver docker-container \
		--driver-opt image=$(TOOLS_REGISTRY)/moby/buildkit:buildx-stable-1 --bootstrap >/dev/null

builder_flag = $(shell docker buildx inspect $(BUILDER) >/dev/null 2>&1 && echo --builder $(BUILDER))

buildx: ## Build every image for $(PLATFORMS) under emulation, without pushing
	@for i in $(IMAGES); do \
		std=$${i%%-*}; a=$${i#*-}; \
		echo "Building $$i for $(PLATFORMS)..."; \
		$(RETRY) docker buildx build $(builder_flag) --platform $(PLATFORMS) \
			--build-arg BASE_REGISTRY=$(BASE_REGISTRY) --build-arg ADAPTER_VERSION=$(VERSION) \
			--file adapters/$$std/$$a/Dockerfile . || exit 1; \
	done

publish: ## Build and push multi-arch images under emulation (requires VERSION=vX.Y.Z; the Release workflow uses publish-arch instead)
	@if ! echo "$(VERSION)" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		echo "Error: VERSION must be in the form v<major>.<minor>.<patch>"; exit 1; fi
	@for i in $(PUBLISHED); do \
		std=$${i%%-*}; a=$${i#*-}; \
		$(RETRY) docker buildx build $(builder_flag) --platform $(PLATFORMS) --push \
			--build-arg BASE_REGISTRY=$(BASE_REGISTRY) --build-arg ADAPTER_VERSION=$(VERSION) \
			--tag $(call image,$$i):$(VERSION) --file adapters/$$std/$$a/Dockerfile . || exit 1; \
	done

ARCH  ?= $(shell docker version --format '{{.Server.Arch}}' 2>/dev/null)
ARCHS ?= amd64 arm64

publish-arch: ## Push the local images of this machine as <image>:$(VERSION)-$(ARCH) (build and self-test them first)
	@if ! echo "$(VERSION)" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		echo "Error: VERSION must be in the form v<major>.<minor>.<patch>"; exit 1; fi
	@for i in $(PUBLISHED); do \
		got=$$(docker image inspect --format '{{.Architecture}}' $(call image,$$i):$(VERSION)) || exit 1; \
		if [ "$$got" != "$(ARCH)" ]; then echo "Error: $$i:$(VERSION) is $$got, not $(ARCH)"; exit 1; fi; \
		echo "Pushing $$i $(VERSION) for linux/$(ARCH)..."; \
		docker tag $(call image,$$i):$(VERSION) $(call image,$$i):$(VERSION)-$(ARCH) || exit 1; \
		$(RETRY) docker push --quiet $(call image,$$i):$(VERSION)-$(ARCH) || exit 1; \
	done

publish-manifest: ## Join the per-architecture images of VERSION into the multi-arch <image>:$(VERSION)
	@for i in $(PUBLISHED); do \
		echo "Creating $$i $(VERSION) from: $(ARCHS)"; \
		$(RETRY) docker buildx imagetools create --tag $(call image,$$i):$(VERSION) \
			$(foreach arch,$(ARCHS),$(call image,$$i):$(VERSION)-$(arch)) || exit 1; \
		for arch in $(ARCHS); do \
			$(RETRY) docker buildx imagetools inspect $(call image,$$i):$(VERSION) --format '{{json .Manifest}}' \
				| tr -d ' \n' | grep -q "\"architecture\":\"$$arch\"" || { echo "Error: $$i:$(VERSION) has no linux/$$arch image"; exit 1; }; \
		done; \
	done

digests: ## Print "<standard>-<adapter> <digest>" of the published multi-arch images of VERSION
	@for i in $(PUBLISHED); do \
		d=$$($(RETRY) docker buildx imagetools inspect $(call image,$$i):$(VERSION) --format '{{.Manifest.Digest}}') || exit 1; \
		echo "$$i $$d"; \
	done

# ── Clean ─────────────────────────────────────────────────────────────────────

clean: ## Remove test containers, networks, local images and reports
	@docker ps -aq --filter "label=iec60870-5-interop.run" | xargs -r docker rm -f >/dev/null 2>&1 || true
	@docker network ls --format '{{.Name}}' | grep '^iec60870-5-interop-' | xargs -r docker network rm >/dev/null 2>&1 || true
	@docker compose down --remove-orphans 2>/dev/null || true
	@for i in $(IMAGES); do docker rmi $(call image,$$i):$(VERSION) 2>/dev/null || true; done
	@rm -rf reports

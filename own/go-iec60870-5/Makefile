# The iec60870-5-interop-<standard>-otfabric images, from the repository root:
#   make -f own/go-iec60870-5/Makefile images smoke interop
#   make -f own/go-iec60870-5/Makefile image smoke interop STANDARD=iec103
#
# Apart from the top-level Makefile on purpose: these images are not adapters
# of a harness release and not in manifest.json (README.md).

STANDARDS ?= iec101 iec102 iec103 iec104
STANDARD ?= iec101
TAG ?= dev
image_name = ghcr.io/otfabric/iec60870-5-interop-$(1)-otfabric:$(TAG)
# The harness release of the reference images to test against.
VERSION ?= $(shell git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -1)
BASE_REGISTRY ?= docker.io/library
# IEC 101/102/103: the self-tests run once per link mode the standard has.
LINK_MODES ?= unbalanced balanced

# The references of each standard: every image of the harness that can pair
# with ours (IEC 102: gisce is a client; our server is the station it reads).
REFERENCES_iec101 ?= lib60870 cbsigma digitalpetri
REFERENCES_iec102 ?= gisce
REFERENCES_iec103 ?= vitusdong goiecp5 ariec103 open103
REFERENCES_iec104 ?= lib60870 openmuc wendy512 cbsigma part5

# The latest go-iec60870-5 release, as the workflow builds it.
UPSTREAM_VERSION ?= $(shell git ls-remote --tags --refs https://github.com/otfabric/go-iec60870-5.git 'v[0-9]*' | sed 's,.*refs/tags/,,' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -1)
# The commit: the peeled line (^{}) of an annotated tag, sorted last, else the tag's own.
UPSTREAM_SHA ?= $(shell git ls-remote --tags https://github.com/otfabric/go-iec60870-5.git 'refs/tags/$(UPSTREAM_VERSION)^{}' 'refs/tags/$(UPSTREAM_VERSION)' | tail -1 | cut -f1)

export VERSION
export ADAPTER_IMAGE_iec101_otfabric = $(call image_name,iec101)
export ADAPTER_IMAGE_iec102_otfabric = $(call image_name,iec102)
export ADAPTER_IMAGE_iec103_otfabric = $(call image_name,iec103)
export ADAPTER_IMAGE_iec104_otfabric = $(call image_name,iec104)

port_of = $(if $(filter iec104,$(1)),2404,2401)

.PHONY: image smoke interop images smoke-all interop-all test

image: ## Build the image of STANDARD
	docker build --build-arg BASE_REGISTRY=$(BASE_REGISTRY) --build-arg STANDARD=$(STANDARD) --build-arg PORT=$(call port_of,$(STANDARD)) \
		--build-arg UPSTREAM_VERSION=$(UPSTREAM_VERSION) --build-arg UPSTREAM_SHA=$(UPSTREAM_SHA) --build-arg ADAPTER_VERSION=$(UPSTREAM_VERSION) \
		--tag $(call image_name,$(STANDARD)) --file own/go-iec60870-5/Dockerfile .

smoke: ## Smoke test the image of STANDARD (IEC 101: every link mode)
	@if [ "$(STANDARD)" = iec101 ]; then for m in $(LINK_MODES); do LINK_MODE=$$m scripts/smoke.sh iec101 otfabric || exit 1; done; \
	else scripts/smoke.sh $(STANDARD) otfabric; fi

# GATE=otfabric: only the pairings with our image, not the references among
# themselves (the harness's own CI runs those).
interop: ## Cross-stack self-test of the image of STANDARD against the references of VERSION
	@if [ "$(STANDARD)" = iec101 ]; then for m in $(LINK_MODES); do GATE=otfabric LINK_MODE=$$m scripts/interop.sh iec101 otfabric $(REFERENCES_iec101) || exit 1; done; \
	else GATE=otfabric scripts/interop.sh $(STANDARD) otfabric $(REFERENCES_$(STANDARD)); fi

test: ## go vet and go test of the module (local toolchain)
	cd own/go-iec60870-5 && go vet ./... && go test ./...

images: ## Build all four images
	@for s in $(STANDARDS); do $(MAKE) -f own/go-iec60870-5/Makefile image STANDARD=$$s || exit 1; done

smoke-all: ## Smoke test all four images
	@for s in $(STANDARDS); do $(MAKE) -f own/go-iec60870-5/Makefile smoke STANDARD=$$s || exit 1; done

interop-all: ## Cross-stack self-test of all four images
	@for s in $(STANDARDS); do $(MAKE) -f own/go-iec60870-5/Makefile interop STANDARD=$$s || exit 1; done

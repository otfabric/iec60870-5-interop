#!/usr/bin/env bash
# Shared helpers for the self-test scripts. Source, do not execute.
#
# Servers and clients run as containers on a private Docker network and reach
# each other by container name, so no host port is published and parallel runs
# do not collide.

REGISTRY="${REGISTRY:-ghcr.io/otfabric}"
VERSION="${VERSION:-dev}"
RUN_ID="${RUN_ID:-$$}"
NETWORK="iec60870-5-interop-${RUN_ID}"
READY_FILE="/run/iec60870-5-interop/ready"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-60}"
# The standard under test (iec101, iec104, ...): the image family, fixtures/<standard>
# and tests/<standard>. The scripts take it as their first argument.
STANDARD="${STANDARD:-iec104}"
# IEC 101/102/103: the link mode every server and client of the run uses.
LINK_MODE="${LINK_MODE:-}"

# serial_standard: the standard runs FT 1.2 over a TCP byte stream (contract 2.0).
serial_standard() { [[ "${STANDARD}" != iec104 ]]; }

# LINK_FLAGS: appended to every server and client invocation of the run.
LINK_FLAGS=()
set_link_mode() {
    LINK_FLAGS=()
    if [[ "${STANDARD}" == iec103 ]]; then
        SUMMARY="${SUMMARY_103}"
        NORMALIZE="${NORMALIZE_103}"
    fi
    if serial_standard; then
        LINK_MODE="${LINK_MODE:-unbalanced}"
        LINK_FLAGS=(--link-mode "${LINK_MODE}")
    fi
}

# check_standard <standard>: one that has fixtures and test cases here.
check_standard() {
    case "$1" in
        iec101|iec102|iec103|iec104) ;;
        *) echo "unknown standard '$1': use iec101, iec102, iec103 or iec104" >&2; exit 2 ;;
    esac
}

PASS=0
FAIL=0
# shellcheck source=scripts/results.sh
source "$(dirname "${BASH_SOURCE[0]}")/results.sh"

# DOCKER_ARGS: extra "docker run" arguments of start_server and run_client,
# such as a mount: DOCKER_ARGS=(-v "${dir}:/secrets:ro").
DOCKER_ARGS=()

log()  { echo "[$(basename "$0" .sh)] $*" >&2; }
ok()   { PASS=$((PASS + 1)); result pass; [[ -n "${VERBOSE:-}" ]] && log "ok: $*"; return 0; }
fail() { FAIL=$((FAIL + 1)); result fail "$*"; log "FAIL: $*"; return 0; }

# image_of <adapter>: ADAPTER_IMAGE_<standard>_<adapter> names an image outside the release
# set, which this repository's scripts know nothing about.
image_of() {
    local override="ADAPTER_IMAGE_${STANDARD}_$1"
    if [[ -n "${!override:-}" ]]; then echo "${!override}"; else echo "${REGISTRY}/iec60870-5-interop-${STANDARD}-$1:${VERSION}"; fi
}

require_tools() {
    local t
    for t in docker jq; do
        command -v "$t" >/dev/null || { log "missing required tool: $t"; exit 2; }
    done
}

network_up() { docker network create "${NETWORK}" >/dev/null; }

cleanup() {
    docker ps -aq --filter "label=iec60870-5-interop.run=${RUN_ID}" | xargs -r docker rm -f >/dev/null 2>&1
    docker network rm "${NETWORK}" >/dev/null 2>&1
    return 0
}

# server_name <adapter>: the container name start_server uses. SERVER_TAG
# distinguishes servers of one adapter that run at the same time.
server_name() { echo "srv-$1-${SERVER_TAG:-one}-${RUN_ID}"; }

# start_server <adapter> [server args...]  -> prints the container name
start_server() {
    local adapter="$1"; shift
    local name
    name=$(server_name "${adapter}")
    docker rm -f "${name}" >/dev/null 2>&1
    docker run -d --name "${name}" --network "${NETWORK}" \
        --label "iec60870-5-interop.run=${RUN_ID}" ${DOCKER_ARGS[@]+"${DOCKER_ARGS[@]}"} \
        "$(image_of "${adapter}")" server ${LINK_FLAGS[@]+"${LINK_FLAGS[@]}"} "$@" >/dev/null
    echo "${name}"
}

# wait_ready <container>: the ready file exists only once the fixture is loaded
# and the listener is up. No fixed delays.
# first_event <container>: the first stdout line of a server, waiting up to
# 5 s for it. The readiness file may appear before the ready event is
# flushed to stdout (the contract does not order the two), so a server that
# is ready can still have an empty log for a moment.
first_event() {
    local name="$1" line="" tries=0
    while (( tries < 50 )); do
        tries=$((tries + 1))
        line=$(docker logs "${name}" 2>/dev/null | head -1)
        [[ -n "${line}" ]] && break
        sleep 0.1
    done
    printf '%s' "${line}"
}

wait_ready() {
    local name="$1" waited=0 state
    while true; do
        state=$(docker inspect --format '{{.State.Status}}' "${name}" 2>/dev/null || echo absent)
        if [[ "${state}" != "running" ]]; then
            log "server ${name} is ${state}"; docker logs --tail 20 "${name}" >&2 2>/dev/null; return 1
        fi
        docker exec "${name}" test -f "${READY_FILE}" 2>/dev/null && return 0
        if (( waited >= WAIT_TIMEOUT * 5 )); then
            log "server ${name} not ready after ${WAIT_TIMEOUT}s"; docker logs --tail 20 "${name}" >&2; return 1
        fi
        sleep 0.2; waited=$((waited + 1))
    done
}

stop_server() { docker rm -f "$1" >/dev/null 2>&1; return 0; }

# run_client <adapter> <args...>: prints the result document and returns the
# exit code of the adapter. Capture both with
#     doc=$(run_client ...); CLIENT_EXIT=$?
run_client() {
    local adapter="$1"; shift
    docker run --rm --network "${NETWORK}" --label "iec60870-5-interop.run=${RUN_ID}" \
        ${DOCKER_ARGS[@]+"${DOCKER_ARGS[@]}"} "$(image_of "${adapter}")" client "$@" ${LINK_FLAGS[@]+"${LINK_FLAGS[@]}"} 2>/dev/null
}

# make_identities <dir>: key material for the security flags, made with
# openssl, valid for two days. Every key is on secp256r1.
#   ca            an authority (CA:TRUE)
#   station       the controlled station, signed by ca
#   scada         a controlling station, signed by ca
#   rogue         a controlling station nobody trusts (self-signed)
#   pin-station   self-signed controlled station, trusted by pin
#   pin-scada     self-signed controlling station, trusted by pin
#   other-ca      an authority that signed none of these
# Mount <dir> read-only; the files are readable by the uid of the images.
make_identities() {
    local dir="$1" n
    command -v openssl >/dev/null || { log "missing required tool: openssl"; return 1; }
    mkdir -p "${dir}" && chmod 755 "${dir}"
    openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 \
        -keyout "${dir}/ca.key" -out "${dir}/ca.pem" -subj "/CN=iec60870-5-interop CA" \
        -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign 2>/dev/null || return 1
    for n in station scada; do
        openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
            -keyout "${dir}/${n}.key" -out "${dir}/${n}.csr" -subj "/CN=${n}" 2>/dev/null || return 1
        openssl x509 -req -in "${dir}/${n}.csr" -CA "${dir}/ca.pem" -CAkey "${dir}/ca.key" -days 2 \
            -out "${dir}/${n}.pem" \
            -extfile <(printf 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyAgreement\n') \
            2>/dev/null || return 1
    done
    for n in rogue pin-station pin-scada; do
        openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 \
            -keyout "${dir}/${n}.key" -out "${dir}/${n}.pem" -subj "/CN=${n}" \
            -addext basicConstraints=critical,CA:FALSE 2>/dev/null || return 1
    done
    openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 \
        -keyout "${dir}/other-ca.key" -out "${dir}/other-ca.pem" -subj "/CN=iec60870-5-interop other CA" \
        -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign 2>/dev/null || return 1
    rm -f "${dir}"/*.csr
    chmod 644 "${dir}"/*
}

# What two adapters must agree on: everything except who answered, how long it
# took, the error message (for people, not part of the contract), the wall-clock
# time tags and the NOS of the last-section F_LS_NA_1 (LSQ 1 or 2), which the
# stacks fill differently, repeated by the F_AF_NA_1 of an upload (AFQ 1 or 2),
# and whether a directory is a sequence (docs/FIXTURES.md).
# shellcheck disable=SC2034
NORMALIZE='del(.adapter, .elapsedMs, .error.message) | (.asdus[]?.objects[]? |= del(.time))
    | (.asdus[]? |= if .type == "F_LS_NA_1" then (.objects[]? |= if .lsq == 1 or .lsq == 2 then del(.nos) else . end) else . end)
    | (.asdus[]? |= if .type == "F_AF_NA_1" then (.objects[]? |= if (.afq % 16) == 1 or (.afq % 16) == 2 then del(.nos) else . end) else . end)
    | (.asdus[]? |= if .type == "F_DR_TA_1" then del(.sequence) | (.objects[]? |= del(.timeInvalid)) else . end)'

# One line that states the outcome of an operation; the expectation format of
# tests/cases.tsv.
# shellcheck disable=SC2034
SUMMARY='"ok=\(.ok) error=\(.error.code // "-") conf=\([.confirmations[] | "\(.cot)\(if .negative then "n" else "" end)"] | join(",")) term=\(.terminated) asdus=\([.asdus[] | "\(.type // .typeId)/\(.cot)"] | join(","))"'

# The same with what tests/feature-cases.tsv also states.
# shellcheck disable=SC2034
FEATURE_SUMMARY="${SUMMARY}"' + (if has("tls") then " tls=\(.tls)" else "" end)
    + (if has("file") then " file=\(.file.length)/\(.file.received)/\(.file.sections)" else "" end)
    + (if has("directory") then " directory=\([.directory[] | "\(.ioa):\(.nof):\(.lof):\(.sof)"] | join(","))" else "" end)
    + (if has("redundancy") then " redundancy=\(.redundancy.primaryAsdus)/\(.redundancy.standbyAsdus)/\(.redundancy.strayAsdus)/\(.redundancy.queuedEvent)" else "" end)'

# IEC 103 (contract 2.1): the outcome line and the normalization leave out
# the identification (checked by values.identification) and the cyclic
# measurands (cause 2), which arrive whenever a client polls class 2, so their
# number in a result is not fixed. A time tag is the station's clock, and the
# raw elements of an ASDU with a time tag carry it too.
# IEC 103 (contract 2.3): for the generic services and the transmission of
# disturbance data (generic-*, disturbance-*) the outcome line leaves out
# every ASDU: their number depends on how the station splits the data and,
# for a client that waits for the list after an interrogation, on the
# interrogation; what they carried is in generic, disturbances and
# disturbance, which the values checks compare with the fixture.
# set_link_mode puts them in place of SUMMARY and NORMALIZE.
# shellcheck disable=SC2034
SUMMARY_103='(.operation | test("^(generic|disturbance)-")) as $svc | "ok=\(.ok) error=\(.error.code // "-") conf=\([.confirmations[] | "\(.cot)\(if .negative then "n" else "" end)"] | join(",")) term=\(.terminated) asdus=\([.asdus[] | select(.typeId != 5 and .cot != 2 and ($svc | not)) | "\(.type // .typeId)/\(.cot)"] | join(","))"'
# shellcheck disable=SC2034
NORMALIZE_103='del(.adapter, .elapsedMs, .error.message) | .asdus |= map(select(.typeId != 5 and .cot != 2))
    | (.asdus[]?.objects[]? |= del(.time, .time32, .timeInvalid))
    | (.asdus[]? |= if (.typeId == 1 or .typeId == 2 or .typeId == 4 or .typeId == 6) then (.objects[]? |= del(.data)) else . end)'

KNOWN=0

finish() {
    echo "" >&2
    local extra=""
    [[ "${KNOWN}" -gt 0 ]] && extra="${extra}, ${KNOWN} known upstream bugs"
    [[ "${SKIPPED:-0}" -gt 0 ]] && extra="${extra}, ${SKIPPED} skipped (capability not declared)"
    log "${PASS} passed, ${FAIL} failed${extra}"
    results_close || FAIL=$((FAIL + 1))
    [[ "${FAIL}" -eq 0 && "${PASS}" -gt 0 ]]
}

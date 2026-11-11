#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# The smoke checks of an IEC 102 image (contract 2.2: FT 1.2 over a TCP byte
# stream, unbalanced, the REE profile). Sourced by scripts/smoke.sh after the
# checks every image shares (capabilities, fixtures); do not execute.
#
# No IEC 102 image has a server, and there is no open IEC 102 controlled
# station to run a client against (docs/REFERENCE_STACKS.md). The client is
# therefore checked against tests/iec102/meter.py, a scripted meter written in
# this repository from the contract and the fixture. It is NOT an independent
# implementation of IEC 60870-5-102: a pass shows that the image keeps the
# contract and reads the fixture's values, nothing more (docs/COMPATIBILITY.md).
#
# shellcheck disable=SC2154  # ADAPTER, IMAGE, FIXTURES, caps come from smoke.sh

METER_IMAGE="${METER_IMAGE:-${BASE_REGISTRY:-docker.io/library}/python:3.12-slim-bookworm@sha256:935c3c5b12357a7e10caa9c1e6ca5692344d97cf229832ef584be2cec9fc03df}"
role() { jq -e --arg r "$1" '.roles[$r] == true' <<<"${caps}" >/dev/null 2>&1; }
FX="${FIXTURES}/baseline/fixture.json"
KEY=$(jq '.station.accessKey' "${FX}")
REE=(--profile ree --access-key "${KEY}")
DOCS=0

if jq -e '.transport == "tcp-stream" and .linkModes == ["unbalanced"] and (.asduTypes.monitor | type == "array")
        and (.asduTypes.control | type == "array") and (.linkAddressSizes | type == "array") and (.caSizes | type == "array")
        and (.profiles | type == "array") and .features.balancedMode == false' <<<"${caps}" >/dev/null 2>&1
then ok "capabilities: tcp-stream, unbalanced only, asduTypes, field sizes, profiles"; else fail "capabilities of an IEC 102 image: ${caps}"; fi
for f in integratedTotals readTime setTime productIdentification tariffInformation reeSession; do
    if jq -e --arg f "${f}" '.features[$f] | type == "boolean"' <<<"${caps}" >/dev/null 2>&1
    then ok "capabilities: feature ${f}"; else fail "capabilities lack the IEC 102 feature ${f}"; fi
done

# ---- usage errors never touch the network ----
TR=(--from 2026-01-01T00:00 --to 2026-01-01T05:00)
expect_exit 2 "unknown command" "${IMAGE}" frobnicate
expect_exit 2 "client without --host" "${IMAGE}" client read-time
expect_exit 2 "client with an unknown operation" "${IMAGE}" client frobnicate --host nowhere
expect_exit 2 "client with an operation of IEC 101/104" "${IMAGE}" client interrogate --host nowhere
expect_exit 2 "client with an unknown flag" "${IMAGE}" client read-time --host nowhere --frobnicate 1
expect_exit 2 "client with --cot-size (one octet in IEC 102)" "${IMAGE}" client read-time --host nowhere --cot-size 1
expect_exit 2 "client with --ioa-size" "${IMAGE}" client read-time --host nowhere --ioa-size 2
expect_exit 2 "client with --originator-address" "${IMAGE}" client read-time --host nowhere --originator-address 1
expect_exit 2 "client with --remote-link-address" "${IMAGE}" client read-time --host nowhere --remote-link-address 2
expect_exit 2 "client in balanced mode" "${IMAGE}" client read-time --host nowhere --link-mode balanced
expect_exit 2 "client with the broadcast link address" "${IMAGE}" client read-time --host nowhere --link-address 65535
expect_exit 2 "client with a link address field of three octets" "${IMAGE}" client read-time --host nowhere --link-address-size 3
for s in 1 2; do
    if ! jq -e --argjson s "${s}" '.linkAddressSizes | index($s)' <<<"${caps}" >/dev/null 2>&1; then
        expect_exit 2 "client with a link address size the image lacks (${s})" "${IMAGE}" client read-time --host nowhere --link-address-size "${s}"
    fi
    if ! jq -e --argjson s "${s}" '.caSizes | index($s)' <<<"${caps}" >/dev/null 2>&1; then
        expect_exit 2 "client with a common address size the image lacks (${s})" "${IMAGE}" client read-time --host nowhere --ca-size "${s}"
    fi
done
expect_exit 2 "client with an unknown profile" "${IMAGE}" client read-time --host nowhere --profile iberdrola
expect_exit 2 "--profile ree without --access-key" "${IMAGE}" client connect --host nowhere --profile ree
expect_exit 2 "--access-key without --profile ree" "${IMAGE}" client connect --host nowhere --access-key 7
expect_exit 2 "--access-key beyond four octets" "${IMAGE}" client connect --host nowhere --profile ree --access-key 4294967296
expect_exit 2 "read-totals without --from" "${IMAGE}" client read-totals --host nowhere --to 2026-01-01T05:00
expect_exit 2 "read-totals with --from after --to" "${IMAGE}" client read-totals --host nowhere --from 2026-01-02T00:00 --to 2026-01-01T00:00
expect_exit 2 "read-totals with a time that is no time a" "${IMAGE}" client read-totals --host nowhere --from 2026-01-01 --to 2026-01-01T05:00
expect_exit 2 "read-totals with an unknown kind" "${IMAGE}" client read-totals --host nowhere --kind cumulative "${TR[@]}"
expect_exit 2 "read-totals with --first after --last" "${IMAGE}" client read-totals --host nowhere --first 5 --last 2 "${TR[@]}"
expect_exit 2 "read-totals with a record address beyond an octet" "${IMAGE}" client read-totals --host nowhere --record 256 "${TR[@]}"
expect_exit 2 "set-time without --profile ree" "${IMAGE}" client set-time --host nowhere --time 2026-01-15T10:20:30.500
expect_exit 2 "set-time without --time" "${IMAGE}" client set-time --host nowhere "${REE[@]}"
expect_exit 2 "set-time with a time without milliseconds" "${IMAGE}" client set-time --host nowhere --time 2026-01-15T10:20 "${REE[@]}"
expect_exit 2 "read-tariff without --profile ree" "${IMAGE}" client read-tariff --host nowhere
expect_exit 2 "read-tariff with --from but no --to" "${IMAGE}" client read-tariff --host nowhere --from 2025-11-01T00:00 "${REE[@]}"
if role server; then
    fail "an IEC 102 image with a server: scripts/smoke-iec102.sh has no checks for it yet"
else
    expect_exit 2 "server of a client-only image" "${IMAGE}" server
fi

# ---- a connection that cannot be made ----
doc=$(run_client "${ADAPTER}" read-time --host "no-such-host-${RUN_ID}" --connect-timeout-ms 1500); CLIENT_EXIT=$?
if [[ "${CLIENT_EXIT}" == 3 ]] && jq -e '.ok == false and .error.code == "connect-failed" and .connected == false' <<<"${doc}" >/dev/null 2>&1
then ok "connect failure: exit 3 and a result document"; else fail "connect failure: exit ${CLIENT_EXIT}, ${doc}"; fi

# ---- the scripted meter ----
meter="meter-${RUN_ID}"
docker run -d --name "${meter}" --network "${NETWORK}" --label "iec60870-5-interop.run=${RUN_ID}" \
    -v "${ROOT}/tests/iec102:/meter:ro" -v "${FIXTURES}:/fixtures:ro" "${METER_IMAGE}" \
    python3 -I /meter/meter.py --fixture /fixtures/baseline/fixture.json --ready-file /tmp/ready >/dev/null
waited=0
until docker exec "${meter}" test -f /tmp/ready 2>/dev/null; do
    waited=$((waited + 1))
    if (( waited > 150 )); then fail "the scripted meter did not become ready"; docker logs "${meter}" >&2; finish; exit 1; fi
    sleep 0.2
done
log "scripted meter ready"

# client <label> <args...>: runs the image's client against the meter; keeps
# the document in REPORT_DIR. Sets doc and CLIENT_EXIT.
client() {
    local label="$1"; shift
    doc=$(run_client "${ADAPTER}" "$@" --host "${meter}"); CLIENT_EXIT=$?
    if [[ -n "${REPORT_DIR:-}" && -n "${doc}" ]]; then
        DOCS=$((DOCS + 1))
        printf '%s\n' "${doc}" > "${REPORT_DIR}/iec102-smoke-${ADAPTER}-${DOCS}-${label}.json"
    fi
}
# check <label> <jq filter on {doc, fx}>: one outcome.
check() {
    if [[ "${CLIENT_EXIT}" == "${WANT_EXIT:-0}" ]] && jq -e -n --argjson doc "${doc:-null}" --slurpfile fx "${FX}" \
        "(\$fx[0]) as \$fx | ${2}" >/dev/null 2>&1
    then ok "$1"; else fail "$1: exit ${CLIENT_EXIT}, ${doc}"; fi
    WANT_EXIT=0
}
asdus_of() { echo "[\$doc.asdus[] | select(.typeId == $1)]"; }
COMMON='$doc.schemaVersion == "1.0" and ($doc.adapter | type == "string") and ($doc.elapsedMs | type == "number")
    and ([$doc.asdus[] | (.type, .typeId, .cot, .negative, .test, .commonAddress, .record, .sequence, .count, .objects) != null] | all)
    and (($doc | has("startdtConfirmed")) | not)'

# A station that does not answer: another link address.
client link-init read-time --link-address 9 --timeout-ms 1500
WANT_EXIT=3 check "link start-up failure: exit 3, link-init-timeout" '$doc.ok == false and $doc.error.code == "link-init-timeout" and $doc.connected == false'

client connect connect
check "connect" "${COMMON}"' and $doc.ok and $doc.connected and $doc.error == null and $doc.asdus == [] and ($doc | has("session") | not)'
if [[ $(wc -l <<<"${doc}") -eq 1 ]]; then ok "client result is one line"; else fail "client printed more than one line on stdout"; fi

client connect-ree connect "${REE[@]}"
check "connect with the REE session" "${COMMON}"' and $doc.ok and $doc.session == {"login": true, "logout": true}
    and ([$doc.asdus[] | "\(.typeId)/\(.cot)/\(.negative)"] == ["183/7/false", "187/7/false"])'

client login-refused connect --profile ree --access-key $((KEY + 1))
WANT_EXIT=1 check "a wrong access key: login-refused" "${COMMON}"' and $doc.ok == false and $doc.error.code == "login-refused"
    and $doc.connected and $doc.session.login == false and ([$doc.asdus[] | select(.typeId == 183) | .negative] == [true])'

client read-product read-product
check "read-product: P_MP_NA_2 with the fixture's product" "${COMMON}"' and $doc.ok
    and ('"$(asdus_of 71)"' | map(.cot, (.objects[0] | {standard, manufacturer, product}))) == [5, $fx.station.product]'

client read-time read-time "${REE[@]}"
check "read-time: M_TI_TA_2 with the fixture's clock" "${COMMON}"' and $doc.ok and $doc.session == {"login": true, "logout": true}
    and ('"$(asdus_of 72)"' | map(.cot, .objects[0])) == [5, {time: $fx.station.clock, su: false, timeInvalid: false}]'

# totals <kind> <record>: the fixture's periods as the documents carry them.
PERIODS='def periods($k; $r; $lo; $hi; $first; $last): [$fx.totals[] | select(.kind == $k and .record == $r) | .periods[]
    | select(.end >= $lo and .end <= $hi) | {time: .end, objects: [.totals[] | select(.address >= $first and .address <= $last)]}
    | select(.objects != [])];
  def got($t): [$doc.asdus[] | select(.typeId == $t) | {time, objects}];'

client totals-absolute read-totals --from 2026-01-01T00:00 --to 2026-01-01T05:00
check "read-totals absolute: every period of the fixture as M_IT_TG_2" "${PERIODS} ${COMMON}"' and $doc.ok
    and $doc.confirmations == [{"cot": 7, "negative": false}] and $doc.terminated
    and got(8) == periods("absolute"; 11; "2026-01-01T00:00"; "2026-01-01T05:00"; 1; 8)
    and ([$doc.asdus[] | select(.typeId == 8) | .cot, .record, .su, .timeInvalid] | unique) == [false, 5, 11]
    and ([$doc.asdus[] | "\(.typeId)/\(.cot)"] | first, last) == ("122/7", "122/10")'

client totals-incremental read-totals --kind incremental --from 2026-01-01T00:00 --to 2026-01-01T05:00 "${REE[@]}"
check "read-totals incremental in a session: every period as M_IT_TK_2" "${PERIODS} ${COMMON}"' and $doc.ok
    and $doc.session == {"login": true, "logout": true} and $doc.terminated
    and got(11) == periods("incremental"; 11; "2026-01-01T00:00"; "2026-01-01T05:00"; 1; 8)'

client totals-subset read-totals --kind incremental --from 2026-01-01T02:00 --to 2026-01-01T03:00 --first 1 --last 3
check "read-totals of two periods and addresses 1..3" "${PERIODS} ${COMMON}"' and $doc.ok
    and got(11) == periods("incremental"; 11; "2026-01-01T02:00"; "2026-01-01T03:00"; 1; 3) and (got(11) | length) == 2'

for refusal in "18:no period in the range:--from 2027-01-01T00:00 --to 2027-01-01T05:00" \
               "16:an unknown record address:--record 12 --from 2026-01-01T00:00 --to 2026-01-01T05:00" \
               "17:no total in the address range:--first 7 --last 8 --from 2026-01-01T00:00 --to 2026-01-01T05:00" \
               "16:another common address:--common-address 2 --from 2026-01-01T00:00 --to 2026-01-01T05:00"; do
    cot=${refusal%%:*} rest=${refusal#*:} what=${rest%%:*} flags=${rest#*:}
    # shellcheck disable=SC2086  # the flags are words
    client "refused-${cot}" read-totals ${flags}
    WANT_EXIT=1 check "read-totals refused with cause ${cot}: ${what}" "${COMMON}"' and $doc.ok == false
        and $doc.error.code == "negative-confirmation" and $doc.terminated == false
        and $doc.confirmations == [{"cot": '"${cot}"', "negative": true}] and ([$doc.asdus[] | .typeId] == [122])'
done

TARIFF='def records($when): [$fx.tariffs[] | select(.register == 134) | .[$when][]];'
client tariff-current read-tariff "${REE[@]}"
check "read-tariff current values: M_TA_VC_2, one record each" "${TARIFF} ${COMMON}"' and $doc.ok and $doc.terminated
    and $doc.confirmations == [{"cot": 7, "negative": false}]
    and [$doc.asdus[] | select(.typeId == 135) | .count] == [records("current")[] | 1]
    and [$doc.asdus[] | select(.typeId == 135) | .objects[]] == records("current")'

client tariff-stored read-tariff --from 2025-11-15T00:00 --to 2026-01-01T00:00 "${REE[@]}"
check "read-tariff stored values of a range: M_TA_VM_2" "${TARIFF} ${COMMON}"' and $doc.ok and $doc.terminated
    and [$doc.asdus[] | select(.typeId == 136) | .objects[]] == [records("stored")[] | select(.periodEnd >= "2025-11-15T00:00" and .periodEnd <= "2026-01-01T00:00")]
    and ([$doc.asdus[] | select(.typeId == 136)] | length) == 2'

client tariff-refused read-tariff --register 135 "${REE[@]}"
WANT_EXIT=1 check "read-tariff of a register the station lacks: cause 16" "${COMMON}"' and $doc.ok == false
    and $doc.error.code == "negative-confirmation" and $doc.confirmations == [{"cot": 16, "negative": true}]
    and $doc.session == {"login": true, "logout": true}'

client set-time set-time --time 2026-02-03T04:05:06.789 "${REE[@]}"
check "set-time: C_CS_TA_2 confirmed" "${COMMON}"' and $doc.ok and $doc.confirmations == [{"cot": 7, "negative": false}]
    and ('"$(asdus_of 181)"' | map(.objects[0].time)) == ["2026-02-03T04:05:06.789"]'
client read-time-after read-time
check "read-time after set-time: the time that was set" "${COMMON}"' and $doc.ok
    and ('"$(asdus_of 72)"' | map(.objects[0].time)) == ["2026-02-03T04:05:06.789"]'

[[ -n "${VERBOSE:-}" ]] && docker logs "${meter}" >&2
docker rm -f "${meter}" >/dev/null 2>&1

#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# The smoke checks of an IEC 102 image (contract 2.2: FT 1.2 over a TCP byte
# stream, unbalanced, the REE profile). Sourced by scripts/smoke.sh after the
# checks every image shares (capabilities, fixtures); do not execute.
#
# There is no open IEC 102 controlled station to run a client against
# (docs/REFERENCE_STACKS.md). A client is therefore checked against
# tests/iec102/meter.py, a scripted meter written in this repository from the
# contract and the fixture. It is NOT an independent implementation of
# IEC 60870-5-102: a pass shows that the image keeps the contract and reads the
# fixture's values, nothing more (docs/COMPATIBILITY.md).
#
# An image with a server (contract, "IEC 102 server") also gets the server
# checks: usage errors, an invalid fixture (exit 4), the ready event, the
# image's own client running the meter checks against the image's server,
# the server events, the field sizes the image lists, a busy port and SIGTERM.
# A pass there shows that the image agrees with itself; the cross-stack run
# (scripts/interop.sh iec102) pairs it with the other clients.
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
    expect_exit 2 "server with an unknown flag" "${IMAGE}" server --frobnicate 1
    expect_exit 2 "server with --access-key (the fixture has the key)" "${IMAGE}" server --access-key "${KEY}"
    expect_exit 2 "server in balanced mode" "${IMAGE}" server --link-mode balanced
    expect_exit 2 "server with --cot-size" "${IMAGE}" server --cot-size 1
    expect_exit 2 "server with --ioa-size" "${IMAGE}" server --ioa-size 2
    expect_exit 2 "server with a link address field of three octets" "${IMAGE}" server --link-address-size 3
    expect_exit 2 "server with an unknown profile" "${IMAGE}" server --profile iberdrola
else
    expect_exit 2 "server of a client-only image" "${IMAGE}" server
fi

# ---- a connection that cannot be made ----
doc=$(run_client "${ADAPTER}" read-time --host "no-such-host-${RUN_ID}" --connect-timeout-ms 1500); CLIENT_EXIT=$?
if [[ "${CLIENT_EXIT}" == 3 ]] && jq -e '.ok == false and .error.code == "connect-failed" and .connected == false' <<<"${doc}" >/dev/null 2>&1
then ok "connect failure: exit 3 and a result document"; else fail "connect failure: exit ${CLIENT_EXIT}, ${doc}"; fi

# ---- the client: against the scripted meter ----
if role client; then
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
fi

# client <label> <args...>: runs the image's client against STATION (the
# meter or the image's own server); keeps the document in REPORT_DIR. Sets
# doc and CLIENT_EXIT.
client() {
    local label="$1"; shift
    doc=$(run_client "${ADAPTER}" "$@" --host "${STATION}"); CLIENT_EXIT=$?
    if [[ -n "${REPORT_DIR:-}" && -n "${doc}" ]]; then
        DOCS=$((DOCS + 1))
        printf '%s\n' "${doc}" > "${REPORT_DIR}/iec102-smoke-${ADAPTER}-${DOCS}-${STATION_LABEL}-${label}.json"
    fi
}
# check <label> <jq filter on {doc, fx}>: one outcome. ON names the station
# in the label ("" for the meter).
check() {
    if [[ "${CLIENT_EXIT}" == "${WANT_EXIT:-0}" ]] && jq -e -n --argjson doc "${doc:-null}" --slurpfile fx "${FX}" \
        "(\$fx[0]) as \$fx | ${2}" >/dev/null 2>&1
    then ok "${ON:-}$1"; else fail "${ON:-}$1: exit ${CLIENT_EXIT}, ${doc}"; fi
    WANT_EXIT=0
}
asdus_of() { echo "[\$doc.asdus[] | select(.typeId == $1)]"; }
COMMON='$doc.schemaVersion == "1.0" and ($doc.adapter | type == "string") and ($doc.elapsedMs | type == "number")
    and ([$doc.asdus[] | (.type, .typeId, .cot, .negative, .test, .commonAddress, .record, .sequence, .count, .objects) != null] | all)
    and (($doc | has("startdtConfirmed")) | not)'

# station_checks: the operations of the contract against STATION, which
# serves the baseline fixture with its clock as the fixture has it.
station_checks() {
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
}

if role client; then
    STATION="${meter}" STATION_LABEL=meter ON=""
    station_checks
    [[ -n "${VERBOSE:-}" ]] && docker logs "${meter}" >&2
    docker rm -f "${meter}" >/dev/null 2>&1
fi

# ---- the server ----
if role server; then
    bad=$(mktemp -d)
    trap 'rm -rf "${bad}"; cleanup' EXIT
    chmod 755 "${bad}"
    jq '.totals[0].kind = "cumulative"' "${FX}" > "${bad}/fixture.json"
    chmod 644 "${bad}/fixture.json"
    expect_exit 4 "server with an invalid fixture" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json
    expect_exit 4 "server with a missing fixture" "${IMAGE}" server --fixture /no/such/fixture.json
    jq '.totals[0].periods[1].end = "2025-01-01T00:00"' "${FX}" > "${bad}/fixture.json"
    expect_exit 4 "server with period ends out of order" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json
    jq '.station.commonAddress = 300' "${FX}" > "${bad}/fixture.json"
    if jq -e '.caSizes | index(1)' <<<"${caps}" >/dev/null 2>&1; then
        expect_exit 4 "server with a common address beyond --ca-size 1" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json --ca-size 1
    fi

    name=$(start_server "${ADAPTER}")
    if wait_ready "${name}"; then ok "server becomes ready"; else fail "server did not become ready"; finish; exit 1; fi
    first=$(first_event "${name}")
    if jq -e --arg a "${ADAPTER}" --slurpfile fx "${FX}" '.event == "ready" and .adapter == $a and .fixture == "baseline"
            and .commonAddress == $fx[0].station.commonAddress and .standard == "iec60870-5-102" and .mode == "unbalanced"
            and .linkAddress == 1 and (.address | type == "string")' <<<"${first}" >/dev/null 2>&1
    then ok "ready event"; else fail "first stdout line is not the ready event: ${first}"; fi

    if role client; then
        STATION="${name}" STATION_LABEL=server ON="own server: "
        station_checks
        ON=""
        events=$(docker logs "${name}" 2>/dev/null)
        if grep -v '^{' <<<"${events}" | grep -q .; then fail "server stdout has lines that are not JSON"; else ok "server stdout is JSON Lines only"; fi
        # What the station received, as its events say; set-time last.
        for want in \
            '.event == "connection-opened" and (.peer | type == "string")' \
            '.event == "login" and .accepted == true' \
            '.event == "login" and .accepted == false' \
            '.event == "logout"' \
            '.event == "read-product"' \
            '.event == "read-time" and .time == $fx.station.clock' \
            '.event == "read-totals" and .typeId == 122 and .record == 11 and .cot == 7 and .periods == 3' \
            '.event == "read-totals" and .typeId == 123 and .record == 11 and .cot == 7 and .periods == 2' \
            '.event == "read-totals" and .typeId == 122 and .cot == 18' \
            '.event == "read-totals" and .typeId == 122 and .record == 12 and .cot == 16' \
            '.event == "read-totals" and .typeId == 122 and .cot == 17' \
            '.event == "refused" and .typeId == 122 and .commonAddress == 2 and .cot == 16' \
            '.event == "read-tariff" and .typeId == 133 and .register == 134 and .cot == 7 and .records == ([$fx.tariffs[] | select(.register == 134) | .current[]] | length)' \
            '.event == "read-tariff" and .typeId == 134 and .register == 134 and .cot == 7 and .records == 2' \
            '.event == "read-tariff" and .register == 135 and .cot == 16' \
            '.event == "set-time" and .time == "2026-02-03T04:05:06.789" and (.su | type == "boolean")' \
            '.event == "read-time" and .time == "2026-02-03T04:05:06.789"' \
            '.event == "connection-closed" and (.peer | type == "string")'
        do
            if jq -e -s --slurpfile fx "${FX}" "(\$fx[0]) as \$fx | map(select(${want})) | length > 0" <<<"${events}" >/dev/null 2>&1
            then ok "event: ${want}"; else fail "no server event with ${want}"; fi
        done
        if jq -e -s '(map(select(.event == "connection-opened")) | length) == (map(select(.event == "connection-closed")) | length)' <<<"${events}" >/dev/null 2>&1
        then ok "every connection-opened has its connection-closed"; else fail "connection events do not pair up"; fi

        # The field sizes the image lists besides the defaults: a run with
        # one-octet fields on both ends, and a client that does not match.
        if jq -e '(.linkAddressSizes | index(1)) and (.caSizes | index(1))' <<<"${caps}" >/dev/null 2>&1; then
            small=(--link-address-size 1 --ca-size 1 --link-address 3)
            sname=$(SERVER_TAG=small start_server "${ADAPTER}" "${small[@]}")
            if wait_ready "${sname}"; then
                STATION="${sname}" STATION_LABEL=small ON="one-octet link address and common address: "
                client small read-totals --from 2026-01-01T00:00 --to 2026-01-01T05:00 "${small[@]}"
                check "read-totals" "${PERIODS} ${COMMON}"' and $doc.ok and $doc.terminated
                    and got(8) == periods("absolute"; 11; "2026-01-01T00:00"; "2026-01-01T05:00"; 1; 8)'
                client small-mismatch read-time --link-address 3 --timeout-ms 1500
                WANT_EXIT=3 check "a client with two-octet fields gets no link" '$doc.error.code == "link-init-timeout"'
                ON=""
            else
                fail "server with one-octet fields did not become ready"
            fi
            stop_server "${sname}"
        fi
    fi

    # A port that is already taken.
    docker exec "${name}" /usr/local/bin/iec60870-5-interop server --ready-file /tmp/second >/dev/null 2>&1
    code=$?
    if [[ "${code}" != 0 ]]; then ok "second server on a busy port fails (exit ${code})"; else fail "second server on a busy port exited 0"; fi

    docker stop -t 10 "${name}" >/dev/null
    code=$(docker inspect --format '{{.State.ExitCode}}' "${name}")
    last=$(docker logs "${name}" 2>/dev/null | tail -1)
    if [[ "${code}" == 0 ]]; then ok "SIGTERM: exit 0"; else fail "SIGTERM: exit ${code}"; fi
    if jq -e '.event == "stopped"' <<<"${last}" >/dev/null 2>&1; then ok "stopped event"; else fail "last stdout line is not the stopped event: ${last}"; fi
    stop_server "${name}"
fi

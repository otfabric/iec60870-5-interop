#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# The checks on values of IEC 102 (contract 2.2), for every client with every
# server: what the documents carry is the fixture's, and what the station's
# events say it received is what the client sent. Sourced by scripts/interop.sh
# after the cases of tests/iec102/cases.tsv; do not execute.
#
# shellcheck disable=SC2154  # SERVERS, CLIENTS, FIXTURE and the helpers come from interop.sh

# value_check <id> <client> <server> <bug label> <got> <want> <description>
value_check() {
    local id="$1" client="$2" server="$3" label="$4" got="$5" want="$6" what="$7" bug
    local pair="${client}->${server}"
    at_pair "${id}" "${client}" "${server}"
    if [[ "${got}" == "${want}" ]]; then
        ok "${pair}: ${what}"
    elif bug=$(known_bug "${client}" "${server}" "${label}"); then
        hit_known_bug "${pair}" "${label}" "${bug}"
    else
        fail "${pair}: ${what}"
        log "    want ${want}"
        log "    got  ${got}"
    fi
}

# values <id> <needs> <description> <want: jq on the fixture> <got: jq on the document> <operation...>
# runs the operation for every pairing on the shared server.
values() {
    local id="$1" needs="$2" what="$3" expr="$5" want got doc server client
    want=$(jq -cS "$4" "${FIXTURE}")
    shift 5
    for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
        for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
            gated "${client}" "${server}" || continue
            if ! supported "${client}" "${server}" "${needs}"; then
                at_pair "${id}" "${client}" "${server}"; skip "${client}->${server}: ${what}"; continue
            fi
            doc=$(run_client "${client}" "$@" --host "$(shared_server "${server}")")
            keep "iec102-${id}-${client}->${server}" "${doc}"
            got=$(jq -cS "${expr}" <<<"${doc}" 2>/dev/null)
            value_check "${id}" "${client}" "${server}" "$*" "${got}" "${want}" "${what}"
        done
    done
}

KEY=$(jq '.station.accessKey' "${FIXTURE}")
REE=(--profile ree --access-key "${KEY}")
# The integration periods of the fixture as the documents carry them.
PERIODS='def periods($k; $lo; $hi; $first; $last): [.totals[] | select(.kind == $k and .record == 11) | .periods[]
    | select(.end >= $lo and .end <= $hi) | {time: .end, su: false, timeInvalid: false,
      objects: [.totals[] | select(.address >= $first and .address <= $last)]} | select(.objects != [])];'
got_periods() { echo "[.asdus[] | select(.typeId == $1 and .cot == 5) | {time, su, timeInvalid, objects}]"; }
TARIFF='def records($when): [.tariffs[] | select(.register == 134) | .[$when][]];'

values values.read-product "server:productIdentification,client:productIdentification" \
    "read-product returns the fixture's product identification" \
    '[.station.product]' '[.asdus[] | select(.typeId == 71 and .cot == 5) | .objects[] | {standard, manufacturer, product}]' \
    read-product
values values.read-time "server:readTime,client:readTime" "read-time returns the fixture's clock" \
    '[{time: .station.clock, su: false, timeInvalid: false}]' '[.asdus[] | select(.typeId == 72 and .cot == 5) | .objects[]]' \
    read-time
values values.read-totals-absolute "server:integratedTotals,client:integratedTotals" \
    "read-totals absolute returns every period of the fixture" \
    "${PERIODS}"' periods("absolute"; "2026-01-01T00:00"; "2026-01-01T05:00"; 1; 255)' "$(got_periods 8)" \
    read-totals --from 2026-01-01T00:00 --to 2026-01-01T05:00
values values.read-totals-incremental "server:integratedTotals,client:integratedTotals" \
    "read-totals incremental returns every period of the fixture" \
    "${PERIODS}"' periods("incremental"; "2026-01-01T00:00"; "2026-01-01T05:00"; 1; 255)' "$(got_periods 11)" \
    read-totals --kind incremental --from 2026-01-01T00:00 --to 2026-01-01T05:00
values values.read-totals-subset "server:integratedTotals,client:integratedTotals" \
    "read-totals of two periods and addresses 1..3 returns those of the fixture" \
    "${PERIODS}"' periods("incremental"; "2026-01-01T02:00"; "2026-01-01T03:00"; 1; 3)' "$(got_periods 11)" \
    read-totals --kind incremental --from 2026-01-01T02:00 --to 2026-01-01T03:00 --first 1 --last 3
values values.read-tariff-current "server:tariffInformation,client:tariffInformation" \
    "read-tariff returns the fixture's current records, one per ASDU" \
    "${TARIFF}"' [records("current")[] | {count: 1, record: 134, object: .}]' \
    '[.asdus[] | select(.typeId == 135) | {count, record, object: .objects[]}]' \
    read-tariff "${REE[@]}"
values values.read-tariff-stored "server:tariffInformation,client:tariffInformation" \
    "read-tariff of a range returns the fixture's stored records in it" \
    "${TARIFF}"' [records("stored")[] | select(.periodEnd >= "2025-11-15T00:00" and .periodEnd <= "2026-01-01T00:00")]' \
    '[.asdus[] | select(.typeId == 136) | .objects[]]' \
    read-tariff --from 2025-11-15T00:00 --to 2026-01-01T00:00 "${REE[@]}"

# ---- values: set-time sets the clock that a later read-time reads ----
# The station's own event says what it received; the client's mirror is not
# evidence of that.
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:setTime,client:setTime,server:readTime,client:readTime"; then
            at_pair values.set-time-read-back "${client}" "${server}"; skip "${client}->${server}: set-time"; continue
        fi
        name=$(start_server "${server}"); wait_ready "${name}" || exit 1
        doc=$(run_client "${client}" set-time --time 2026-02-03T04:05:06.789 "${REE[@]}" --host "${name}")
        keep "iec102-values-set-time-${client}->${server}" "${doc}"
        after=$(run_client "${client}" read-time --host "${name}")
        keep "iec102-values-set-time-read-${client}->${server}" "${after}"
        value_check values.set-time-read-back "${client}" "${server}" "set-time" \
            "$(jq -c '[.asdus[] | select(.typeId == 72 and .cot == 5) | .objects[].time]' <<<"${after}" 2>/dev/null)" \
            '["2026-02-03T04:05:06.789"]' "a later read-time reads the time that was set"
        value_check values.set-time-event "${client}" "${server}" "set-time" \
            "$(docker logs "${name}" 2>/dev/null | jq -c 'select(.event == "set-time") | .time' 2>/dev/null)" \
            '"2026-02-03T04:05:06.789"' "the station's set-time event has the time sent"
        stop_server "${name}"
    done
done

# ---- values: the station's events say what the client asked for ----
# A fresh server per pairing, so that its events are this client's only.
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:integratedTotals,client:integratedTotals,server:reeSession,client:reeSession"; then
            at_pair values.read-totals-event "${client}" "${server}"; skip "${client}->${server}: events"; continue
        fi
        name=$(start_server "${server}"); wait_ready "${name}" || exit 1
        run_client "${client}" read-totals --kind incremental --from 2026-01-01T02:00 --to 2026-01-01T03:00 \
            --first 1 --last 3 "${REE[@]}" --host "${name}" >/dev/null
        run_client "${client}" connect --profile ree --access-key $((KEY + 1)) --host "${name}" >/dev/null
        events=$(docker logs "${name}" 2>/dev/null)
        value_check values.read-totals-event "${client}" "${server}" "read-totals" \
            "$(jq -c 'select(.event == "read-totals") | [.typeId, .record, .cot, .periods]' <<<"${events}" 2>/dev/null)" \
            '[123,11,7,2]' "the station's read-totals event has the type, record address and periods"
        value_check values.login-events "${client}" "${server}" "connect --profile ree" \
            "$(jq -c 'select(.event == "login" or .event == "logout") | [.event, .accepted]' <<<"${events}" 2>/dev/null | tr -d '\n')" \
            '["login",true]["logout",null]["login",false]' "the station's login and logout events: the key, then a wrong key"
        stop_server "${name}"
    done
done

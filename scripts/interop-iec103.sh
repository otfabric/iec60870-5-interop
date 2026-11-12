#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# The checks on values and sessions of IEC 103 (contract 2.3), for every
# client with every server. Sourced by scripts/interop.sh after the cases; do
# not execute.
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

# ---- values: a general interrogation returns the fixture's indications ----
# Every indication the fixture puts in the interrogation, with its DPI, cause 9
# and the scan number as SIN.
want_gi=$(jq -cS '[.indications[] | select(.gi != false) | {fun, inf, dpi, sin: 5}] | sort_by(.fun, .inf)' "${FIXTURE}")
got_gi='[.asdus[] | select((.typeId == 1 or .typeId == 2) and .cot == 9) | .objects[0] | {fun, inf, dpi, sin}] | sort_by(.fun, .inf)'
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    name=$(shared_server "${server}")
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        doc=$(run_client "${client}" interrogate --scn 5 --host "${name}")
        keep "iec103-values-interrogation-${client}->${server}" "${doc}"
        value_check values.interrogation "${client}" "${server}" "interrogate --scn 5" \
            "$(jq -cS "${got_gi}" <<<"${doc}" 2>/dev/null)" "${want_gi}" "interrogation matches the fixture"
    done
done

# ---- values: the identification after the reset of the communication unit ----
want_id=$(jq -cS '[.station.identification | {fun, inf: 3, col, ascii, software}]' "${FIXTURE}")
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    name=$(shared_server "${server}")
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:identification,client:identification"; then
            at_pair values.identification "${client}" "${server}"; skip "${client}->${server}: identification"; continue
        fi
        doc=$(run_client "${client}" connect --hold-ms 300 --host "${name}")
        keep "iec103-values-identification-${client}->${server}" "${doc}"
        value_check values.identification "${client}" "${server}" "identification" \
            "$(jq -cS '[.asdus[] | select(.typeId == 5 and .cot == 4) | .objects[0] | {fun, inf, col, ascii, software}]' <<<"${doc}" 2>/dev/null)" \
            "${want_id}" "identification after the reset of the communication unit"
    done
done

# ---- values: a general command changes what an interrogation reports ----
# Clause 7.4.4: the acknowledgement (cause 20, SIN the RII; its DPI is not
# significant) and then the change of state (cause 12, the new DPI; its SIN
# is not significant), and a later interrogation reports the new state.
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:generalCommand,client:generalCommand"; then
            at_pair values.command-interrogation "${client}" "${server}"; skip "${client}->${server}: command"; continue
        fi
        name=$(start_server "${server}"); wait_ready "${name}" || exit 1
        doc=$(run_client "${client}" command --fun 160 --inf 17 --dco on --rii 9 --collect-ms 500 --host "${name}")
        keep "iec103-values-command-${client}->${server}" "${doc}"
        value_check values.command-confirmation "${client}" "${server}" "command --fun 160 --inf 17" \
            "$(jq -c '[.asdus[] | select((.typeId == 1 or .typeId == 2) and (.cot == 20 or .cot == 21)) | {cot} + (.objects[0] | {fun, inf, sin})]' <<<"${doc}" 2>/dev/null)" \
            '[{"cot":20,"fun":160,"inf":17,"sin":9}]' "the command is acknowledged with cause 20 and the RII"
        value_check values.command-report "${client}" "${server}" "command --fun 160 --inf 17" \
            "$(jq -c '[.asdus[] | select((.typeId == 1 or .typeId == 2) and .cot == 12) | .objects[0] | {fun, inf, dpi}]' <<<"${doc}" 2>/dev/null)" \
            '[{"fun":160,"inf":17,"dpi":2}]' "the change of state follows with cause 12 and the new state"
        after=$(run_client "${client}" interrogate --host "${name}" \
            | jq -c '[.asdus[] | select(.cot == 9) | .objects[0] | select(.fun == 160 and .inf == 17) | .dpi]' 2>/dev/null)
        value_check values.command-interrogation "${client}" "${server}" "command --fun 160 --inf 17" \
            "${after}" "[2]" "the command is visible to a later interrogation"
        stop_server "${name}"
    done
done

# ---- values: the measurands are sent as cyclic class 2 data ----
want_meas=$(jq -cS '[.measurands[]? | {type, fun, inf, values: [.values[] | {value, ov: (.ov // false), er: (.er // false)}]}] | sort_by(.fun, .inf)' "${FIXTURE}")
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    name=$(shared_server "${server}")
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:measurands,client:measurands"; then
            at_pair values.measurands "${client}" "${server}"; skip "${client}->${server}: measurands"; continue
        fi
        doc=$(run_client "${client}" monitor --duration-ms 2000 --host "${name}")
        keep "iec103-values-measurands-${client}->${server}" "${doc}"
        value_check values.measurands "${client}" "${server}" "monitor measurands" \
            "$(jq -cS '[.asdus[] | select(.cot == 2 and (.typeId == 3 or .typeId == 9)) | .typeId as $t | .objects[0] | {type: $t, fun, inf, values}] | unique_by(.fun, .inf) | sort_by(.fun, .inf)' <<<"${doc}" 2>/dev/null)" \
            "${want_meas}" "the measurands of the fixture arrive as class 2 data"
    done
done

# ---- values (contract 2.3): generic data of the fixture ----
# Clause 7.4.8.6.2: a read of attribute KOD 1 (actual value) of each entry
# answers its GDD (type, size, NUMBER 1) and value; KOD 10 its description.
want_gen=$(jq -cS '[.generic[]? | {gin: "\(.gin.group).\(.gin.entry)", kod: 1, type, size, number: 1, values: [.value]},
    {gin: "\(.gin.group).\(.gin.entry)", kod: 10, type: 1, size: (.description | length), number: 1, values: [.description]}]' "${FIXTURE}")
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    name=$(shared_server "${server}")
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:genericServices,client:genericServices"; then
            at_pair values.generic-read "${client}" "${server}"; skip "${client}->${server}: generic read"; continue
        fi
        got='[]'
        while read -r gin; do
            for kod in 1 10; do
                doc=$(run_client "${client}" generic-read --gin "${gin}" --kod "${kod}" --host "${name}")
                keep "iec103-values-generic-read-${gin}-${kod}-${client}->${server}" "${doc}"
                got=$(jq -cS --argjson d "$(jq -c '.generic.sets // []' <<<"${doc}" 2>/dev/null || echo '[]')" '. + $d' <<<"${got}")
            done
        done < <(jq -r '.generic[]? | "\(.gin.group).\(.gin.entry)"' "${FIXTURE}")
        value_check values.generic-read "${client}" "${server}" "generic-read" "${got}" "${want_gen}" \
            "generic reads return the fixture's values and descriptions"
    done
done

# Clause 7.4.8.6.3: a write with execution (INF 250) to a writable entry is
# acknowledged with cause 40 and a later read returns the new value.
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:genericServices,client:genericServices"; then
            at_pair values.generic-write "${client}" "${server}"; skip "${client}->${server}: generic write"; continue
        fi
        name=$(start_server "${server}"); wait_ready "${name}" || exit 1
        doc=$(run_client "${client}" generic-write --gin 1.2 --type 7 --size 4 --value 2.25 --host "${name}")
        keep "iec103-values-generic-write-${client}->${server}" "${doc}"
        after=$(run_client "${client}" generic-read --gin 1.2 --host "${name}" | jq -c '[.generic.sets[]?.values[]?]' 2>/dev/null)
        value_check values.generic-write "${client}" "${server}" "generic-write" \
            "$(jq -c '[.confirmations[] | .cot]' <<<"${doc}" 2>/dev/null) ${after}" "[40] [2.25]" \
            "a write with execution is acknowledged with cause 40 and visible to a later read"
        stop_server "${name}"
    done
done

# ---- values (contract 2.3): the recorded disturbance of the fixture ----
# Clause 7.3.1.11: FAN, SOF and the time of the fault of each record.
want_list=$(jq -cS '[.disturbances[]? | {fan, sof: {tp: (.sof.tp // false), tm: (.sof.tm // false), test: (.sof.test // false), otev: (.sof.otev // false)}, time}]' "${FIXTURE}")
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    name=$(shared_server "${server}")
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:disturbanceList,client:disturbanceList"; then
            at_pair values.disturbance-list "${client}" "${server}"; skip "${client}->${server}: disturbance list"; continue
        fi
        doc=$(run_client "${client}" disturbance-list --host "${name}")
        keep "iec103-values-disturbance-list-${client}->${server}" "${doc}"
        value_check values.disturbance-list "${client}" "${server}" "disturbance-list" \
            "$(jq -cS '.disturbances' <<<"${doc}" 2>/dev/null)" "${want_list}" "the list of recorded disturbances is the fixture's"
    done
done

# Clause 7.4.7: the whole transfer of FAN 7 returns its description (ASDU
# 26), its tags with their positions (ASDU 29) and every channel (ASDU 27);
# a second check compares the values of the channels (ASDU 30). NOF and
# TOV are not compared: not every library reports them.
want_dist=$(jq -cS '.disturbances[0] | {fan, noc: (.channels | length), noe: (.channels[0].values | length), interval, start,
    tags: [.tags[] | {pos, fun, inf, dpi}], channels: [.channels[] | {acc, rpv, rsv, rfa}]}' "${FIXTURE}")
want_vals=$(jq -cS '[.disturbances[0].channels[] | {acc, values}]' "${FIXTURE}")
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        if ! supported "${client}" "${server}" "server:disturbanceDataServer,client:disturbanceData"; then
            at_pair values.disturbance-transfer "${client}" "${server}"; skip "${client}->${server}: disturbance transfer"; continue
        fi
        name=$(start_server "${server}"); wait_ready "${name}" || exit 1
        doc=$(run_client "${client}" disturbance-transfer --fan 7 --host "${name}")
        keep "iec103-values-disturbance-transfer-${client}->${server}" "${doc}"
        value_check values.disturbance-transfer "${client}" "${server}" "disturbance-transfer" \
            "$(jq -cS '.disturbance | {fan, noc, noe, interval, start, tags, channels: [.channels[] | {acc, rpv, rsv, rfa}]}' <<<"${doc}" 2>/dev/null)" \
            "${want_dist}" "the transferred disturbance has the fixture's description, tags and channels"
        value_check values.disturbance-values "${client}" "${server}" "disturbance-transfer values" \
            "$(jq -cS '[.disturbance.channels[] | {acc, values}]' <<<"${doc}" 2>/dev/null)" "${want_vals}" \
            "the transferred channels have the fixture's values"
        stop_server "${name}"
    done
done

# ---- sessions: an idle line stays up, and the next connection works ----
for server in ${SERVERS[@]+"${SERVERS[@]}"}; do
    name=$(shared_server "${server}")
    for client in ${CLIENTS[@]+"${CLIENTS[@]}"}; do
        gated "${client}" "${server}" || continue
        pair="${client}->${server}"
        at_pair session.idle-hold "${client}" "${server}"
        doc=$(run_client "${client}" connect --hold-ms 3000 --host "${name}")
        if jq -e '.ok and .connected' <<<"${doc}" >/dev/null 2>&1; then ok "${pair}: idle line stays up for 3 s"
        else fail "${pair}: idle line: $(jq -r "${SUMMARY}" <<<"${doc}" 2>/dev/null)"; fi
        at_pair session.after-idle "${client}" "${server}"
        doc=$(run_client "${client}" interrogate --scn 1 --host "${name}")
        if jq -e '.ok and .terminated' <<<"${doc}" >/dev/null 2>&1; then ok "${pair}: interrogation on the next connection"
        else fail "${pair}: interrogation after an idle connection: $(jq -r "${SUMMARY}" <<<"${doc}" 2>/dev/null)"; fi
    done
done

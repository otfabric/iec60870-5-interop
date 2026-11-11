#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# The smoke checks of an IEC 103 image (contract 2.1: FT 1.2 over a TCP byte
# stream, unbalanced, one-octet link address). Sourced by scripts/smoke.sh
# after the checks every image shares (capabilities, fixtures); do not
# execute.
#
# An image may be a client only. Its client is then checked against the
# server of SMOKE_SERVER (default vitusdong, the IEC 103 image with a
# server), which must be built.
#
# shellcheck disable=SC2154  # ADAPTER, IMAGE, FIXTURES, caps come from smoke.sh

SMOKE_SERVER="${SMOKE_SERVER:-vitusdong}"
role() { jq -e --arg r "$1" '.roles[$r] == true' <<<"${caps}" >/dev/null 2>&1; }

if jq -e '.transport == "tcp-stream" and .linkModes == ["unbalanced"] and (.asduTypes.monitor | type == "array")
        and (.asduTypes.control | type == "array") and .features.balancedMode == false' <<<"${caps}" >/dev/null 2>&1
then ok "capabilities: tcp-stream, unbalanced only, asduTypes"; else fail "capabilities of an IEC 103 image: ${caps}"; fi
for f in generalCommand identification measurands broadcastClockSync genericServices disturbanceData; do
    if jq -e --arg f "${f}" '.features[$f] | type == "boolean"' <<<"${caps}" >/dev/null 2>&1
    then ok "capabilities: feature ${f}"; else fail "capabilities lack the IEC 103 feature ${f}"; fi
done

# ---- usage errors never touch the network ----
expect_exit 2 "unknown command" "${IMAGE}" frobnicate
expect_exit 2 "client without --host" "${IMAGE}" client interrogate
expect_exit 2 "client with an unknown operation" "${IMAGE}" client frobnicate --host nowhere
expect_exit 2 "client with an operation of IEC 101/104 only" "${IMAGE}" client counter-interrogate --host nowhere
expect_exit 2 "client read (no information object addresses in IEC 103)" "${IMAGE}" client read --host nowhere --ioa 1
expect_exit 2 "client with an unknown flag" "${IMAGE}" client interrogate --host nowhere --frobnicate 1
expect_exit 2 "client with a field size flag of IEC 101" "${IMAGE}" client interrogate --host nowhere --cot-size 1
expect_exit 2 "client with --originator-address" "${IMAGE}" client interrogate --host nowhere --originator-address 1
expect_exit 2 "client in balanced mode" "${IMAGE}" client interrogate --host nowhere --link-mode balanced
expect_exit 2 "client with the broadcast link address" "${IMAGE}" client interrogate --host nowhere --link-address 255
expect_exit 2 "interrogate with a scan number beyond an octet" "${IMAGE}" client interrogate --host nowhere --scn 256
expect_exit 2 "command without --dco" "${IMAGE}" client command --host nowhere --fun 160 --inf 16
expect_exit 2 "command without --fun" "${IMAGE}" client command --host nowhere --inf 16 --dco on
expect_exit 2 "command with a DCO of four" "${IMAGE}" client command --host nowhere --fun 160 --inf 16 --dco 4
expect_exit 2 "command with --type (IEC 101/104)" "${IMAGE}" client command --host nowhere --fun 160 --inf 16 --dco on --type C_SC_NA_1
expect_exit 2 "clock-sync with a time that is no time" "${IMAGE}" client clock-sync --host nowhere --time yesterday
if ! jq -e '.features.broadcastClockSync == true' <<<"${caps}" >/dev/null 2>&1; then
    expect_exit 2 "clock-sync --broadcast without the feature" "${IMAGE}" client clock-sync --host nowhere --broadcast
fi

if role server; then
    expect_exit 2 "server with an unknown flag" "${IMAGE}" server --frobnicate
    expect_exit 2 "server with an APCI flag" "${IMAGE}" server --k 4
    expect_exit 2 "server in balanced mode" "${IMAGE}" server --link-mode balanced
    expect_exit 2 "server with the broadcast link address" "${IMAGE}" server --link-address 255
    expect_exit 2 "server with a field size flag of IEC 101" "${IMAGE}" server --ioa-size 2

    # ---- an invalid fixture stops the server before it listens ----
    bad=$(mktemp -d)
    trap 'rm -rf "${bad}"; cleanup' EXIT
    chmod 755 "${bad}"
    jq '.commands += [{"fun": 160, "inf": 99}]' "${FIXTURES}/baseline/fixture.json" > "${bad}/fixture.json"
    chmod 644 "${bad}/fixture.json"
    expect_exit 4 "server with a command whose indication is missing" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json
    jq '.station.identification.ascii = "SHORT"' "${FIXTURES}/baseline/fixture.json" > "${bad}/fixture.json"
    expect_exit 4 "server with an identification of five characters" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json
    jq '.measurands[0].values[0].value = 5000' "${FIXTURES}/baseline/fixture.json" > "${bad}/fixture.json"
    expect_exit 4 "server with a measurand beyond 13 bits" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json
    expect_exit 4 "server with a missing fixture" "${IMAGE}" server --fixture /no/such/fixture.json
else
    expect_exit 2 "server of a client-only image" "${IMAGE}" server
fi

# ---- a connection that cannot be made ----
if role client; then
    doc=$(run_client "${ADAPTER}" interrogate --host "no-such-host-${RUN_ID}" --connect-timeout-ms 1500); CLIENT_EXIT=$?
    if [[ "${CLIENT_EXIT}" == 3 ]] && jq -e '.ok == false and .error.code == "connect-failed" and .connected == false' <<<"${doc}" >/dev/null 2>&1
    then ok "connect failure: exit 3 and a result document"; else fail "connect failure: exit ${CLIENT_EXIT}, ${doc}"; fi
fi

# ---- the server, or the peer server of a client-only image ----
if role server; then
    server_adapter="${ADAPTER}"
else
    server_adapter="${SMOKE_SERVER}"
fi
client_adapter="${ADAPTER}"
role client || client_adapter="${SMOKE_CLIENT:-goiecp5}"

name=$(start_server "${server_adapter}")
if wait_ready "${name}"; then ok "server ${server_adapter} becomes ready"; else fail "server ${server_adapter} did not become ready"; finish; exit 1; fi
if role server; then
    first=$(first_event "${name}")
    if jq -e --arg a "${ADAPTER}" '.event == "ready" and .adapter == $a and .fixture == "baseline" and .commonAddress == 1
            and (.address | type == "string") and .standard == "iec60870-5-103" and .mode == "unbalanced" and .linkAddress == 1' <<<"${first}" >/dev/null 2>&1
    then ok "ready event"; else fail "first stdout line is not the ready event: ${first}"; fi
fi

# A station that does not answer: another link address.
doc=$(run_client "${client_adapter}" interrogate --host "${name}" --link-address 9 --timeout-ms 1500); CLIENT_EXIT=$?
if [[ "${CLIENT_EXIT}" == 3 ]] && jq -e '.ok == false and .error.code == "link-init-timeout" and .connected == false' <<<"${doc}" >/dev/null 2>&1
then ok "link start-up failure: exit 3, link-init-timeout"; else fail "link start-up failure: exit ${CLIENT_EXIT}, ${doc}"; fi

doc=$(run_client "${client_adapter}" interrogate --scn 3 --host "${name}"); CLIENT_EXIT=$?
if [[ "${CLIENT_EXIT}" == 0 ]] && jq -e --arg a "${client_adapter}" '
        .schemaVersion == "1.0" and .adapter == $a and .operation == "interrogate" and .ok == true
        and .error == null and .connected and .terminated and (has("startdtConfirmed") | not)
        and .confirmations == [] and (.elapsedMs | type == "number")
        and ([.asdus[] | select(.typeId == 8) | .objects[0].scn] == [3])
        and ([.asdus[] | (.type, .typeId, .cot, .negative, .test, .originator, .commonAddress, .sequence, .count, .objects) != null] | all)
        and ([.asdus[] | .objects[] | (.fun | type == "number") and (.inf | type == "number")] | all)' <<<"${doc}" >/dev/null 2>&1
then ok "client result document"; else fail "interrogate: exit ${CLIENT_EXIT}, ${doc}"; fi
if [[ $(wc -l <<<"${doc}") -eq 1 ]]; then ok "client result is one line"; else fail "client printed more than one line on stdout"; fi

if role server; then
    # One line, one control system: a second connection gets nothing.
    run_client "${client_adapter}" connect --hold-ms 3000 --host "${name}" >/dev/null &
    holder=$!
    sleep 1.5
    doc=$(run_client "${client_adapter}" interrogate --host "${name}" --timeout-ms 1000); CLIENT_EXIT=$?
    wait "${holder}"
    if [[ "${CLIENT_EXIT}" != 0 ]] && jq -e '.ok == false and .connected == false' <<<"${doc}" >/dev/null 2>&1
    then ok "a second connection is not served (exit ${CLIENT_EXIT})"; else fail "a second connection was served: exit ${CLIENT_EXIT}, ${doc}"; fi

    run_client "${client_adapter}" command --fun 160 --inf 17 --dco on --rii 5 --host "${name}" --timeout-ms 1500 >/dev/null
    run_client "${client_adapter}" clock-sync --time 2026-10-09T15:35:12.345Z --host "${name}" >/dev/null
    sleep 0.3
    events=$(docker logs "${name}" 2>/dev/null)
    if grep -v '^{' <<<"${events}" | grep -q .; then fail "server stdout has lines that are not JSON"; else ok "server stdout is JSON Lines only"; fi
    for want in \
        '.event == "connection-opened" and (.peer | type == "string")' \
        '.event == "interrogation" and .scn == 3 and .commonAddress == 1 and .accepted' \
        '.event == "command" and .fun == 160 and .inf == 17 and .dco == 2 and .rii == 5 and .outcome == "executed"' \
        '.event == "clock-sync" and .time == "2026-10-09T15:35:12.345Z" and .broadcast == false' \
        '.event == "connection-closed"'
    do
        if jq -e -s "map(select(${want})) | length > 0" <<<"${events}" >/dev/null 2>&1; then ok "event: ${want}"; else fail "no server event with ${want}"; fi
    done

    docker stop -t 10 "${name}" >/dev/null
    code=$(docker inspect --format '{{.State.ExitCode}}' "${name}")
    last=$(docker logs "${name}" 2>/dev/null | tail -1)
    if [[ "${code}" == 0 ]]; then ok "SIGTERM: exit 0"; else fail "SIGTERM: exit ${code}"; fi
    if jq -e '.event == "stopped"' <<<"${last}" >/dev/null 2>&1; then ok "stopped event"; else fail "last stdout line is not the stopped event: ${last}"; fi
    stop_server "${name}"

    # ---- a port that is already taken ----
    name=$(start_server "${ADAPTER}"); wait_ready "${name}"
    docker exec "${name}" /usr/local/bin/iec60870-5-interop server --ready-file /tmp/second >/dev/null 2>&1
    code=$?
    if [[ "${code}" != 0 ]]; then ok "second server on a busy port fails (exit ${code})"; else fail "second server on a busy port exited 0"; fi
fi
stop_server "${name}"

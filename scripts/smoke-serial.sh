#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# The smoke checks of an IEC 101/102/103 image (contract 2.0, FT 1.2 over a
# TCP byte stream). Sourced by scripts/smoke.sh after the checks every image
# shares (capabilities, fixtures); do not execute.
#
# shellcheck disable=SC2154  # ADAPTER, IMAGE, FIXTURES, caps come from smoke.sh

# Every server and client of the run has LINK_FLAGS (the link mode) except
# where a check is about the flags themselves.
mode_flags=("${LINK_FLAGS[@]}")

if jq -e '.transport == "tcp-stream" and (.linkModes | type == "array" and length > 0)' <<<"${caps}" >/dev/null 2>&1
then ok "capabilities: transport and linkModes"; else fail "capabilities lack transport tcp-stream or linkModes"; fi
if jq -e --arg m "${LINK_MODE}" '.linkModes | index($m) != null' <<<"${caps}" >/dev/null 2>&1
then ok "capabilities: link mode ${LINK_MODE}"; else fail "capabilities do not list the link mode ${LINK_MODE}"; fi

# ---- usage errors never touch the network ----
expect_exit 2 "unknown command" "${IMAGE}" frobnicate
expect_exit 2 "client without --host" "${IMAGE}" client interrogate
expect_exit 2 "client with an unknown operation" "${IMAGE}" client frobnicate --host nowhere
expect_exit 2 "client with an operation of IEC 104 only" "${IMAGE}" client file-get --host nowhere --ioa 1
expect_exit 2 "client with an unknown flag" "${IMAGE}" client interrogate --host nowhere --frobnicate 1
expect_exit 2 "command without --value" "${IMAGE}" client command --host nowhere --type C_SC_NA_1 --ioa 500
expect_exit 2 "command with a bad value" "${IMAGE}" client command --host nowhere --type C_SE_NB_1 --ioa 504 --value 99999
expect_exit 2 "command with --with-time (no CP56Time2a commands in IEC 101)" "${IMAGE}" client command --host nowhere --type C_SC_NA_1 --ioa 500 --value true --with-time
expect_exit 2 "server with an unknown flag" "${IMAGE}" server --frobnicate
expect_exit 2 "server with an APCI flag" "${IMAGE}" server --k 4
expect_exit 2 "server with an unknown link mode" "${IMAGE}" server --link-mode sideways
expect_exit 2 "client with an unknown link mode" "${IMAGE}" client interrogate --host nowhere --link-mode sideways
expect_exit 2 "link address size 0 in unbalanced mode" "${IMAGE}" server --link-mode unbalanced --link-address-size 0
expect_exit 2 "link address size 3" "${IMAGE}" server --link-address-size 3
expect_exit 2 "link address beyond its size" "${IMAGE}" server --link-address 255
expect_exit 2 "cause of transmission of 3 octets" "${IMAGE}" server --cot-size 3
expect_exit 2 "common address of 3 octets" "${IMAGE}" client interrogate --host nowhere --ca-size 3
expect_exit 2 "information object address of 4 octets" "${IMAGE}" client interrogate --host nowhere --ioa-size 4
expect_exit 2 "remote link address in unbalanced mode" "${IMAGE}" client interrogate --host nowhere --link-mode unbalanced --remote-link-address 2
expect_exit 2 "client address that does not fit --ioa-size" "${IMAGE}" client read --host nowhere --ioa 300 --ioa-size 1

# ---- an invalid fixture stops the server before it listens ----
bad=$(mktemp -d)
trap 'rm -rf "${bad}"; cleanup' EXIT
chmod 755 "${bad}"
jq '.commands[0].target = 300' "${FIXTURES}/baseline/fixture.json" > "${bad}/fixture.json"
chmod 644 "${bad}/fixture.json"
expect_exit 4 "server with an invalid fixture" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/fixture.json "${mode_flags[@]}"
expect_exit 4 "server with a missing fixture" "${IMAGE}" server --fixture /no/such/fixture.json "${mode_flags[@]}"
expect_exit 4 "server whose fixture does not fit --ioa-size 1" "${IMAGE}" server --ioa-size 1 "${mode_flags[@]}"

# ---- a connection that cannot be made ----
doc=$(run_client "${ADAPTER}" interrogate --host "no-such-host-${RUN_ID}" --connect-timeout-ms 1500); CLIENT_EXIT=$?
if [[ "${CLIENT_EXIT}" == 3 ]] && jq -e '.ok == false and .error.code == "connect-failed" and .connected == false' <<<"${doc}" >/dev/null 2>&1
then ok "connect failure: exit 3 and a result document"; else fail "connect failure: exit ${CLIENT_EXIT}, ${doc}"; fi

# ---- server: readiness, events, operations, clean stop ----
name=$(start_server "${ADAPTER}")
if wait_ready "${name}"; then ok "server becomes ready"; else fail "server did not become ready"; finish; exit 1; fi
first=$(first_event "${name}")
if jq -e --arg a "${ADAPTER}" --arg s "iec60870-5-${STANDARD#iec}" --arg m "${LINK_MODE}" '
        .event == "ready" and .adapter == $a and .fixture == "baseline" and .commonAddress == 1
        and (.address | type == "string") and .standard == $s and .mode == $m and .linkAddress == 1' <<<"${first}" >/dev/null 2>&1
then ok "ready event"; else fail "first stdout line is not the ready event: ${first}"; fi

# A station that does not answer: a link address field of another size, so no
# frame is valid for the other side. (Another link address is not enough: a
# balanced lib60870 station accepts frames for any address.) The link
# start-up fails, which is exit 3 like a connection that cannot be made.
doc=$(run_client "${ADAPTER}" interrogate --host "${name}" --link-address-size 2 --timeout-ms 1500); CLIENT_EXIT=$?
if [[ "${CLIENT_EXIT}" == 3 ]] && jq -e '.ok == false and .error.code == "link-init-timeout" and .connected == false' <<<"${doc}" >/dev/null 2>&1
then ok "link start-up failure: exit 3, link-init-timeout"; else fail "link start-up failure: exit ${CLIENT_EXIT}, ${doc}"; fi

doc=$(run_client "${ADAPTER}" interrogate --host "${name}"); CLIENT_EXIT=$?
if [[ "${CLIENT_EXIT}" == 0 ]] && jq -e --arg a "${ADAPTER}" '
        .schemaVersion == "1.0" and .adapter == $a and .operation == "interrogate" and .ok == true
        and .error == null and .connected and .terminated and (has("startdtConfirmed") | not)
        and (.confirmations == [{"cot":7,"negative":false}]) and (.elapsedMs | type == "number")
        and ([.asdus[] | (.type, .typeId, .cot, .negative, .test, .originator, .commonAddress, .sequence, .count, .objects) != null] | all)' <<<"${doc}" >/dev/null 2>&1
then ok "client result document"; else fail "interrogate: exit ${CLIENT_EXIT}, ${doc}"; fi
if [[ $(wc -l <<<"${doc}") -eq 1 ]]; then ok "client result is one line"; else fail "client printed more than one line on stdout"; fi

# One line, one controlling station: a second connection gets nothing.
run_client "${ADAPTER}" connect --hold-ms 3000 --host "${name}" >/dev/null &
holder=$!
sleep 1
doc=$(run_client "${ADAPTER}" interrogate --host "${name}" --timeout-ms 1000); CLIENT_EXIT=$?
wait "${holder}"
if [[ "${CLIENT_EXIT}" != 0 ]] && jq -e '.ok == false and .connected == false' <<<"${doc}" >/dev/null 2>&1
then ok "a second connection is not served (exit ${CLIENT_EXIT})"; else fail "a second connection was served: exit ${CLIENT_EXIT}, ${doc}"; fi

run_client "${ADAPTER}" command --type C_SC_NA_1 --ioa 500 --value false --host "${name}" >/dev/null
run_client "${ADAPTER}" clock-sync --time 2026-10-09T15:35:12.345Z --host "${name}" >/dev/null
sleep 0.3
events=$(docker logs "${name}" 2>/dev/null)
if grep -v '^{' <<<"${events}" | grep -q .; then fail "server stdout has lines that are not JSON"; else ok "server stdout is JSON Lines only"; fi
for want in \
    '.event == "connection-opened" and (.peer | type == "string")' \
    '.event == "interrogation" and .qoi == 20 and .commonAddress == 1 and .accepted' \
    '.event == "command" and .type == "C_SC_NA_1" and .ioa == 500 and .value == false and .select == false and .cot == 6 and .outcome == "executed"' \
    '.event == "clock-sync" and .time == "2026-10-09T15:35:12.345Z"' \
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

# ---- field sizes other than the defaults ----
name=$(SERVER_TAG=sizes start_server "${ADAPTER}" --link-address-size 2 --link-address 300 --cot-size 2 --ca-size 2 --ioa-size 3)
if wait_ready "${name}"; then
    doc=$(run_client "${ADAPTER}" interrogate --host "${name}" --link-address-size 2 --link-address 300 --cot-size 2 \
        --ca-size 2 --ioa-size 3 --originator-address 9 --common-address 65535); CLIENT_EXIT=$?
    if [[ "${CLIENT_EXIT}" == 0 ]] && jq -e '.ok and ([.asdus[] | select(.cot == 20)] | length > 0)
            and ([.asdus[] | .originator] | all(. == 9)) and ([.asdus[] | .commonAddress] | all(. == 1))' <<<"${doc}" >/dev/null 2>&1
    then ok "two-octet link address, cause and common address, three-octet object address"
    else fail "field sizes 2/2/2/3: exit ${CLIENT_EXIT}, $(jq -r "${SUMMARY}" <<<"${doc}" 2>/dev/null)"; fi
else
    fail "server with field sizes 2/2/2/3 did not become ready"
fi
stop_server "${name}"

# ---- a port that is already taken ----
name=$(start_server "${ADAPTER}"); wait_ready "${name}"
docker exec "${name}" /usr/local/bin/iec60870-5-interop server --ready-file /tmp/second >/dev/null 2>&1
code=$?
if [[ "${code}" != 0 ]]; then ok "second server on a busy port fails (exit ${code})"; else fail "second server on a busy port exited 0"; fi
stop_server "${name}"

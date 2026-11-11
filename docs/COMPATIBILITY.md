# Compatibility

What the self-tests of this repository showed, with exact counts. A count is
the number of outcomes in `reports/results.json` (interop-result-v1); only
`pass` passes. Every run below was made locally on 2026-11-11 on Docker for
macOS (linux/arm64 engine) with the `:dev` images built from this commit's
sources. Nothing has run on amd64 or in CI yet.

Reproduce: `make images`, then `make documents` (or the commands per row).

`reports/results.json` of these runs: 4014 pass, 0 fail, 583 unsupported,
44 known-difference, 0 infra-error. The 7 capability documents and 1647
client result documents kept in `reports/` validate against `schemas/`
(`scripts/validate-documents.py`, 0 errors). The IEC 101 rows are the final
runs on the committed sources; the IEC 104 images were built before the last
edit of THIRD_PARTY_NOTICES.md (which they copy to `/licenses`), with
otherwise identical sources.

## IEC 60870-5-101

| Run | Command | pass | fail | other |
|-----|---------|-----:|-----:|------:|
| smoke lib60870, unbalanced | `LINK_MODE=unbalanced scripts/smoke.sh iec101 lib60870` | 46 | 0 | 0 |
| smoke lib60870, balanced | `LINK_MODE=balanced scripts/smoke.sh iec101 lib60870` | 46 | 0 | 0 |
| smoke cbsigma, unbalanced | `LINK_MODE=unbalanced scripts/smoke.sh iec101 cbsigma` | 46 | 0 | 0 |
| smoke cbsigma, balanced | `LINK_MODE=balanced scripts/smoke.sh iec101 cbsigma` | 46 | 0 | 0 |
| cross-stack, unbalanced | `LINK_MODE=unbalanced scripts/interop.sh iec101 lib60870 cbsigma` | 245 | 0 | 0 |
| cross-stack, balanced | `LINK_MODE=balanced scripts/interop.sh iec101 lib60870 cbsigma` | 245 | 0 | 0 |

A cross-stack run is every client against every server (lib60870 →
lib60870, cbsigma → lib60870, lib60870 → cbsigma, cbsigma → cbsigma) on the
31 cases of `tests/iec101/cases.tsv`: 124 outcome checks plus 93 checks that
all four pairings return the same normalized result document, then 20 value
checks (interrogation and counter interrogation return the fixture, a set
point is reported and read back, a single command shows in the next
interrogation) and 8 session checks (a line idle for 3 s stays up; the next
connection is served). Both cross-stack runs were repeated once more without
keeping documents, with the same result (245/0 each).

So in both link modes lib60870-C v2.4.1 and cB-Sigma/iec60870 v0.3.0 agree,
in both roles, on: link start-up, general and counter interrogation
(including the broadcast common address 255 and the refusal of an unknown
one with cause 46), read (and cause 47 for an unknown address), clock
synchronization, the test command C_TS_NA_1, direct, select, select-before-
operate and cancelled commands of all six command types, the refusals of the
fixture (select required, invalid double and step states, unknown object),
and the spontaneous M_*_TB_1 report after a command.

The smoke tests also exercise, per adapter against itself: two-octet link
address, cause of transmission (with originator address 9 echoed) and common
address with a three-octet object address; a link that does not start
(`link-init-timeout`, exit 3); a second connection while one is served
(refused).

### Not shown by these runs

- The pair is not two fully independent oracles: cB-Sigma's IEC 101 link was
  debugged against lib60870-C (docs/REFERENCE_STACKS.md). A third peer
  (digitalpetri) is planned.
- Non-default field sizes were not run across stacks, only each adapter
  against itself.
- Balanced mode with distinct link addresses (`--remote-link-address` other
  than `--link-address`): cbsigma cannot, and no lib60870 pairing ran it.
- `--link-address-size 0` was not run.
- Interrogation groups and qualities on M_BO_NA_1 have no IEC 101 fixture yet.

### Observations

- A balanced lib60870 station (and station A) accepts frames whatever link
  address they carry; cB-Sigma ignores frames for another address. Both
  readings are possible on a point-to-point balanced line, so this is not
  listed as a defect.
- lib60870's CS101 slave keeps its link state machine and queues across what
  is, for it, one serial port. With one slave for the life of the process, a
  balanced lib60870 station lost the first ASDU (the confirmation) of most
  operations after the first connection, against a lib60870 controlling
  station; the likely cause is link state (frame count bit, primary link
  state) carried over from the previous connection. The adapter now creates a new slave for
  every connection (contract: every connection is a line that has just come
  up). This was a harness issue, not a defect of lib60870.
- Before that change and a client rebuild, one manual run of lib60870 (client)
  against cbsigma (server), balanced, lost the first ASDU of most operations.
  It did not reproduce afterwards: three manual repeats and three full
  cross-stack runs passed. The cause is not established.

### Known upstream defects

None found for IEC 101 (`tests/iec101/known-bugs.tsv` is empty).

## IEC 60870-5-104

The five adapters of iec104-interop v0.6.0, rebuilt here under the new
names (entrypoint, readiness path, `standard` in capabilities); protocol
code unchanged. Status in versions.yaml: experimental, because these
images have not been released from this repository yet.

| Run | Command | pass | fail | known bug | unsupported |
|-----|---------|-----:|-----:|----------:|------------:|
| smoke lib60870 | `scripts/smoke.sh iec104 lib60870` | 55 | 0 | 0 | 0 |
| smoke openmuc | `scripts/smoke.sh iec104 openmuc` | 55 | 0 | 0 | 0 |
| smoke wendy512 | `scripts/smoke.sh iec104 wendy512` | 51 | 0 | 0 | 0 |
| smoke cbsigma | `scripts/smoke.sh iec104 cbsigma` | 59 | 0 | 0 | 0 |
| smoke part5 | `scripts/smoke.sh iec104 part5` | 49 | 0 | 0 | 0 |
| cross-stack, 5 × 5 pairings | `scripts/interop.sh iec104 lib60870 openmuc wendy512 cbsigma part5` | 3071 | 0 | 44 | 583 |

The smoke counts differ per adapter because the checks of TLS, security and
redundancy run only for images that declare those features. "unsupported"
are cases a pairing skips because one side does not declare the feature;
they are not passes.

### Known upstream defects

| Stack | Version | Defect | Evidence |
|-------|---------|--------|----------|
| OpenMUC j60870 | 1.8.0 | Cannot decode C_RD_NA_1 and closes the connection when it receives one | reproduced in this run (44 known-difference outcomes, every pairing with openmuc on a read); `tests/iec104/known-bugs.tsv`; https://github.com/gythialy/j60870/pull/75 |

Carried over from iec104-interop, where it was first found; this run
confirms it still reproduces with the same pin.

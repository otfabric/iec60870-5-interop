# Roadmap

What exists is in [COMPATIBILITY.md](COMPATIBILITY.md); candidates are
assessed in [REFERENCE_STACKS.md](REFERENCE_STACKS.md). Nothing here is done.

## IEC 60870-5-101

1. Done in 0.4.0: digitalpetri/iec60870 as the third adapter, and the
   field sizes in the cross-stack run (contract 2.3: 2/2/2/3, 1/2/1/3,
   2/1/2/2 and, balanced, no link address field).
2. More cases: interrogation groups (an IEC 101 `extended` fixture), a
   one-octet information object address (needs a fixture whose addresses fit
   it), distinct balanced addresses between stacks that support them
   (lib60870 only so far).
3. Captures: a `tshark -d tcp.port==2401,iec60870_101` decode of each pairing
   as a passive check of the frames.

## IEC 60870-5-102

Contract 2.2 has the IEC 102 section, the baseline fixture and the client
image `iec102-gisce` (gisce/iec870ree), smoke-tested against a scripted
meter. Next:

1. **A controlled station.** None is open (REFERENCE_STACKS.md); until one
   is, `iec102-gisce` stays experimental and there is no cross-stack run.
2. More of gisce: single points by dates (C_SP_NB_2, events), blocks of
   totals (C_CB_UN_2), configuration (C_RM_NA_2), contracted powers
   (C_PC_NA_2), special days (C_DF_NA_2), as operations and fixture parts.
3. The standard read commands other than C_CI_NT_2 and C_CI_NU_2 (C_CI_NA_2
   .. C_CI_NS_2): no open client sends them.
4. One-octet link and common addresses: gisce has two octets only.

## IEC 60870-5-103

Contract 2.1 has the IEC 103 images (vitusdong server and client; goiecp5,
ariec103, open103 clients). Next:

1. **A second fixture-driven server.** The only one, vitusdong, deviates from
   the standard in every reply the cases check (tests/iec103/known-bugs.tsv),
   so the expectations of `tests/iec103/cases.tsv` are the contract's and no
   reference server meets them yet. Candidates: the ArIEC103 slave simulator
   behind a fixture (its model is fixed in code, so this needs a change
   upstream or a different simulator); dscsystems/rs-iec60870-5 has no
   secondary.
3. **Generic services and disturbance data** (ASDU 10/11/21, 23..31):
   operations `generic-read` and `disturbance-list`/`disturbance-get`, a
   `generic` and a `disturbances` part of the fixture. Only vitusdong has both
   roles for them, so the first run would be one pairing.
4. Broadcast clock synchronization: no client of the survey sends SEND/NO
   REPLY to address 255; the contract defines `--broadcast` for a consumer.
5. Upstream reports of the known bugs (the maintainer files them).
6. Captures: `tshark -d tcp.port==2401,iec60870_5_103` of each pairing.

## IEC 60870-5-104

- Carry over from iec104-interop: the Upstream workflow and `candidate-*`
  targets, the `run-*` targets, and an `own/` image (ADR-0012). (The
  release workflow is carried over in v0.1.0.)
- Publish the images under the new names; consumers move from
  `iec104-interop-<stack>` to `iec60870-5-interop-iec104-<stack>`.

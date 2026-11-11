# Roadmap

What exists is in [COMPATIBILITY.md](COMPATIBILITY.md); candidates are
assessed in [REFERENCE_STACKS.md](REFERENCE_STACKS.md). Nothing here is done.

## IEC 60870-5-101

1. **digitalpetri/iec60870** (Java 17, EPL-2.0, native 101-over-TCP) as a third
   adapter, pinned by full SHA. cB-Sigma's IEC 101 link was debugged against
   lib60870-C, so the two validated adapters are not fully independent
   evidence; digitalpetri is the strongest independent peer of the survey.
2. More cases: interrogation groups (an IEC 101 `extended` fixture), field
   sizes other than the defaults in the cross-stack run (today only the smoke
   test exercises 2/2/2/3 per adapter), `--link-address-size 0` in balanced
   mode, distinct balanced addresses between stacks that support them.
3. Captures: a `tshark -d tcp.port==2401,iec60870_101` decode of each pairing
   as a passive check of the frames.

## IEC 60870-5-102

- gisce/iec870ree (AGPL-3.0, Python, REE profile, master only) as the
  controlling-station oracle; the controlled-station role has no open peer.
- Its own fixture, cases and contract section (a minor bump).

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
2. **goiecp5 licence**: the maintainer decides between the LICENSE file
   (LGPL-3.0) and the README and file headers (GPL-3.0) of riclolsen/go-iecp5;
   then the image moves from proposed and joins the published set
   (`UNPUBLISHED` in the Makefile).
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

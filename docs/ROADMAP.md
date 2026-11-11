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

## IEC 60870-5-102 and -103

- 102: gisce/iec870ree (AGPL-3.0, Python, REE profile, master only) as the
  controlling-station oracle; the controlled-station role has no open peer.
- 103: masarray/ARIEC103 (Apache-2.0, .NET, serial only, pty bridge) and
  vitusdong/iec103-dotnet (MIT; not independent of ARIEC103).
- Both need their own fixtures, cases and contract sections (minor bumps).

## IEC 60870-5-104

- Carry over from iec104-interop: the Upstream workflow and `candidate-*`
  targets, the `run-*` targets, and an `own/` image (ADR-0012). (The
  release workflow is carried over in v0.1.0.)
- Publish the images under the new names; consumers move from
  `iec104-interop-<stack>` to `iec60870-5-interop-iec104-<stack>`.

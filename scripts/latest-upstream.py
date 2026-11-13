#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Compare the pins of versions.yaml with the latest upstream.

For every implementation of versions.yaml:

- a release pin (`version` is a version tag) against the newest version tag of
  its GitHub `repository`;
- a commit pin (digitalpetri, Open103, ARIEC103: no release, or a commit after
  the last one, `version` is not a plain version tag) against the head of the
  repository's default branch, compared with `revision`;
- a Maven pin (`artifact` group:artifact, OpenMUC j60870) against the
  <release> of its Maven Central metadata.

Writes a Markdown table to stdout (or --output) and, under GitHub Actions,
`behind=<n>` to $GITHUB_OUTPUT. With --rows, also one JSON row per pin in the
format of otfabric/.github/actions/upstream-row; the Upstream workflow checks
the base images with otfabric/.github/actions/upstream-images. A lookup that
fails is an `error` row, never a failure of the whole check. Never changes a
pin: bumping is a reviewed change that CI builds and smoke-tests.

    scripts/latest-upstream.py [--output table.md] [--rows DIR]
"""

import argparse
import json
import os
import re
import sys
import urllib.request
import xml.etree.ElementTree as ET

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RELEASE = re.compile(r"v?\d+(\.\d+)*")


def fetch(url, accept="application/json"):
    req = urllib.request.Request(url, headers={"User-Agent": "iec60870-5-interop-upstream-check",
                                               "Accept": accept})
    token = os.environ.get("GITHUB_TOKEN")
    if token and url.startswith("https://api.github.com/"):
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.read()


def get_json(url):
    return json.loads(fetch(url))


def parse_versions():
    """A small reader of versions.yaml (no PyYAML needed): the implementations."""
    impls, cur, section = {}, None, None
    for line in open(os.path.join(ROOT, "versions.yaml")):
        line = line.split(" #")[0].rstrip()
        if not line or line.lstrip().startswith("#"):
            continue
        if not line.startswith(" "):
            section = line.split(":")[0]
            continue
        m = re.match(r"^  (\w[\w-]*):\s*$", line)
        if m:
            cur = impls.setdefault(m.group(1), {}) if section == "implementations" else None
            continue
        m = re.match(r"^    (\w+):\s*(.*)$", line)
        if m and cur is not None:
            cur[m.group(1)] = m.group(2).strip().strip('"')
    return impls


def github(repository):
    m = re.fullmatch(r"https://github\.com/([^/]+/[^/]+?)/?", repository)
    return m.group(1) if m else None


def vkey(tag):
    return tuple(int(x) for x in re.findall(r"\d+", tag))


def newest_tag(repo):
    tags = [t["name"] for t in get_json("https://api.github.com/repos/%s/tags?per_page=100" % repo)]
    tags = [t for t in tags if RELEASE.fullmatch(t)]
    if not tags:
        raise RuntimeError("no version tag in %s" % repo)
    return max(tags, key=vkey)


def default_head(repo):
    branch = get_json("https://api.github.com/repos/%s" % repo)["default_branch"]
    return get_json("https://api.github.com/repos/%s/commits/%s" % (repo, branch))["sha"], branch


def maven_release(artifact):
    group, name = artifact.split(":")
    url = "https://repo1.maven.org/maven2/%s/%s/maven-metadata.xml" % (group.replace(".", "/"), name)
    meta = ET.fromstring(fetch(url, "application/xml"))
    release = meta.findtext("versioning/release") or meta.findtext("versioning/latest")
    if not release:
        raise RuntimeError("no release in %s" % url)
    return release


def check(impl):
    """(kind, source, pinned, latest, current) of one implementation."""
    version, revision = impl.get("version", ""), impl.get("revision", "")
    repo = github(impl.get("repository", ""))
    if impl.get("artifact") and ":" in impl["artifact"] and not repo:
        latest = maven_release(impl["artifact"])
        return "library", "Maven Central", version, latest, latest.lstrip("v") == version.lstrip("v")
    if not repo:
        raise RuntimeError("no GitHub repository or Maven artifact to look up")
    if RELEASE.fullmatch(version):
        latest = newest_tag(repo)
        return "library", "GitHub tag", version, latest, vkey(latest) <= vkey(version)
    head, branch = default_head(repo)
    return "commit", "%s head" % branch, revision[:12], head[:12], head == revision


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--output")
    ap.add_argument("--rows", help="write one JSON row per pin into this directory")
    args = ap.parse_args()
    rows, records, behind, errors = [], [], 0, 0
    for name, impl in sorted(parse_versions().items()):
        try:
            kind, source, pinned, latest, current = check(impl)
        except Exception as e:  # report, do not fail the whole check
            errors += 1
            pinned = impl.get("version", "")
            rows.append((name, pinned, "?", "error: %s" % e))
            records.append(dict(component=name, kind="library", source="", pinned=pinned, latest="",
                                status="error", detail=str(e)[-160:]))
            continue
        if not current:
            behind += 1
        rows.append((name, pinned, latest, "up to date" if current else "behind (%s)" % source))
        records.append(dict(component=name, kind=kind, source=source, pinned=pinned, latest=latest,
                            status="current" if current else "behind", detail=""))
    table = "| Component | Pinned | Latest | Status |\n|---|---|---|---|\n" + \
        "".join("| %s | %s | %s | %s |\n" % r for r in rows)
    text = "%d pin(s) behind the latest upstream, %d lookup(s) failed.\n\n%s" % (behind, errors, table)
    if args.rows:
        os.makedirs(args.rows, exist_ok=True)
        for r in records:
            r.update(test_outcome="", test_result="", test_cached=False, known="")
            slug = re.sub(r"[^A-Za-z0-9._-]+", "-", r["component"]).strip("-").lower()
            with open(os.path.join(args.rows, "row-%s.json" % slug), "w") as f:
                json.dump(r, f)
    if args.output:
        with open(args.output, "w") as f:
            f.write(text)
    sys.stdout.write(text)
    if os.environ.get("GITHUB_OUTPUT"):
        with open(os.environ["GITHUB_OUTPUT"], "a") as f:
            f.write("behind=%d\n" % behind)


if __name__ == "__main__":
    main()

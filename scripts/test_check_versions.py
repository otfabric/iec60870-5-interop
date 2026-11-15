#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Tests of scripts/check-versions.sh: on a copy of the build files in which
one disagrees with versions.yaml, the check exits 1 and says where."""

import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
# What scripts/check-versions.sh reads.
BUILD_FILES = {"Dockerfile", "go.mod", "go.sum", "pom.xml", "requirements.txt", "build-requirements.txt",
               "packages.lock.json"}


def build_files_only(directory, names):
    return [n for n in names if not os.path.isdir(os.path.join(directory, n)) and n not in BUILD_FILES]


@unittest.skipUnless(shutil.which("bash"), "needs bash")
class CheckVersions(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = os.path.join(self.tmp.name, "repo")
        os.makedirs(os.path.join(self.root, ".github", "workflows"))
        for name in ("versions.yaml", "Makefile", os.path.join(".github", "workflows", "ci.yml")):
            shutil.copy2(os.path.join(ROOT, name), os.path.join(self.root, name))
        shutil.copytree(os.path.join(ROOT, "scripts"), os.path.join(self.root, "scripts"),
                        ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
        shutil.copytree(os.path.join(ROOT, "adapters"), os.path.join(self.root, "adapters"), ignore=build_files_only)

    def edit(self, path, old, new, count=1):
        path = os.path.join(self.root, path)
        with open(path, newline="") as f:
            text = f.read()
        self.assertEqual(text.count(old), count, "%s: %r" % (path, old))
        with open(path, "w", newline="") as f:
            f.write(text.replace(old, new))

    def check(self):
        p = subprocess.run(["bash", "scripts/check-versions.sh"], cwd=self.root, capture_output=True, text=True)
        return p.returncode, [line for line in p.stdout.splitlines() if line.startswith("FAIL")]

    def assertFails(self, needle):
        code, failed = self.check()
        self.assertEqual(code, 1, failed)
        self.assertTrue(any(needle in line for line in failed), failed)

    def test_the_repository_as_committed_passes(self):
        self.assertEqual(self.check(), (0, []))

    # A value the build files do not have (an ARG renamed, or written into FROM).
    def test_a_revision_that_is_not_found_fails(self):
        for standard in ("iec101", "iec104"):
            self.edit("adapters/%s/lib60870/Dockerfile" % standard, "ARG LIB60870_SHA=7a38", "ARG LIB60870_COMMIT=7a38",
                      count=2)  # both build stages
        self.assertFails("lib60870 revision: no value found")
        self.assertFails("iec101 lib60870 LIB60870_SHA: not found in both lib60870 Dockerfiles")

    def test_a_base_image_digest_that_is_not_found_fails(self):
        self.edit("adapters/iec104/wendy512/Dockerfile", "ARG GOLANG_DIGEST=", "ARG GO_DIGEST=")
        self.assertFails("golang digest: no value found")

    def test_an_implementation_without_published_fails(self):
        self.edit("versions.yaml", "    roles: [server, client]\n    published: true\n\n  iec104-openmuc:",
                  "    roles: [server, client]\n\n  iec104-openmuc:")
        self.assertFails("iec104-lib60870 published: ''")

    def test_published_that_the_makefile_does_not_publish_fails(self):
        self.edit("Makefile", "UNPUBLISHED ?=\n", "UNPUBLISHED ?= iec104-part5\n")
        self.assertFails("iec104-part5 published: true, but the Makefile (PUBLISHED) says false")

    def test_a_module_sum_that_go_sum_does_not_record_fails(self):
        self.edit("versions.yaml", 'module_sum: "h1:y6wzML0vxqqzfvvaXvifp4Tcups9opG42oKny0xPliw="',
                  'module_sum: "h1:AAwzML0vxqqzfvvaXvifp4Tcups9opG42oKny0xPliw="')
        self.assertFails("iec104-part5 module sum")

    def test_a_module_sum_of_the_engine_that_go_sum_does_not_record_fails(self):
        self.edit("adapters/iec104/wendy512/go.sum", "go-iecp5 v1.2.6 h1:EEYV", "go-iecp5 v1.2.6 h1:AAYV")
        self.assertFails("iec104-wendy512 engine module sum")

    def test_an_implementation_without_a_source_hash_fails(self):
        self.edit("versions.yaml", '    source_sha256: "fe2d45013643b8b47d9601ac9858789e0575db7fde58458955ed117afdce562e"',
                  '    source_sha256: ""')
        self.assertFails("iec103-open103 source_sha256: '' is not a SHA-256")

    def test_a_tool_image_without_a_digest_fails(self):
        self.edit("Makefile", "koalaman/shellcheck:v0.11.0@sha256:61862eba1fcf09a484ebcc6feea46f1782532571a34ed51fedf90dd25f925a8d",
                  "koalaman/shellcheck:stable")
        self.assertFails("Makefile SHELLCHECK_IMAGE")

    def test_a_tool_image_that_versions_yaml_does_not_record_fails(self):
        self.edit("Makefile", "python:3.12.16-slim@sha256:5e2b", "python:3.12.16-slim@sha256:0000")
        self.assertFails("Makefile PYTHON_IMAGE")

    def test_a_govulncheck_of_ci_that_versions_yaml_does_not_record_fails(self):
        self.edit(".github/workflows/ci.yml", "GOVULNCHECK_VERSION: v1.9.0", "GOVULNCHECK_VERSION: v1.8.0")
        self.assertFails("ci.yml govulncheck")
        self.edit(".github/workflows/ci.yml", 'GO_VERSION: "1.27.2"', 'GO_VERSION: "1.26.0"')
        self.assertFails("ci.yml govulncheck Go")


if __name__ == "__main__":
    unittest.main()

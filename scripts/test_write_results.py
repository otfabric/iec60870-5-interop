#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Tests of scripts/write-results.py and scripts/results.sh that need no Docker."""

import importlib.util
import json
import os
import subprocess
import tempfile
import unittest

import interop_result

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("write_results", os.path.join(HERE, "write-results.py"))
wr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(wr)

RUN = {"kind": "run", "startedAt": "2026-10-30T00:00:00Z", "reproduce": "make interop"}


def result(outcome, **kw):
    r = {"kind": "result", "id": "suite.scenario", "client": "a", "server": "b", "outcome": outcome,
         "durationMs": 3, "reason": "", "waiver": ""}
    r.update(kw)
    return r


def waiver(reproduced):
    return {"kind": "waiver", "id": "w", "scope": "client@a", "evidence": "https://example.com/1", "reproduced": reproduced}


def convert(*records):
    doc = wr.convert([RUN] + list(records), {"a": {"version": "1", "revision": "abc"}})
    doc["summary"] = interop_result.summarize(doc["results"])
    return doc


class Convert(unittest.TestCase):
    def test_a_pass_passes_and_validates(self):
        doc = convert(result("pass"))
        self.assertEqual(interop_result.problems(doc), [])
        self.assertEqual(interop_result.schema_errors(doc), [])
        self.assertEqual(doc["results"][0]["suite"], "suite")
        self.assertEqual(doc["results"][0]["roles"], {"client": "a", "server": "b"})

    def test_every_outcome_but_pass_has_a_reason(self):
        for outcome in interop_result.OUTCOMES[1:]:
            self.assertTrue(convert(result(outcome, waiver="w")).get("results")[0]["reason"])

    def test_a_reproduced_waiver_is_not_a_pass(self):
        doc = convert(result("pass", id="x.y"), result("known-difference", reason="r", waiver="w"), waiver(True))
        self.assertEqual(doc["summary"]["known-difference"], 1)
        self.assertEqual(doc["summary"]["pass"], 1)
        self.assertEqual(interop_result.problems(doc), [])
        self.assertEqual(interop_result.schema_errors(doc), [])

    def test_a_stale_waiver_fails(self):
        self.assertTrue(interop_result.problems(convert(result("pass"), waiver(False))))

    def test_only_known_differences_fail(self):
        self.assertTrue(interop_result.problems(convert(result("known-difference", reason="r", waiver="w"), waiver(True))))

    def test_all_skipped_and_empty_runs_fail(self):
        self.assertTrue(interop_result.problems(convert(result("unsupported", reason="r"), result("not-tested", reason="r"))))
        doc = convert()
        self.assertTrue(interop_result.problems(doc))
        self.assertEqual(interop_result.schema_errors(doc), [])

    def test_fail_and_infra_error_fail(self):
        self.assertTrue(interop_result.problems(convert(result("pass"), result("fail", reason="r"))))
        self.assertTrue(interop_result.problems(convert(result("pass"), result("infra-error", reason="r"))))

    def test_a_run_without_peers_has_a_role(self):
        self.assertEqual(convert(result("pass", client="", server=""))["results"][0]["roles"], {"harness": "selftest"})

    def test_a_waiver_seen_by_two_self_tests_reproduced_if_either_saw_it(self):
        self.assertTrue(convert(result("pass"), waiver(False), waiver(True))["waivers"][0]["reproduced"])

    def test_the_implementation_comes_from_versions_yaml(self):
        self.assertEqual(wr.implementation("a", {"a": {"revision": "0123456789abcdef"}}, None)["version"], "0123456789ab")
        self.assertTrue(wr.versions_yaml())

    def test_an_image_outside_versions_yaml_says_what_it_is(self):
        seen = {"kind": "implementation", "name": "b", "version": "v1.2.3", "upstream": "stack", "revision": "abc",
                "license": "MIT", "independent": False}
        doc = convert(result("pass"), seen)
        got = {i["name"]: i for i in doc["implementations"]}
        self.assertEqual(got["b"], {"name": "b", "version": "v1.2.3", "independent": False, "upstream": "stack",
                                    "revision": "abc", "license": "MIT"})
        self.assertTrue(got["a"]["independent"])
        self.assertEqual(interop_result.schema_errors(doc), [])


class Records(unittest.TestCase):
    """scripts/results.sh, through a shell."""

    def run_shell(self, body):
        with tempfile.TemporaryDirectory() as d:
            script = 'set -u; source "%s/results.sh"; D=%s; %s' % (HERE, d, body)
            p = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            with open(os.path.join(d, "results.json")) as f:
                return p.returncode, json.load(f)

    def test_a_passing_run(self):
        code, doc = self.run_shell('results_open t $D "make t"; at a.b x y; result pass; results_close')
        self.assertEqual((code, doc["summary"]["pass"], doc["reproduce"]), (0, 1, "make t"))

    def test_a_run_that_stops_early_is_an_infra_error(self):
        code, doc = self.run_shell('results_open t $D "make t"; at a.b x y; result pass; results_abort')
        self.assertEqual(doc["summary"]["infra-error"], 1)
        self.assertEqual(doc["results"][-1]["id"], "harness.aborted")

    def test_a_failure_outside_a_scenario_is_an_infra_error(self):
        code, doc = self.run_shell('results_open t $D "make t"; result fail "no openssl"; results_close')
        self.assertNotEqual(code, 0)
        self.assertEqual(doc["results"][0]["id"], "harness.check")
        self.assertEqual(doc["results"][0]["outcome"], "infra-error")

    def test_a_run_with_no_scenario_fails(self):
        code, _ = self.run_shell('results_open t $D "make t"; results_close')
        self.assertNotEqual(code, 0)


class KnownBugs(unittest.TestCase):
    """judge_known_bugs of scripts/lib.sh, the end of scripts/interop.sh: which
    known bug that did not reproduce is stale."""

    ENTRY = "vitusdong@client\t^generic-\tsends INF 0\tvitusdong-inf\thttps://example.com/1\n"

    def judge(self, env, reproduced="", consumer=True):
        with tempfile.TemporaryDirectory() as d:
            w = os.path.join(d, "work")
            os.mkdir(w)
            for name, text in (("active", self.ENTRY), ("reproduced", reproduced),
                               ("consumer", "vitusdong-inf\n" if consumer else "")):
                with open(os.path.join(w, name), "w", newline="") as f:
                    f.write(text)
            script = ('set -u; source "%s/lib.sh"; D=%s; results_open t $D "make t"; at a.b x y; ok "a check"; '
                      'judge_known_bugs %s; finish' % (HERE, d, w))
            run_env = {k: v for k, v in os.environ.items() if k not in ("GATE", "CONSUMER_RUN")}
            run_env.update(env)
            p = subprocess.run(["bash", "-c", script], capture_output=True, text=True, env=run_env)
            with open(os.path.join(d, "results.json")) as f:
                doc = json.load(f)
            stale = [r["outcome"] for r in doc["results"] if r["id"] == "waiver.stale"]
            return p.returncode, stale, [w["reproduced"] for w in doc["waivers"]]

    def test_a_consumer_entry_that_does_not_reproduce_with_the_own_image_is_stale(self):
        code, stale, waivers = self.judge({"GATE": "otfabric", "CONSUMER_RUN": "1"})
        self.assertNotEqual(code, 0)
        self.assertEqual((stale, waivers), (["fail"], [False]))

    def test_a_consumer_entry_is_never_stale_in_a_run_of_the_references(self):
        self.assertEqual(self.judge({}), (0, [], []))
        self.assertEqual(self.judge({"GATE": "otfabric"}), (0, [], []))

    def test_a_consumer_entry_that_reproduces_is_a_waiver(self):
        self.assertEqual(self.judge({"GATE": "otfabric", "CONSUMER_RUN": "1"}, reproduced="sends INF 0\n"),
                         (0, [], [True]))

    def test_an_entry_of_another_adapter_than_the_gate_is_not_stale(self):
        self.assertEqual(self.judge({"GATE": "otfabric", "CONSUMER_RUN": "1"}, consumer=False), (0, [], []))
        code, stale, _ = self.judge({}, consumer=False)
        self.assertEqual((code != 0, stale), (True, ["fail"]))


if __name__ == "__main__":
    unittest.main()

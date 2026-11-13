#!/usr/bin/env python3
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

import copy
import json
import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import build_report  # noqa: E402


def fixture():
    with open(os.path.join(HERE, "sample-results.json"), encoding="utf-8") as f:
        return json.load(f)


def case(page, case_id):
    start = page.index('<code class="id">{}</code>'.format(case_id))
    end = page.find('<details class="case', start)
    return page[start : end if end != -1 else len(page)]


class RenderTests(unittest.TestCase):
    def test_fixture_summary(self):
        page = build_report.render(fixture())
        self.assertEqual(page.count('<details class="case'), 10)
        self.assertIn('<span class="big">8<span class="of">/10</span></span>', page)
        self.assertIn("<code>docstring-helpers</code> and <code>unit-test-parser</code>", page)
        self.assertEqual(page.count('<details class="case is-split"'), 2)
        self.assertNotIn("http://", page)
        self.assertNotIn("https://", page)

    def test_scores_read_in_opposite_directions(self):
        page = build_report.render(fixture())
        split = case(page, "unit-test-parser")
        self.assertIn('<span class="calc"><b>0.10</b> \u2264 0.15</span>', split)
        self.assertIn('<span class="calc"><b>0.60</b> &lt; 0.7</span>', split)
        self.assertIn("Threshold raised from 0.6", split)

    def test_rounding_never_hides_the_side_of_the_threshold(self):
        self.assertEqual(build_report.fmt_score(0.146, 0.15), "0.146")
        self.assertEqual(build_report.fmt_score(0.15, 0.15), "0.15")
        self.assertEqual(build_report.fmt_score(0.42, 0.15), "0.42")

    def test_answer_states(self):
        data = fixture()
        runs = data["cases"][0]["runs"]
        runs["jev"]["answer"], runs["jev"]["finish_reason"] = None, "length"
        runs["classifier"]["answer"], runs["classifier"]["finish_reason"] = "x" * 1000, "length"
        block = case(build_report.render(data), data["cases"][0]["id"])
        self.assertIn("used its whole token budget on reasoning", block)
        self.assertIn("cut off at the token limit", block)
        self.assertIn("Show the full answer", block)

    def test_escapes_answers_and_evidence(self):
        data = fixture()
        hostile = '<script>alert("x & y")</script>'
        first = data["cases"][0]
        first["title"] = first["prompt"] = hostile
        first["runs"]["jev"]["answer"] = hostile
        first["runs"]["classifier"]["evidence"]["evidence"][hostile] = hostile
        data["judges"][0]["model"] = hostile
        page = build_report.render(data)
        self.assertNotIn('alert("x & y")', page)
        self.assertIn("&lt;script&gt;alert(&quot;x &amp; y&quot;)&lt;/script&gt;", page)
        self.assertEqual(page.count("<script>"), 2)

    def test_reads_v1_files(self):
        v1 = {
            "judge": {"name": "jev", "format": "system_one", "model": "jev-latest"},
            "virtual_model": "coding-auto",
            "targets": {"efficient": "small", "capable": "large"},
            "algorithm": {"type": "llm_classifier", "cutoff": 0.15},
            "cases": [
                {
                    "id": "one",
                    "expected_tier": "capable",
                    "selected_model": "large",
                    "reason": "llm_task_classifier:decision_classifier",
                    "evidence": {"evidence": {"score": 0.4, "threshold": 0.15}},
                    "latency_ms": 90,
                }
            ],
        }
        page = build_report.render(v1)
        self.assertEqual(page.count('<details class="case'), 1)
        self.assertIn('<span class="calc"><b>0.40</b> &gt; 0.15</span>', page)
        self.assertNotIn("big", page.split("<h2>")[2].split("</table>")[0].split('class="tbl"')[0])

    def test_empty_cases(self):
        data = fixture()
        data["cases"] = []
        page = build_report.render(data)
        self.assertIn("No cases in this run", page)
        self.assertNotIn('<details class="case', page)

    def test_missing_fields(self):
        page = build_report.render({"cases": [{"id": "bare"}]})
        self.assertIn('<code class="id">bare</code>', page)
        self.assertIn("unlabeled", page)

    def test_rejects_non_object_cases(self):
        with self.assertRaises(ValueError):
            build_report.render({"cases": [1]})

    def test_percentile_nearest_rank(self):
        self.assertIsNone(build_report.percentile([], 50))
        self.assertEqual(build_report.percentile([5], 95), 5)
        self.assertEqual(build_report.percentile(list(range(1, 11)), 50), 5)
        self.assertEqual(build_report.percentile(list(range(1, 11)), 95), 10)


if __name__ == "__main__":
    unittest.main()

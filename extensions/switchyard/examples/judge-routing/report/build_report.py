#!/usr/bin/env python3
# Copyright Built On Envoy
# SPDX-License-Identifier: Apache-2.0
# The full text of the Apache license is available in the LICENSE file at
# the root of the repo.

"""Builds a self-contained HTML report from the judge-routing demo's results JSON.

Usage: build_report.py RESULTS_JSON OUT_HTML

The input is the demo's results JSON (v2): the targets, the judges, and one entry per case with
the human label (expected_tier) and, for each judge, the model it routed to, the decision reason,
the judge evidence, the latency, and the target model's answer. A v1 file, with a single judge
and the run fields directly on each case, is read as a v2 file with one judge.

The output is one HTML file with inline CSS and a few lines of script for the theme toggle and
the case and answer disclosures. It loads nothing from the network. Every string taken from the
JSON is HTML-escaped.
"""

import html
import json
import math
import sys
from datetime import datetime, timezone

DASH = "\u2014"
ANSWER_PREVIEW_LINES = 12

# How each judge's score reads. Jev scores the advantage of the capable model, so a score above
# the cutoff escalates. An LLM classifier scores the odds that the efficient model solves the task,
# so a score below the threshold escalates.
SCORE_SEMANTICS = {
    "decision": ("Capable-model advantage", "above"),
    "llm": ("Odds the efficient model solves it", "below"),
}


def esc(value):
    return html.escape("" if value is None else str(value), quote=True)


def num(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    if isinstance(value, float) and not math.isfinite(value):
        return None
    return float(value)


def percentile(values, pct):
    if not values:
        return None
    ordered = sorted(values)
    rank = max(1, math.ceil(pct / 100 * len(ordered)))
    return ordered[rank - 1]


def fmt_ms(value):
    if value is None:
        return DASH
    if value >= 1000:
        return "{:.1f} s".format(value / 1000)
    return "{:,.0f} ms".format(value)


def fmt_time(raw):
    if not isinstance(raw, str):
        return DASH
    try:
        parsed = datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        return raw
    if parsed.tzinfo is None:
        return parsed.strftime("%d %B %Y, %H:%M")
    return parsed.astimezone(timezone.utc).strftime("%d %B %Y, %H:%M UTC").lstrip("0")


def fmt_score(score, threshold):
    # Two decimals, unless rounding would make a score next to the threshold look equal to it.
    if threshold is not None and score != threshold and round(score, 2) == round(threshold, 2):
        return "{:.3f}".format(score)
    return "{:.2f}".format(score)


def as_dict(value):
    return value if isinstance(value, dict) else {}


def as_str(value):
    return value if isinstance(value, str) and value else None


def upgrade_v1(data):
    if isinstance(data.get("judges"), list):
        return data
    judge = as_dict(data.get("judge"))
    judge_id = as_str(judge.get("name")) or "judge"
    v2 = dict(data)
    v2["judges"] = [
        {
            "id": judge_id,
            "virtual_model": data.get("virtual_model"),
            "kind": "llm" if judge.get("format") in ("openai_chat", "openai_responses", "anthropic_messages") else "decision",
            "format": judge.get("format"),
            "model": judge.get("model"),
            "algorithm": as_dict(data.get("algorithm")),
        }
    ]
    cases = []
    for raw in data.get("cases", []):
        if not isinstance(raw, dict):
            cases.append(raw)
            continue
        run = {k: raw.get(k) for k in ("selected_model", "served_model", "reason", "evidence", "status", "latency_ms", "answer", "finish_reason")}
        case = {k: raw.get(k) for k in ("id", "title", "prompt", "expected_tier")}
        case["runs"] = {judge_id: run}
        cases.append(case)
    v2["cases"] = cases
    return v2


def read_judges(raw_judges):
    judges = []
    for raw in raw_judges:
        raw = as_dict(raw)
        if not as_str(raw.get("id")):
            continue
        kind = raw["kind"] if raw.get("kind") in SCORE_SEMANTICS else "decision"
        algorithm = as_dict(raw.get("algorithm"))
        judges.append(
            {
                "id": raw["id"],
                "virtual_model": as_str(raw.get("virtual_model")),
                "kind": kind,
                "format": as_str(raw.get("format")),
                "model": as_str(raw.get("model")),
                "algorithm": algorithm,
                "score_label": SCORE_SEMANTICS[kind][0],
                "escalates": SCORE_SEMANTICS[kind][1],
            }
        )
    return judges


def read_run(raw, judge, targets):
    raw = as_dict(raw)
    selected = as_str(raw.get("selected_model"))
    routed = next((tier for tier, model in targets.items() if model == selected), None)
    reason = as_str(raw.get("reason"))
    evidence = raw.get("evidence")
    inner = as_dict(as_dict(evidence).get("evidence")) or as_dict(evidence)
    score = num(inner.get("score"))
    threshold = num(inner.get("threshold"))
    if threshold is None:
        threshold = num(judge["algorithm"].get("cutoff"))
    if threshold is None:
        threshold = num(judge["algorithm"].get("base_threshold"))
    answer = raw.get("answer") if isinstance(raw.get("answer"), str) else None
    return {
        "selected": selected,
        "routed": routed,
        "served": as_str(raw.get("served_model")),
        "reason": reason,
        "fallback": reason is not None and ("fallback" in reason or "fail_open" in reason),
        "evidence": evidence,
        "inner": inner,
        "score": score if score is not None and 0.0 <= score <= 1.0 else None,
        "threshold": threshold if threshold is not None and 0.0 <= threshold <= 1.0 else None,
        "status": raw.get("status"),
        "latency": num(raw.get("latency_ms")),
        "answer": answer,
        "finish": as_str(raw.get("finish_reason")),
        "present": bool(raw),
    }


def read_case(raw, judges, targets):
    expected = raw.get("expected_tier") if isinstance(raw.get("expected_tier"), str) else None
    runs = as_dict(raw.get("runs"))
    results = {}
    for judge in judges:
        run = read_run(runs.get(judge["id"]), judge, targets)
        run["agree"] = None if expected is None or run["routed"] is None else run["routed"] == expected
        results[judge["id"]] = run
    routed = {run["routed"] for run in results.values() if run["routed"] is not None}
    return {
        "id": raw.get("id"),
        "title": raw.get("title") or raw.get("id") or DASH,
        "prompt": raw.get("prompt"),
        "expected": expected,
        "runs": results,
        "split": len(results) > 1 and len(routed) > 1,
    }


def summarize(cases, judges, targets):
    per_judge = {}
    for judge in judges:
        runs = [c["runs"][judge["id"]] for c in cases]
        labeled = [r for r in runs if r["agree"] is not None]
        latencies = [r["latency"] for r in runs if r["latency"] is not None]
        per_judge[judge["id"]] = {
            "agreed": sum(1 for r in labeled if r["agree"]),
            "labeled": len(labeled),
            "split": [(tier, sum(1 for r in runs if r["routed"] == tier)) for tier in targets],
            "p50": percentile(latencies, 50),
            "p95": percentile(latencies, 95),
            "fallbacks": sum(1 for r in runs if r["fallback"]),
        }
    comparable = [c for c in cases if all(r["routed"] for r in c["runs"].values())]
    return {
        "judges": per_judge,
        "cross_same": sum(1 for c in comparable if not c["split"]),
        "cross_total": len(comparable),
        "splits": [c for c in cases if c["split"]],
    }


ICONS = {
    "summary": '<path d="M4 5h16M4 10h16M4 15h10M4 20h7"/>',
    "judges": '<path d="M12 3v18M5 7h14M7 7l-3 7a3 3 0 0 0 6 0zM17 7l-3 7a3 3 0 0 0 6 0z"/>',
    "cases": '<path d="M4 6h16M4 12h16M4 18h16"/><circle cx="8" cy="6" r=".5"/>',
    "theme": '<path d="M21 13A9 9 0 1 1 11 3a7 7 0 0 0 10 10z"/>',
    "chev": '<path d="M4 6l4 4 4-4"/>',
}


def icon(name, cls="ico-h"):
    view = "0 0 16 16" if name == "chev" else "0 0 24 24"
    return '<svg class="{}" viewBox="{}" aria-hidden="true">{}</svg>'.format(cls, view, ICONS[name])


def tier_dot(tier):
    if tier in ("efficient", "capable"):
        return '<span class="tier {0}">{0}</span>'.format(tier)
    return '<span class="tier none">{}</span>'.format(esc(tier) if tier else "no tier")


def score_bar(run, judge):
    if run["score"] is None:
        return '<span class="score"><span class="none">no score</span></span>'
    style = "--v:{:.1f}%".format(run["score"] * 100)
    label = "{} {}".format(judge["score_label"], fmt_score(run["score"], run["threshold"]))
    tick = ""
    if run["threshold"] is not None:
        style += ";--t:{:.1f}%".format(run["threshold"] * 100)
        label += ", capable when {} {:g}".format(judge["escalates"], run["threshold"])
        tick = '<span class="tick"></span>'
    return (
        '<span class="score" title="{label}" aria-label="{label}">'
        '<span class="bar {tier}" style="{style}">{tick}</span>'
        '<span class="n">{n}</span></span>'
    ).format(label=esc(label), tier=run["routed"] or "none", style=style, tick=tick, n=fmt_score(run["score"], run["threshold"]))


def decision_sentence(run, judge):
    score, threshold = run["score"], run["threshold"]
    if run["evidence"] is None or score is None:
        why = esc(run["reason"]) if run["reason"] else "no reason recorded"
        return '<p class="decision quiet">No judge score. Reason <code>{}</code>.</p>'.format(why)
    if threshold is None:
        return '<p class="decision"><span class="dl">{}</span> <b>{}</b></p>'.format(esc(judge["score_label"]), fmt_score(score, None))
    if judge["escalates"] == "above":
        escalate = score > threshold
        op = ">" if escalate else "\u2264"
    else:
        escalate = score < threshold
        op = "<" if escalate else "\u2265"
    tier = "capable" if escalate else "efficient"
    base = num(judge["algorithm"].get("base_threshold"))
    raised = ""
    if base is not None and abs(base - threshold) > 1e-9:
        raised = '<p class="raised">Threshold raised from {:g}: the judge was unsure the efficient model could do it.</p>'.format(base)
    return (
        '<p class="decision"><span class="dl">{label}</span> '
        '<span class="calc-row"><span class="calc"><b>{s}</b> {op} {t:g}</span> <span class="arrow" aria-hidden="true">\u2192</span> {tier}</span></p>{raised}'
    ).format(label=esc(judge["score_label"]), s=fmt_score(score, threshold), op=esc(op), t=threshold, tier=tier_dot(tier), raised=raised)


def evidence_facts(run):
    rows = []
    for key, value in run["inner"].items():
        if key in ("score", "threshold") or isinstance(value, (dict, list)):
            continue
        rows.append("<dt>{}</dt><dd><code>{}</code></dd>".format(esc(key.replace("_", " ")), esc(value)))
    return '<dl class="fact">{}</dl>'.format("".join(rows)) if rows else ""


def render_answer(run):
    if run["answer"] is None:
        if run["finish"] == "length":
            return '<p class="answer-none">No answer. The model used its whole token budget on reasoning.</p>'
        if run["present"]:
            return '<p class="answer-none">No answer text was returned.</p>'
        return '<p class="answer-none">This judge has no run for this case.</p>'
    long = run["answer"].count("\n") >= ANSWER_PREVIEW_LINES or len(run["answer"]) > 900
    note = ' <span class="trunc">cut off at the token limit</span>' if run["finish"] == "length" else ""
    body = '<pre class="answer{}">{}</pre>'.format(" clip" if long else "", esc(run["answer"]))
    more = '<button type="button" class="more" data-more>Show the full answer</button>' if long else ""
    tail = '<p class="note">{}</p>'.format(note.strip()) if note else ""
    return '<div class="answer-wrap">{}{}</div>{}'.format(body, more, tail)


def render_judge_block(run, judge):
    status = run["status"]
    code = num(status)
    facts = []
    if run["served"] and run["served"] != run["selected"]:
        facts.append('<p class="alert">Served by <code>{}</code>, not the routed <code>{}</code>.</p>'.format(esc(run["served"]), esc(run["selected"])))
    if code is not None and not 200 <= code < 300:
        facts.append('<p class="alert">The request failed with HTTP {}.</p>'.format(esc(status)))
    detail_rows = [
        ("model", run["selected"]),
        ("served by", run["served"]),
        ("http", status),
        ("reason", run["reason"]),
    ]
    detail = evidence_facts(run)
    extra = "".join(
        "<dt>{}</dt><dd><code>{}</code></dd>".format(k, esc(v)) for k, v in detail_rows if v is not None
    )
    detail = detail.replace("</dl>", extra + "</dl>") if detail else '<dl class="fact">{}</dl>'.format(extra)
    return (
        '<section class="jb"><div class="jb-top"><header class="jb-head"><h3 class="jh">{name}</h3><span class="kind">{kind}</span></header>'
        "{decision}{alerts}</div>"
        '<div class="jb-answer">{answer}</div>'
        '<details class="ev"><summary>Details</summary>{detail}</details></section>'
    ).format(
        name=esc(judge["id"]),
        kind=esc(judge["model"] or judge["kind"]),
        decision=decision_sentence(run, judge),
        alerts="".join(facts),
        answer=render_answer(run),
        detail=detail,
    )


def render_case(index, case, judges):
    lines = []
    for judge in judges:
        run = case["runs"][judge["id"]]
        extra = ""
        if run["fallback"]:
            extra += ' <span class="mark warn">fallback</span>'
        miss = ' <span class="mark bad">not expected</span>' if run["agree"] is False else ""
        lines.append(
            '<span class="jr"><span class="jn">{name}</span><span class="jt">{tier}{miss}{extra}</span>{bar}'
            '<span class="lat">{lat}</span></span>'.format(
                name=esc(judge["id"]), tier=tier_dot(run["routed"]), miss=miss,
                extra=extra, bar=score_bar(run, judge), lat=fmt_ms(run["latency"]),
            )
        )
    split = '<span class="mark split">judges disagree</span>' if case["split"] else ""
    prompt = (
        '<p class="prompt">{}</p>'.format(esc(case["prompt"])) if case["prompt"] else '<p class="prompt quiet">No prompt recorded.</p>'
    )
    return (
        '<details class="case{split_cls}" id="case-{i}"><summary>'
        '<span class="cn">{i:02d}</span>'
        '<span class="ct"><span class="t">{title}</span><code class="id">{cid}</code>{split}</span>'
        '<span class="cx"><span class="lbl">Expected</span>{expected}</span>'
        '<span class="cj">{lines}</span>'
        '{chev}</summary>'
        '<div class="panel"><p class="prompt-label">Prompt</p>{prompt}<div class="judges-grid">{blocks}</div></div>'
        "</details>"
    ).format(
        split_cls=" is-split" if case["split"] else "",
        i=index,
        title=esc(case["title"]),
        cid=esc(case["id"] or DASH),
        expected=tier_dot(case["expected"]) if case["expected"] else '<span class="tier none">unlabeled</span>',
        split=split,
        lines="".join(lines),
        chev=icon("chev", "chev"),
        prompt=prompt,
        blocks="".join(render_judge_block(case["runs"][j["id"]], j) for j in judges),
    )


def render_facts(data, judges, targets):
    models = "".join(
        '<span class="tgt">{} <code>{}</code></span>'.format(tier_dot(tier), esc(model)) for tier, model in targets.items()
    )
    lines = ['<p class="setup-line"><span class="lbl">Targets</span><span class="sv">{}</span></p>'.format(models or DASH)]
    for judge in judges:
        algo = judge["algorithm"]
        threshold = num(algo.get("cutoff"))
        if threshold is None:
            threshold = num(algo.get("base_threshold"))
        lines.append(
            '<p class="setup-line"><span class="lbl">{name}</span><span class="sv"><code>{model}</code> '
            '<span class="quiet">({fmt}) on <code>{vm}</code>. Routes to the capable model when its score is {dir} {thr}.</span></span></p>'.format(
                name=esc(judge["id"]),
                model=esc(judge["model"] or DASH),
                fmt=esc(judge["format"] or judge["kind"]),
                vm=esc(judge["virtual_model"] or DASH),
                dir=judge["escalates"],
                thr="{:g}".format(threshold) if threshold is not None else "the threshold",
            )
        )
    if as_str(data.get("environment")):
        lines.append('<p class="setup-line"><span class="lbl">Ran on</span><span class="sv">{}</span></p>'.format(esc(data["environment"])))
    return '<div class="setup">{}</div>'.format("".join(lines))


def render_summary(stats, judges, cases):
    if not cases:
        return '<p class="quiet">No cases, so nothing to summarize.</p>'
    lead = ""
    if len(judges) > 1:
        lead = (
            '<div class="lead-stat"><span class="big">{same}<span class="of">/{total}</span></span>'
            '<p class="cap">requests where both judges picked the same model.{split}</p></div>'
        ).format(
            same=stats["cross_same"],
            total=stats["cross_total"],
            split=(
                " They disagreed on "
                + " and ".join("<code>{}</code>".format(esc(c["id"] or DASH)) for c in stats["splits"])
                + "."
                if stats["splits"]
                else ""
            ),
        )
    blocks = []
    for judge in judges:
        p = stats["judges"][judge["id"]]
        matched = "{} of {}".format(p["agreed"], p["labeled"]) if p["labeled"] else DASH
        split = ", ".join('<span class="nw">{} {}</span>'.format(n, esc(t)) for t, n in p["split"])
        fallbacks = (
            '<p class="js-line warn-line">{} fallback{}</p>'.format(p["fallbacks"], "" if p["fallbacks"] == 1 else "s")
            if p["fallbacks"]
            else ""
        )
        blocks.append(
            '<div class="js"><p class="js-name">{name}</p>'
            '<p class="js-fig">{matched} <span class="quiet">as expected</span></p>'
            '<p class="js-line">{split}</p>'
            '<p class="js-line">Median {p50} \u00b7 p95 {p95}</p>{fb}</div>'.format(
                name=esc(judge["id"]), matched=matched, split=split, p50=fmt_ms(p["p50"]), p95=fmt_ms(p["p95"]), fb=fallbacks
            )
        )
    note = '<p class="note">Latency is end to end: the judge call plus the full model answer.</p>'
    if lead:
        return '<section class="inc-lead">{}<div class="judge-stats">{}</div></section>{}'.format(lead, "".join(blocks), note)
    return '<div class="judge-stats">{}</div>'.format("".join(blocks))


def render(data):
    if not isinstance(data, dict):
        raise ValueError("results JSON must be an object")
    if not isinstance(data.get("cases", []), list) or not all(isinstance(c, dict) for c in data.get("cases", [])):
        raise ValueError('"cases" must be a list of objects')
    data = upgrade_v1(data)
    targets = {k: v for k, v in as_dict(data.get("targets")).items() if isinstance(v, str)}
    judges = read_judges(data.get("judges") or [])
    cases = [read_case(c, judges, targets) for c in data.get("cases", [])]
    stats = summarize(cases, judges, targets)

    names = " and ".join("<code>{}</code>".format(esc(j["id"])) for j in judges) or "no judges"
    plain_names = " and ".join(esc(j["id"]) for j in judges) or "no judges"
    heading = "Routing decisions by {}".format(plain_names)
    lede = "{} each picked a model for the same {} coding-agent request{}, choosing between {}. The answers are the real responses from the model each judge picked.".format(
        names,
        len(cases),
        "" if len(cases) == 1 else "s",
        " and ".join("<code>{}</code>".format(esc(m)) for m in targets.values()) or "the targets",
    )
    if cases:
        cols = (
            '<div class="cols" aria-hidden="true"><span>#</span><span>Case</span><span>Expected</span>'
            '<span class="cols-j"><span>Judge</span><span>Picked</span><span>Score vs threshold</span>'
            '<span class="r">Latency</span></span><span></span></div>'
        )
        body = '<div class="cases">{}{}</div>'.format(cols, "".join(render_case(i, c, judges) for i, c in enumerate(cases, 1)))
        tools = (
            '<div class="tools" hidden><button type="button" class="btn" data-open>Expand all</button>'
            '<button type="button" class="btn" data-close>Collapse all</button></div>'
        )
    else:
        body = (
            '<div class="empty"><p class="label">No cases in this run</p>'
            '<p class="quiet">The results file has an empty <code>cases</code> list. Run the demo again and point '
            "<code>build_report.py</code> at the results JSON it writes.</p></div>"
        )
        tools = ""
    return PAGE.format(
        title=esc("Judge routing report"),
        css=CSS,
        head_script=HEAD_SCRIPT,
        date=esc(fmt_time(data.get("generated_at"))),
        theme_icon=icon("theme", "ti"),
        lede=lede,
        heading=heading,
        facts=render_facts(data, judges, targets),
        ico_summary=icon("summary"),
        ico_judges=icon("judges"),
        ico_cases=icon("cases"),
        summary=render_summary(stats, judges, cases),
        count=len(cases),
        tools=tools,
        body=body,
        script=SCRIPT,
    )


PAGE = """<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
{head_script}
<style>{css}</style>
</head>
<body>
<main>
<div class="top"><span><b>Switchyard</b> \u00b7 judge routing report \u00b7 {date}</span>
<button type="button" class="theme" data-theme-toggle hidden>{theme_icon}<span data-theme-label>Dark</span></button></div>
<header class="hero"><h1>{heading}</h1>
<p class="lead">{lede}</p></header>
<h2>{ico_summary}Summary</h2>
{summary}
<h2>{ico_judges}Setup</h2>
{facts}
<div class="h2row"><h2>{ico_cases}Cases <span class="count">{count}</span></h2>{tools}</div>
{body}
</main>
{script}
</body>
</html>
"""

HEAD_SCRIPT = """<script>
(function () {
  var saved = null;
  try { saved = localStorage.getItem("judge-report-theme"); } catch (e) {}
  var dark = saved ? saved === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
  document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
})();
</script>"""

SCRIPT = """<script>
(function () {
  var root = document.documentElement;
  var toggle = document.querySelector("[data-theme-toggle]");
  var label = document.querySelector("[data-theme-label]");
  function sync() { label.textContent = root.getAttribute("data-theme") === "dark" ? "Light" : "Dark"; }
  toggle.hidden = false;
  sync();
  toggle.addEventListener("click", function () {
    var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    root.setAttribute("data-theme", next);
    try { localStorage.setItem("judge-report-theme", next); } catch (e) {}
    sync();
  });
  var cases = document.querySelectorAll("details.case");
  var tools = document.querySelector(".tools");
  function setAll(open) { for (var i = 0; i < cases.length; i++) cases[i].open = open; }
  if (tools) {
    tools.hidden = false;
    tools.querySelector("[data-open]").addEventListener("click", function () { setAll(true); });
    tools.querySelector("[data-close]").addEventListener("click", function () { setAll(false); });
  }
  document.addEventListener("click", function (event) {
    var button = event.target.closest && event.target.closest("[data-more]");
    if (!button) return;
    var pre = button.closest(".answer-wrap").querySelector("pre");
    var clipped = pre.classList.toggle("clip");
    button.textContent = clipped ? "Show the full answer" : "Show less";
  });
  window.addEventListener("beforeprint", function () { setAll(true); });
})();
</script>"""

CSS = """
:root {
  color-scheme: light;
  --paper: #fcfcfb; --paper-2: #f3f3f0;
  --ink: #16191c; --ink-2: #4a5259; --ink-3: #646d75;
  --rule: #d9dcdf; --rail: #b9bfc4; --track: #ecece8;
  --sy: #4e7d00; --sy-soft: #dfeccb; --sy-text: #3f6600;
  --ar: #d94800; --ar-soft: #fbe3d6; --ar-text: #b33b00;
  --danger: #b3261e;
  --focus: #16191c;
  --sans: 'Overpass', 'Helvetica Neue', Arial, sans-serif;
  --mono: 'Overpass Mono', Menlo, Consolas, monospace;
  --type-display: clamp(2.125rem, 5.2vw, 3.625rem);
  --type-lead: clamp(1.125rem, 2vw, 1.3125rem);
  --type-heading: 1.5rem;
  --type-body: 1rem; --type-meta: .875rem; --type-label: .75rem;
  --leading-body: 1.55; --leading-heading: 1.2;
}
:root[data-theme="dark"] {
  color-scheme: dark;
  --paper: #111417; --paper-2: #181c20;
  --ink: #eef0f2; --ink-2: #c3cad0; --ink-3: #9aa4ad;
  --rule: #343c42; --rail: #5a646d; --track: #262c31;
  --sy: #74b421; --sy-soft: #23330e; --sy-text: #74b421;
  --ar: #ff8a4d; --ar-soft: #3d2114; --ar-text: #ff8a4d;
  --danger: #ff6b63;
  --focus: #eef0f2;
}
* { box-sizing: border-box; }
html { scrollbar-color: var(--rail) var(--paper); }
.answer-wrap { scrollbar-color: var(--rail) var(--paper-2); }
body {
  margin: 0; overflow-x: clip; background: var(--paper); color: var(--ink);
  font-family: var(--sans); font-size: var(--type-body); line-height: var(--leading-body);
  -webkit-font-smoothing: antialiased;
}
::selection { background: var(--ar-soft); color: var(--ink); }
:focus-visible { outline: 2px solid var(--focus); outline-offset: 3px; border-radius: 3px; }
code, pre { font-family: var(--mono); font-size: .9em; }
code { overflow-wrap: break-word; }
b { font-weight: 600; color: var(--ink); }
main { max-width: 1080px; margin: 0 auto; padding: 0 24px 72px; }
.quiet, .none { color: var(--ink-3); }
.nw { white-space: nowrap; }

.top { display: flex; justify-content: space-between; align-items: center; gap: 16px; padding: 22px 0 0; font-size: var(--type-meta); color: var(--ink-3); }
.top b { color: var(--ink); }
.theme { font: inherit; color: var(--ink); background: transparent; border: 1px solid var(--rule); border-radius: 999px; padding: 6px 12px; cursor: pointer; display: inline-flex; gap: 6px; align-items: center; flex: none; }
.theme:hover { border-color: var(--ink-3); }
.ti { width: 13px; height: 13px; stroke: currentColor; fill: none; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
.hero { padding: 44px 0 4px; }
h1 { font-size: var(--type-display); line-height: 1.05; letter-spacing: -.028em; font-weight: 700; margin: 0 0 20px; text-wrap: pretty; }
.lead { font-size: var(--type-lead); color: var(--ink-2); margin: 0; max-width: 46rem; text-wrap: pretty; }
.lead code { color: var(--ink); }
h2 { font-size: var(--type-heading); line-height: var(--leading-heading); letter-spacing: -.02em; font-weight: 700; margin: 44px 0 16px; padding-top: 28px; border-top: 1px solid var(--rule); display: flex; align-items: center; }
.ico-h { width: .82em; height: .82em; margin-right: .42em; flex: none; fill: none; stroke: var(--ar); stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
.count { font-weight: 400; color: var(--ink-3); margin-left: .4em; font-variant-numeric: tabular-nums; }
.h2row { display: flex; align-items: flex-end; gap: 16px; margin: 44px 0 12px; padding-top: 28px; border-top: 1px solid var(--rule); }
.h2row h2 { margin: 0; padding: 0; border: 0; }
.tools { margin-left: auto; display: flex; gap: 8px; }
.btn { font: inherit; font-size: var(--type-meta); color: var(--ink); background: transparent; border: 1px solid var(--rule); border-radius: 999px; padding: 5px 12px; cursor: pointer; }
.btn:hover { border-color: var(--ink-3); }
.label { font-weight: 700; color: var(--ink); margin: 0 0 8px; }
.note { color: var(--ink-3); font-size: var(--type-meta); margin: 6px 0 0; }

.setup { display: grid; gap: 10px; padding: 16px 20px; background: var(--paper-2); border-radius: 12px; }
.setup-line { margin: 0; color: var(--ink); display: grid; grid-template-columns: 7rem minmax(0, 1fr); gap: 16px; align-items: baseline; }
.sv { display: flex; flex-wrap: wrap; align-items: baseline; gap: 6px 28px; min-width: 0; }
.tgt { display: inline-flex; align-items: baseline; gap: 8px; white-space: nowrap; }
.sv > .quiet { flex-basis: 100%; }
.sv > code + .quiet { flex-basis: auto; }
.lbl { font-size: var(--type-label); font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); }

.tier { display: inline-flex; align-items: center; gap: 7px; font-size: var(--type-meta); color: var(--ink-2); white-space: nowrap; }
.tier::before { content: ""; width: 8px; height: 8px; flex: none; }
.tier.efficient::before { background: var(--sy); border-radius: 50%; }
.tier.capable::before { background: var(--ar); clip-path: polygon(50% 0, 100% 50%, 50% 100%, 0 50%); }
.tier.none::before { border: 1.5px solid var(--ink-3); border-radius: 50%; width: 7px; height: 7px; }
.mark { font-size: var(--type-label); font-weight: 700; text-transform: uppercase; letter-spacing: .06em; white-space: nowrap; }
.mark.ok { color: var(--sy-text); }
.mark.bad { color: var(--danger); }
.mark.warn { color: var(--ar-text); }
.mark.unk { color: var(--ink-3); }
.mark.split { color: var(--ar-text); }

.inc-lead { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.35fr); gap: 20px 32px; align-items: center; padding: 20px 22px; border: 2px solid var(--rule); border-radius: 12px; background: var(--paper-2); }
.lead-stat { display: flex; flex-direction: column; gap: 10px; }
.big { font-size: clamp(2.75rem, 6vw, 3.75rem); font-weight: 700; letter-spacing: -.03em; line-height: 1; font-variant-numeric: tabular-nums; }
.big .of { color: var(--ink-3); font-weight: 400; margin-left: .06em; }
.cap { margin: 0; color: var(--ink-2); text-wrap: pretty; }
.judge-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr)); gap: 12px; }
.js { padding: 12px 16px; border: 2px solid var(--rule); border-radius: 12px; background: var(--paper); }
.js p { margin: 0; }
.js-name { font-family: var(--mono); font-size: .875rem; color: var(--ink-3); margin-bottom: 4px !important; }
.js-fig { text-wrap: balance; font-size: 1.25rem; font-weight: 700; letter-spacing: -.01em; font-variant-numeric: tabular-nums; }
.js-fig .quiet { font-size: 1rem; font-weight: 400; letter-spacing: 0; }
.js-line { color: var(--ink-2); font-size: var(--type-meta); margin-top: 2px !important; font-variant-numeric: tabular-nums; }
.warn-line { color: var(--ar-text); }
.cases { --cols: 2.25rem minmax(14rem, 1fr) 6.5rem minmax(24rem, 1.5fr) 16px; --jcols: 5.75rem 9.5rem minmax(8rem, 1fr) 3.5rem; }
.cols { display: grid; grid-template-columns: var(--cols); gap: 6px 18px; padding: 0 10px 8px; border-bottom: 1px solid var(--rail); font-size: var(--type-label); font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); }
.cols-j { display: grid; grid-template-columns: var(--jcols); gap: 10px; }
.cols .r { text-align: right; }
.case + .case { border-top: 1px solid var(--rule); }
.case > summary {
  list-style: none; cursor: pointer; display: grid; gap: 6px 18px; align-items: start;
  grid-template-columns: var(--cols);
  padding: 16px 10px; border-radius: 6px; transition: background-color .15s ease-out;
}
.case > summary::-webkit-details-marker { display: none; }
.case > summary:hover { background: var(--paper-2); }
.cn { font-family: var(--mono); font-size: var(--type-meta); color: var(--ink-3); padding-top: 2px; }
.ct .t { display: block; font-weight: 600; text-wrap: pretty; }
.ct .id { display: block; font-size: .8125rem; color: var(--ink-3); margin-top: 2px; }
.ct .mark.split { display: inline-block; margin-top: 4px; }
.cx { display: flex; flex-direction: column; gap: 6px; align-items: flex-start; padding-top: 2px; }
.lbl { font-size: .6875rem; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); }
.cj { display: grid; gap: 8px; }
.jr { display: grid; grid-template-columns: var(--jcols); gap: 10px; align-items: center; }
.jn { font-family: var(--mono); font-size: .875rem; color: var(--ink); }
.jt { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
.lat { font-family: var(--mono); font-size: .875rem; color: var(--ink-2); text-align: right; white-space: nowrap; font-variant-numeric: tabular-nums; }
.chev { width: 16px; height: 16px; margin-top: 4px; fill: none; stroke: var(--ink-3); stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; transition: transform .2s cubic-bezier(.16, 1, .3, 1), stroke .15s ease-out; }
.case > summary:hover .chev { stroke: var(--ar); }
.case[open] .chev { transform: rotate(180deg); }


.score { display: flex; align-items: center; gap: 8px; min-width: 0; }
.bar { position: relative; flex: 1; min-width: 48px; height: 6px; border-radius: 999px; background: var(--track); }
.bar::before { content: ""; position: absolute; inset: 0 auto 0 0; width: max(var(--v), 6px); border-radius: inherit; background: var(--ink-3); }
.bar.efficient::before { background: var(--sy); }
.bar.capable::before { background: var(--ar); }
.tick { position: absolute; top: -6px; bottom: -6px; left: calc(var(--t) - 1px); width: 2px; border-radius: 1px; background: var(--ink); box-shadow: 0 0 0 2px var(--paper); }
.case > summary:hover .tick { box-shadow: 0 0 0 2px var(--paper-2); }
.score .n { font-family: var(--mono); font-size: .875rem; min-width: 2.6rem; text-align: right; font-variant-numeric: tabular-nums; }

.panel { padding: 6px 10px 26px calc(2.25rem + 28px); animation: fold-in .28s cubic-bezier(.16, 1, .3, 1); }
@keyframes fold-in { from { transform: translateY(-6px); } }
.jb-head { display: flex; align-items: baseline; gap: 10px; margin: 0 0 4px; }
.jh { font-size: 1.0625rem; font-weight: 700; letter-spacing: -.01em; margin: 0; }
.jb-head .kind { font-family: var(--mono); font-size: .75rem; color: var(--ink-3); }
.decision { margin: 0; display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 10px; font-size: var(--type-meta); color: var(--ink-2); }
.decision .dl { color: var(--ink-3); }
.calc { font-family: var(--mono); color: var(--ink); font-variant-numeric: tabular-nums; }
.arrow { color: var(--ink-3); }
.decision .tier { color: var(--ink); font-weight: 600; }
.raised { margin: 4px 0 0; font-size: var(--type-meta); color: var(--ar-text); }
.alert { margin: 8px 0 0; font-size: var(--type-meta); color: var(--danger); }
.jb-answer { margin-top: 16px; align-self: start; }
.calc-row { display: inline-flex; align-items: baseline; gap: 8px; white-space: nowrap; }
.prompt-label { margin: 4px 0 4px; font-size: var(--type-label); font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); }
.prompt { margin: 0; max-width: 48rem; color: var(--ink-2); white-space: pre-wrap; overflow-wrap: break-word; text-wrap: pretty; }
.judges-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 380px), 1fr)); grid-template-rows: auto auto auto; gap: 0 16px; margin-top: 20px; align-items: start; }
.judges-grid > .jb { display: grid; grid-row: span 3; grid-template-rows: subgrid; row-gap: 0; }
.jb-top { align-self: start; }
.jb { min-width: 0; padding: 16px 18px 18px; border: 2px solid var(--rule); border-radius: 12px; }
.verdict { margin: 0; color: var(--ink); font-size: 1.0625rem; line-height: 1.5; text-wrap: pretty; }
.verdict .quiet { display: block; margin-top: 4px; font-size: var(--type-meta); }
.keyline { margin: 10px 0 0; font-size: var(--type-meta); color: var(--ink); line-height: 1.7; font-variant-numeric: tabular-nums; }
.kl { color: var(--ink-3); }
.ev .fact { font-size: .8125rem; }
.ev { margin-top: 12px; }
.ev[open] > summary { margin-bottom: 8px; }
.fact { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 2px 16px; margin: 0; color: var(--ink-2); }
.fact dt { font-size: .6875rem; font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); padding-top: 2px; }
.fact dd { margin: 0; min-width: 0; overflow-wrap: anywhere; }
.ev > summary { cursor: pointer; font-size: var(--type-meta); color: var(--ink-3); width: max-content; }
.ev > summary:hover { color: var(--ink); }
.verdict.quiet { color: var(--ink-3); }
.answer-wrap { background: var(--paper-2); border: 1.5px solid var(--rule); border-radius: 10px; overflow: hidden; }
.answer { margin: 0; padding: 10px 14px; font-size: .84375rem; line-height: 1.55; color: var(--ink); white-space: pre-wrap; overflow-wrap: break-word; }
.answer.clip { max-height: 26em; overflow: hidden; -webkit-mask-image: linear-gradient(#000 calc(100% - 1.6em), rgb(0 0 0 / .55)); mask-image: linear-gradient(#000 calc(100% - 1.6em), rgb(0 0 0 / .55)); }
.more { display: block; width: 100%; text-align: left; font: inherit; font-size: var(--type-meta); font-weight: 600; color: var(--ink); background: none; border: 0; padding: 4px 14px 10px; cursor: pointer; }
.more:hover { color: var(--ar-text); }
.answer-none { margin: 0; color: var(--ink-3); font-style: italic; }
.trunc { font-size: var(--type-label); font-weight: 700; text-transform: uppercase; letter-spacing: .06em; color: var(--ink-3); }
.empty { padding: 32px 0; }

@media (max-width: 1040px) {
  .inc-lead { grid-template-columns: minmax(0, 1fr); }
  .cols { display: none; }
  .cases { border-top: 1px solid var(--rail); padding-top: 4px; }
  .case > summary { grid-template-columns: 2rem minmax(0, 1fr) 16px; grid-template-areas: "n t c" "n x x" "n j j"; }
  .cn { grid-area: n; } .ct { grid-area: t; } .chev { grid-area: c; }
  .cx { grid-area: x; flex-direction: row; align-items: center; flex-wrap: wrap; gap: 10px; }
  .cj { grid-area: j; }
  .panel { padding-left: 10px; }
}
@media (max-width: 560px) {
  main { padding: 0 18px 56px; }
  .top { flex-wrap: wrap; align-items: flex-start; }
  .h2row { flex-wrap: wrap; }
  .tools { margin-left: 0; }
  .setup-line { grid-template-columns: minmax(0, 1fr); gap: 2px; }
  .setup { padding: 14px 16px; }
  .inc-lead { grid-template-columns: minmax(0, 1fr); padding: 16px; }
  .jb { padding: 14px; }
  .answer.clip { max-height: 18em; }
  .decision { flex-direction: column; align-items: flex-start; gap: 2px; }
  .judges-grid { gap: 14px; grid-template-rows: none; }
  .judges-grid > .jb { display: block; grid-row: auto; }
  .keyline { line-height: 1.6; }
  .cj { gap: 8px; grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr)); }
  .jr { grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: "jn lat" "jt jt" "bar bar"; row-gap: 6px; padding: 10px 12px; background: var(--paper-2); border-radius: 10px; }
  .jn { grid-area: jn; font-weight: 600; } .jt { grid-area: jt; } .lat { grid-area: lat; } .jr .score { grid-area: bar; }
  .tick { box-shadow: 0 0 0 2px var(--paper-2); }

  .case > summary { padding: 14px 4px; }
  .tg { margin-right: 14px; }
}
@media (prefers-reduced-motion: reduce) {
  .panel { animation: none; }
  .chev, .case > summary { transition: none; }
}
@media print {
  .theme, .tools, .more, .chev { display: none !important; }
  main { max-width: none; padding: 0; }
  .case { break-inside: avoid; }
  .answer.clip { max-height: none; -webkit-mask-image: none; mask-image: none; }
  .bar, .bar::before, .tick, .tier::before { print-color-adjust: exact; -webkit-print-color-adjust: exact; }
}
"""


def main(argv):
    if len(argv) != 3:
        sys.stderr.write("usage: build_report.py RESULTS_JSON OUT_HTML\n")
        return 2
    try:
        with open(argv[1], encoding="utf-8") as f:
            data = json.load(f)
        page = render(data)
    except (OSError, ValueError) as error:
        sys.stderr.write("build_report: {}\n".format(error))
        return 1
    with open(argv[2], "w", encoding="utf-8") as f:
        f.write(page)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

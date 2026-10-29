"""Regenerates figures and RESULTS.md from study/ledger.csv.

Detection rate per bug and method with a Wilson 95% interval; Kaplan-Meier
median and p90 time to detect (wall seconds) with no-detection trials censored
at their budget.
"""
import math
import pathlib
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
import pandas as pd  # noqa: E402

HERE = pathlib.Path(__file__).parent
LEDGER = HERE / "ledger.csv"


def wilson(k, n, z=1.96):
    if n == 0:
        return (math.nan, math.nan)
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (max(0.0, c - h), min(1.0, c + h))


def km_quantiles(durations, events, qs=(0.5, 0.9)):
    """Kaplan-Meier quantiles; returns None where the curve never reaches q."""
    pairs = sorted(zip(durations, events))
    at_risk = len(pairs)
    surv = 1.0
    out = {q: None for q in qs}
    i = 0
    while i < len(pairs):
        t = pairs[i][0]
        d = c = 0
        while i < len(pairs) and pairs[i][0] == t:
            d += pairs[i][1]
            c += 1 - pairs[i][1]
            i += 1
        if d:
            surv *= 1 - d / at_risk
        for q in qs:
            if out[q] is None and 1 - surv >= q:
                out[q] = t
        at_risk -= d + c
    return out


def fmt(v):
    return "not reached" if v is None else f"{v:.2f}"


def main():
    if not LEDGER.exists():
        sys.exit(f"{LEDGER} not found: run seedlog study first")
    df = pd.read_csv(LEDGER)
    rows = []
    for (bug, method), g in df.groupby(["bug", "method"]):
        n, k = len(g), int((g.outcome == "found").sum())
        lo, hi = wilson(k, n)
        q = km_quantiles(g.wall_s.tolist(), (g.outcome == "found").astype(int).tolist())
        found = g[g.outcome == "found"].seeds_or_schedules
        rows.append((bug, method, k, n, lo, hi, q[0.5], q[0.9], found.median() if len(found) else None))
    out = ["# Study results", "",
           "Detection rate is k of n trials with a Wilson 95% interval. Median and p90 time to detect come from a",
           "Kaplan-Meier curve over wall seconds with no-detection trials censored at their budget.", "",
           "| bug | method | detected | rate (95% CI) | median s | p90 s | median seeds or schedules |",
           "|----|--------|----------|---------------|----------|-------|---------------------------|"]
    for bug, method, k, n, lo, hi, m, p, s in rows:
        out.append(f"| {bug} | {method} | {k}/{n} | {k / n:.2f} ({lo:.2f}-{hi:.2f}) | {fmt(m)} | {fmt(p)} | "
                   f"{'-' if s is None or math.isnan(s) else int(s)} |")
    (HERE / "RESULTS.md").write_text("\n".join(out) + "\n", newline="\n")

    fig, ax = plt.subplots(figsize=(8, 4))
    for method, g in df.groupby("method"):
        per = g.groupby("bug").apply(lambda x: (x.outcome == "found").mean(), include_groups=False)
        ax.bar([b + (0.2 if method == "stress" else -0.2) for b in per.index], per.values, width=0.4, label=method)
    ax.set_xlabel("bug")
    ax.set_ylabel("detection rate")
    ax.set_xticks(range(1, 11))
    ax.legend()
    fig.tight_layout()
    fig.savefig(HERE / "detection_rate.png", dpi=120)
    print(f"wrote {HERE / 'RESULTS.md'} and detection_rate.png from {len(df)} trials")


if __name__ == "__main__":
    main()

# Study pre-registration

Committed before the first full run.

- Bug order: 1 to 10 as in docs/PLAN.md, each under method `sim`, then `stress`.
- Budget B: pilot B = 2 min wall per trial, 3 trials per bug per method; full study B = 2 min wall per
  trial (the simulation side finishes in seconds, so a larger B changes only the censoring of `stress`).
- Trials: 30 per bug per method, each with an independent seed offset (`sim`) or a fresh cluster (`stress`).
- CPU allowance per trial: 1 core for both methods (`--cpus 1`; containers use `cpus: 1`).
- Statistics: detection rate k of 30 with a Wilson 95% interval; Kaplan-Meier median and p90 of wall
  seconds to first detection with no-detection trials censored at B. Failure signature is the pair
  (detector, invariant).
- Same fault menu, workload (including CAS and INCR), checkers and checker cadence for both methods.
- Results are reported whichever method wins.

Status: only the `sim` method can run in this environment; the `stress` method needs Docker (not
available when this was built), so its rows are absent and the table says so.

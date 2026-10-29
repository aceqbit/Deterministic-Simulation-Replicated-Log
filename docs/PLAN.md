# SeedLog plan (working copy)

Condensed from the optimized spec. Two phases, twelve milestones, two gates.

## Claims

1. One pure state machine runs in both the simulator and production.
2. A disk model with torn writes and lost fsyncs feeds that same code.
3. A measured detection comparison against a stress baseline with the same fault menu.
4. Choice-sequence shrinking over the whole fault schedule.

Prior art: TigerBeetle VOPR, FoundationDB simulation, Porcupine. Out of scope: membership changes,
multi-key transactions. Snapshots are a stretch.

## Architecture

core (pure) runs under two hosts, sim and runtime. Both feed check/ (invariants, Porcupine, oracle, liveness,
ledger) and telemetry (OTLP to Collector, Prometheus, Tempo, Loki, Grafana).

## Ten bugs and expected detectors

| # | Flag | Expected detector |
|---|------|-------------------|
| 1 | vote_before_persist | Election safety: two leaders in one term after a crash |
| 2 | skip_uptodate_check | Leader completeness; committed-entry ledger |
| 3 | fig8_commit | State-machine safety: a committed entry is replaced |
| 4 | truncate_always | Log matching; acknowledged write lost |
| 5 | no_stepdown | Bounded liveness; stale reads in the linearizability check |
| 6 | commit_past_end | Invariant: commit index never exceeds last log index |
| 7 | stale_leader_read | Linearizability: stale read |
| 8 | ack_before_fsync | Acknowledged-write ledger after a crash |
| 9 | double_apply_retry | Linearizability on CAS and increment ops |
| 10 | accept_torn_tail | WAL recovery check; log matching |

## Reference profile (sim/profile.go)

5 nodes, 60 simulated s per seed, 100 ops per simulated s (40% put, 40% get, 10% CAS, 10% incr over 16 keys),
50 ms heartbeat, 150-300 ms election timeout, per-seed fault selection.

## Targets (report, never edit)

10 of 10 bugs found; replay 100%; hash identical on 3 platforms; >= 1,000 simulated s per busy core-s;
0 allocs per event after warm-up; >= 85% parallel efficiency to 8 cores; telemetry overhead <= 2%;
>= 90% statement coverage of core, kv, wal; median shrinker reduction >= 90%.

## Study protocol

Same fault menu, workload, checkers and cadence (one checker run per 1,000 completed ops) for both methods.
Same CPU allowance and wall budget B per trial. Per bug and method: detection rate k of 30 with Wilson 95%
interval; Kaplan-Meier median and p90 with no-detection trials censored. Bug order, B and statistics are
committed in study/prereg.md before the first full run.

## Deviations recorded during the build

- go.mod `go 1.26.0`: the pinned OpenTelemetry v1.47.0 requires it; no separate `toolchain` line (redundant).
- Explorer metric attribute and coverage names follow the tables; the fault `kind` set is closed.
- The Checkers apply Raft's actual rules: leader completeness binds a leader only to entries committed in
  earlier terms; a delayed append from an earlier-term leader may remove an entry from a node that has not
  learned the commit (see logs/errors.md #5, #7, #8).
- The trace hash uses an inline-free `hash/fnv` writer over eight-byte little-endian words.
- Nemesis includes targeted crashes (after a vote, after an election) and fast restarts, added because
  uniform random crashes did not reach bugs 1 and 3 within the pilot budget.

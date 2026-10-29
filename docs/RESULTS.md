# Measured results

Machine: Intel i5-1335U, 12 threads, Windows 11, Go 1.26.0. Everything below was run in this repo; nothing
here was estimated.

## Gates

| Gate | Result |
|---|---|
| 1.1: 1,000 fault-free seeds elect a leader; hashes identical on two runs | pass (`logs/gate-1.1.txt`) |
| 1.2: tests, `FuzzRecover` 60 s, coverage >= 90% | pass; coverage core 97.6%, kv 100%, wal 100% (`logs/fuzz-wal.txt`: 7.2M execs, no failure) |
| 1.3: every fault kind fires; >= 80% coverage points; replay identical | pass: all 9 fault kinds fire, 46/46 points hit in 10,000 seeds, golden hashes identical across runs and GOMAXPROCS 1 and 8 |
| 1.4: checkers | pass: bad histories fail, good pass, oracle and Porcupine agree on 10,000 histories |
| p1: 10,000 all-fault seeds, zero violations | pass: 10,000/10,000 (`logs/gate-p1-explore.txt`) |
| p1: hash identical on 3 platforms | **not verified**: only windows/amd64 ran locally; the linux/amd64, linux/arm64 and windows CI matrix is written (`.github/workflows/ci.yml`) but has not run; linux/arm64 builds |
| 2.2: 10 of 10 bugs | pass (`study/pilot.csv`, `study/pilot.md`) |
| 2.5: shrink | schedules replay 100%; median reduction **85.8%, below the 90% target** (`shrink/REPORT.md`) |

## Performance targets (reported, not adjusted)

`bench/baseline.txt`, one core, reference profile, linearizability check off:

| Target | Measured |
|---|---|
| >= 1,000 simulated s per busy core-s | 1,624 (met) |
| 0 allocs per event after warm-up | 0.52 allocs/event (41,674 per seed): **not met**; no pprof pass was done |
| >= 85% parallel efficiency to 8 cores | not measured |
| telemetry overhead <= 2% | not measured (no collector available); instruments are no-ops without an endpoint |

## Study (simulation only)

`study/RESULTS.md`: 10 trials per bug, 20 s wall budget, 1 core (reduced from the pre-registered 30 trials
and a longer budget to fit this session). Bugs 1 (first found at seed 351) and 8 (seed 1,744) need more than
one core-20 s; bug 10 detected 4 of 10. The stress method was **not run**, so there is no sim-vs-stress
comparison.

## Not built or not run

Docker-based pieces could not run (no Docker): `make obs-up`, dashboards populated, `make cluster-up` and
`e2e`, the stress nemesis and stress pilot, alert test, calibration. Their config files exist under
`deploy/` and are unverified. Not implemented: runtime Cluster metrics instruments, `workload` container
command, `bench-telemetry`, parallel-efficiency bench, the P2.1 profile-led optimizations, snapshots.

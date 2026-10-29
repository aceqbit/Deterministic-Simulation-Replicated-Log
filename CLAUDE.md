# SeedLog

Raft-style replicated log in Go, run inside a deterministic simulator. The deliverable is a measured result:
simulation finds 10 injected bugs faster than a stress test on the same code, and every failure replays from a
seed. It is not "a Raft in Go".

Source of truth: `docs/PLAN.md`. Work one milestone per session and stop at its gate.

## Commands

    make build | test | lint        # go build, go test -race ./..., staticcheck + golangci-lint (depguard)
    make golden                     # regenerate golden trace hashes for seeds 1..100 (must not change)
    make sim-smoke                  # 2,000 seeds, all faults, zero violations
    make bench                      # events/s, allocs/event
    make gate-1.1 ... gate-1.4 | gate-2.2 | gate-2.5 | gate-p1
    seedlog sim|explore|replay|shrink|pilot|study|node
    seedlog replay --seed N [--otel]   # reproduces any failure event for event

## Layout

- `core/` Raft state machine: `Step(Event) -> ([]Effect, []Observation)`. Pure.
- `kv/` client sessions (clientID, seq), PUT, GET, CAS, INCR. Pure.
- `wal/` CRC32C (Castagnoli) framing, recovery to last valid record, Storage interface. Pure.
- `sim/` event heap, virtual clock, ChoiceSource, network/disk/process models, nemesis, coverage points.
- `runtime/` UDP transport, file Storage, real timers, HTTP API. Only place with goroutines (besides cmd).
- `check/` online invariants, Porcupine adapter, brute-force oracle, bounded liveness, acked-write ledger.
- `shrink/` choice-sequence minimizer. `telemetry/` OpenTelemetry setup and instruments.
- `deploy/`, `stress/`, `study/`, `docs/`, `logs/`.

## Determinism rules (never break)

1. `core/`, `kv/`, `wal/` import none of: time, math/rand, math/rand/v2, os, net, sync, telemetry (depguard).
2. Time arrives as Tick/timer events. Randomness exists only in sim `ChoiceSource`. The core requests jitter via
   `SetTimer(kind, base, jitter)`; the host draws it.
3. Persistence is an effect: `Persist(batch, token)`. Votes, AppendEntries acks and client replies leave only
   after `PersistDone(token)`.
4. sim uses integers only, one thread, no goroutines. Sort map keys before iterating.
5. Bug flags live in core config. The simulator never draws from the ChoiceSource conditionally on a flag.
6. Trace hash is incremental FNV-1a 64 over fixed-width little-endian encodings; identical on all platforms.
7. A performance change is valid only if `make golden` is unchanged.

## Telemetry rules

Telemetry observes and never steers. No OpenTelemetry call on the simulator hot path; workers keep plain
counters and flush once per seed. Labels have closed value sets; never label with seed, trace ID or key.
Pin all go.opentelemetry.io module versions.

## Workflow

- Tests first. Run `make test lint` and the milestone gate before saying done; paste gate output.
- A failing seed is a finding: replay, shrink, write a failing regression test, then fix `core/`.
  Never loosen a checker; change one only when the oracle shows it is wrong.
- A linearizability timeout is Unknown, never a pass.
- Every build/test/gate failure and its fix goes in `logs/errors.md`.
- One commit per logical change, message `m1.2: add commit rule`; tag passed gates.
- Standard library first. Extra modules allowed: OpenTelemetry, protobuf, porcupine.

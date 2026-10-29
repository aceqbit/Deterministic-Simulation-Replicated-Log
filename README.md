# SeedLog

A Raft-style replicated log in Go, run inside a deterministic simulator. One pure state machine
(`core/`, `kv/`, `wal/`) runs unchanged under two hosts: the simulator (`sim/`) and a real UDP/HTTP
runtime (`runtime/`). Every simulated failure replays from a seed, and ten injected Raft bugs measure how
fast simulation finds them.

Deterministic simulation testing, linearizability checking and shrinking are prior art. Credit goes to
TigerBeetle's VOPR, FoundationDB's simulation testing, and Porcupine (Anish Athalye).

## Reproduce a failing seed

    go build -o seedlog ./cmd/seedlog
    ./seedlog explore --seeds 10000            # any failure prints: seedlog replay --seed N
    ./seedlog replay --seed N                  # reruns and verifies an identical trace hash
    ./seedlog shrink --seed N --bug K          # minimizes the choice sequence, writes a schedule file
    ./seedlog replay --schedule schedule-N.json

Inject a bug with `--bug K` (1 to 10, see `docs/PLAN.md`): `./seedlog explore --bug 6 --seeds 5000`
stops at the first failure and prints detector, invariant, seed and event count.

## Layout

| Package | Role |
|---|---|
| `core/` | Raft state machine, `Step(Event) -> (effects, observations)`. No clock, I/O or randomness. |
| `kv/` | Client sessions (clientID, seq), PUT, GET, CAS, INCR. |
| `wal/` | CRC32C record framing, recovery to the last valid record, `Storage`. |
| `sim/` | Event heap, virtual clock, PCG choice source, network/disk/process faults, nemesis, coverage. |
| `check/` | Online invariants, Porcupine adapter, brute-force oracle, liveness, acknowledged-write ledger. |
| `shrink/` | Choice-sequence minimizer. |
| `runtime/` | UDP + protobuf wire, file storage with fsync, HTTP API (`/kv/{key}`, `/healthz`, `/readyz`). |
| `telemetry/` | OpenTelemetry setup and instruments; a no-op unless an OTLP endpoint is set. |
| `deploy/` | Observability stack, dashboards, alert rules, Compose cluster (not exercised here). |

`pure` packages may not import `time`, `math/rand`, `os`, `net`, `sync` or `telemetry`; `golangci-lint`
(depguard) enforces it. See `CLAUDE.md` for the determinism rules.

## Status and results

See `logs/errors.md` for every failure met during the build and its fix, `study/pilot.csv` for the
10-bug pilot, and `docs/RESULTS.md` for measured numbers against the targets.

## Limits

- No membership changes and no multi-key transactions. Snapshots are not implemented.
- The disk is a model (volatile buffer; kept, dropped or torn at 512-byte sector granularity).
- The simulator is not calibrated against a real cluster (`docs/CALIBRATION.md`).
- The stress baseline needs Docker; it was not run, so the study table has the simulation column only.

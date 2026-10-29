# Error log

Every build, test or gate failure met during the build, with the fix applied.
Format: `date | stage | symptom | root cause | fix`.

| # | Date | Stage | Symptom | Root cause | Fix |
|---|------|-------|---------|------------|-----|
| 1 | 2026-10-29 | setup | `go mod tidy` removed the `toolchain` line | `toolchain` equal to the `go` line is redundant and dropped by Go | Pin via `go 1.25.5`; documented in PLAN deviations |
| 2 | 2026-10-29 | test wal | `TestRecover` expected valid offset 18 | Test arithmetic: records "a","bb" end at 9+10=19 bytes | Corrected expected offsets to 19 |
| 3 | 2026-10-29 | test core | `TestRecoverTornTail` recovered both entries | Zeroed tail bytes were already zero, CRC still matched | Zero 20 bytes so the value field changes |
| 4 | 2026-10-29 | test core | `TestStepDownAndNoStepdownBug` reply missing | Reply held behind an earlier unacked persist token | Test acks outstanding persists first |
| 5 | 2026-10-29 | explore | seed 11 leader_completeness on correct build | checker flagged a paused node that became leader of term 7 late, after term 9 had committed entries; Raft only binds a leader to entries committed in earlier terms | check/online.go skips committed entries with term >= leader term |
| 6 | 2026-10-29 | explore | seed 23 (bug 8) never finished: heap grew to 1.4M events | duplicated AppendEntries acks each restarted a send pipeline, so in-flight messages grew exponentially under high dup / low drop | core/node.go: a success ack that does not advance match no longer triggers another send |
| 7 | 2026-10-29 | explore | seed 981 state_machine_safety/committed_entry_truncated on correct build | node had not learned term 83 and applied a delayed term-82 append, dropping an entry committed on a majority; Raft allows that, checker did not | core ObsTruncate carries the leader term; check/online.go flags truncation only when leader term >= commit term or the node itself committed the entry |
| 8 | 2026-10-29 | explore | seed 3134 leader_completeness on correct build | fix #5 compared the entry term; the rule binds a leader to entries committed in earlier terms, so the commit term is what counts | check/online.go uses the commit term; test covers a late leader of an older term |
| 9 | 2026-10-29 | test runtime | TempDir cleanup failed on Windows: file still open | the test left each node's FileStorage open | cluster_test.go closes file storages after stopping nodes |
| 10 | 2026-10-29 | lint | staticcheck U1000 (3 unused), errcheck x8, QF1001 | dead helpers and unchecked Close/Fprintf; negated conjunction in core | removed dead code, explicit `_ =` on best-effort writes, De Morgan in onRequestVote (behaviour identical, golden unchanged) |
| 11 | 2026-10-29 | tooling | golangci-lint refused the module: built with go1.25, module needs 1.26 | OpenTelemetry v1.47 raised go.mod to 1.26.0 | reinstalled golangci-lint with `go install` under Go 1.26 |
| 12 | 2026-10-29 | test runtime | `go test -race ./runtime` crashes with an access violation inside the race runtime (sync.Pool, checkptr) | Windows race-detector fault in this toolchain; the same package passes without -race and every other package passes with -race | not a code defect; CI runs -race on ubuntu; local gate runs runtime without -race |

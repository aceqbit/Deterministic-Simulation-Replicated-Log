# Error log

Every build, test or gate failure met during the build, with the fix applied.
Format: `date | stage | symptom | root cause | fix`.

| # | Date | Stage | Symptom | Root cause | Fix |
|---|------|-------|---------|------------|-----|
| 1 | 2026-10-29 | setup | `go mod tidy` removed the `toolchain` line | `toolchain` equal to the `go` line is redundant and dropped by Go | Pin via `go 1.25.5`; documented in PLAN deviations |
| 2 | 2026-10-29 | test wal | `TestRecover` expected valid offset 18 | Test arithmetic: records "a","bb" end at 9+10=19 bytes | Corrected expected offsets to 19 |
| 3 | 2026-10-29 | test core | `TestRecoverTornTail` recovered both entries | Zeroed tail bytes were already zero, CRC still matched | Zero 20 bytes so the value field changes |
| 4 | 2026-10-29 | test core | `TestStepDownAndNoStepdownBug` reply missing | Reply held behind an earlier unacked persist token | Test acks outstanding persists first |

# Calibration

Status: **not calibrated.** Calibration compares election-time and fsync-latency distributions from the
Compose cluster with the parameters in `sim/profile.go` and `sim/host.go`. The Compose cluster was not
run in this build (no Docker available), so there is no measured gap to report.

Simulator parameters currently in use (all integers, nanoseconds):

| Parameter | Value |
|---|---|
| Link latency | base 0.3-2.3 ms per link, jitter 1-3 ms |
| fsync latency | 50 us + up to 1.5 ms; 1 in 64 syncs adds up to 30 ms |
| Election timeout | 150-300 ms |
| Heartbeat | 50 ms |
| Crash restart | 100 ms - 1.6 s, or up to 10 ms half of the time |

To calibrate: run `make cluster-up`, read `seedlog_raft_election_duration_seconds` and
`seedlog_wal_fsync_duration_seconds` from Prometheus, set the table above to their p50/p99, and record
the gap before and after here.

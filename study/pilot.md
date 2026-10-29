# Pilot: 10 bugs, 10,000 seeds each

`study/pilot.csv` is produced by `seedlog pilot --seeds 10000`. All ten bugs were detected; no bug is
recorded as missed. `seeds_to_detect` is the seed number of the first failing seed (seeds start at 1).

| # | Bug | Seeds to detect | First detector / invariant | Expected detector | Match |
|---|-----|-----------------|----------------------------|-------------------|-------|
| 1 | vote_before_persist | 351 | election_safety / two_leaders_same_term | election_safety | yes |
| 2 | skip_uptodate_check | 1 | leader_completeness / leader_missing_committed_entry | leader_completeness | yes |
| 3 | fig8_commit | 1 | state_machine_safety / old_term_entry_committed_by_count | state_machine_safety | yes (rule check, see below) |
| 4 | truncate_always | 1 | state_machine_safety / committed_entry_truncated | log_matching | no |
| 5 | no_stepdown | 1 | election_safety / two_leaders_same_term | liveness | no |
| 6 | commit_past_end | 1 | commit_bound / commit_past_last_index | commit_bound | yes |
| 7 | stale_leader_read | 1 | linearizability / history_not_linearizable | linearizability | yes |
| 8 | ack_before_fsync | 1744 | leader_completeness / leader_missing_committed_entry | ack_ledger | no |
| 9 | double_apply_retry | 1 | linearizability / history_not_linearizable | linearizability | yes |
| 10 | accept_torn_tail | 2402 | wal_recovery / checksum_failing_record_accepted | wal_recovery | yes |

## Mismatches to explain (findings, not failures)

- **4 truncate_always.** Truncating matching entries removes entries that a node already committed, which
  the state-machine-safety check sees before log matching can diverge.
- **5 no_stepdown.** A leader that adopts a higher term but stays leader is a second leader of that term as
  soon as another node wins it, so election safety fires before bounded liveness or a stale read.
- **8 ack_before_fsync.** The ledger needs a majority to have lost the same acknowledged entry; a new leader
  that lacks a committed entry trips leader completeness first.

## Notes on how the pilot got here

- Uniform random crashes did not reach bugs 1 and 3 in 10,000 seeds. The nemesis gained targeted crashes
  (after a granted vote, after an election, the current leader), a restart-immediately mode, and a
  leader-isolating partition; the replication batch in the simulator is 4 entries so that catch-up spans
  several acknowledgements. No checker was loosened: every checker change in `logs/errors.md` (#5, #7, #8)
  removed a false positive on the correct build and was backed by a unit test.
- Bug 3 is detected by a rule check, `old_term_entry_committed_by_count`: a leader committing an older-term
  entry by counting replicas. The end-state symptom (a committed entry later replaced, Figure 8) did not
  occur within 10,000 seeds, so the study reports the earlier, rule-level detection and says so.
- Each seed's failure signature is the (detector, invariant) pair, locked in `sim/bugs_test.go`.

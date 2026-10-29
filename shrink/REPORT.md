# Shrink report

`seedlog shrink --seed N --bug K` minimizes the recorded choice sequence (block deletion, block zeroing,
value lowering) and keeps a change only if the same (detector, invariant) fires and the run does not grow.
Schedules are in `shrink/schedules/`; `seedlog replay --schedule FILE` reproduced the signature for all ten.

| Bug | Seed | Events before | Events after | Removed | Attempts | Time |
|-----|------|---------------|--------------|---------|----------|------|
| 1 vote_before_persist | 351 | 17,786 | 217 | 98.8% | 2,000 | 67.5 s |
| 2 skip_uptodate_check | 1 | 8,383 | 176 | 97.9% | 2,000 | 63.6 s |
| 3 fig8_commit | 1 | 15,421 | 197 | 98.7% | 2,000 | 136.3 s |
| 4 truncate_always | 1 | 184 | 47 | 74.5% | 2,000 | 1.2 s |
| 5 no_stepdown | 1 | 5,874 | 97 | 98.3% | 2,000 | 74.3 s |
| 6 commit_past_end | 1 | 2,483 | 841 | 66.1% | 2,000 | 217.2 s |
| 7 stale_leader_read | 1 | 51,876 | 51,876 | 0% | 150 | 19.6 s |
| 8 ack_before_fsync | 1,744 | 21,411 | 21,411 | 0% | 150 | 49.8 s |
| 9 double_apply_retry | 1 | 49,122 | 49,122 | 0% | 150 | 26.4 s |
| 10 accept_torn_tail | 2,402 | 34,356 | 1,046 | 97.0% | 2,500 | 159.6 s |

**Median reduction: 85.8%, against the 90% target (missed).** Seven of ten bugs shrink by 66% to 99%.

Bugs 7 and 9 are only detected by the end-of-run linearizability check, and bug 8 (as pilot-detected) at
the end of its run, so every candidate re-run costs a full 60 simulated seconds plus a Porcupine check
(about 0.1-0.4 s each). The 150-attempt budget used here removed nothing for those three; a larger budget
or checkpointed linearizability (the M2.1 optimization, not built) is needed. Reproduction of the
minimized schedules is 100% (deterministic replay); the stress replay test was not run (no stress runs).

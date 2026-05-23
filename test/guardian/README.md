# x/guardian Integration Tests

End-to-end coverage for [x/guardian](../../x/guardian/) — the authority-gating proxy that owns the `Authority` address for `bank`, `mint`, `staking`, `distribution`, `gov`, `slashing`, `auth`, and `consensus`, and routes gov-submitted `MsgUpdateParams`-style msgs through per-msg-type field filters before dispatch.

The unit tests in [x/guardian/keeper/msg_server_test.go](../../x/guardian/keeper/msg_server_test.go) exhaustively cover the filter logic in isolation. These e2e scripts verify the integration story end-to-end on a live chain:

- the wiring in [app/app_config.go](../../app/app_config.go) routes the listed modules' authority to the guardian module address;
- `guardian.MsgExec` invoked from a gov proposal passes the inner-msg authority check;
- guardian's switch arms correctly accept tunable fields and reject immutable / out-of-bounds fields;
- the proposal status machine reflects the result (`PROPOSAL_STATUS_PASSED` for legitimate updates, `PROPOSAL_STATUS_FAILED` for filter rejections).

## Layout

| File                          | What it covers                                                                                      |
|-------------------------------|-----------------------------------------------------------------------------------------------------|
| `_common.sh`                  | Helpers: `submit_proposal`, `vote_yes`, `wait_voting`, `check_status`, `guardian_exec_proposal`.    |
| `allowlist_test.sh`           | `q guardian allowed-msgs` returns the 10-entry list; non-allowlisted inner (`bank.MsgSend`) rejected. |
| `mint_filter_test.sh`         | 5 immutable fields rejected (`inflation_*`, `goal_bonded`, `mint_denom`); `blocks_per_year` passthrough. |
| `staking_filter_test.sh`      | `bond_denom` rejected; `max_validators` passthrough.                                                |
| `bank_filter_test.sh`         | `SetSendEnabled` on native bond / native dream / `use_default_for` native rejected; foreign denom OK; `MsgUpdateParams` no-op passthrough. |
| `distribution_filter_test.sh` | `MsgCommunityPoolSpend` hard reject; `community_tax` floor (0.05) and ceiling (0.25) enforced; in-band update passthrough. |
| `gov_filter_test.sh`          | `voting_period` < 6h, `quorum` < 0.20, `threshold` < 0.50, `veto_threshold` < 0.20 rejected.        |
| `slashing_filter_test.sh`     | Zero `slash_fraction_double_sign` / `slash_fraction_downtime` rejected; oversized `signed_blocks_window` rejected; `downtime_jail_duration` passthrough. |
| `auth_filter_test.sh`         | Zero gas-cost floors rejected; `max_memo_characters` passthrough.                                   |
| `consensus_filter_test.sh`    | Tiny `block.max_bytes` / `block.max_gas` / `evidence.max_age_num_blocks` rejected; `max_gas=-1` (unlimited) accepted. |
| `run_all_tests.sh`            | Driver. Each test runs in sequence; flags `--only-<name>` and `--no-<name>` available.              |

## Batching strategy

Each test file submits all of its proposals in a single voting cycle. The naive structure would be "submit one, wait 60s, check, submit next, wait again" — at ~30 proposals across the suite that would take an hour. Instead, every test file's flow is:

1. Build N proposal JSON files (each mutating one field).
2. `submit_proposal` and `vote_yes` for each in a tight loop (~2-5s per proposal).
3. `wait_voting` ONCE (75s — covers the configured 60s voting period plus a safety margin).
4. `check_status` for every proposal id.
5. Verify post-state matches expectations.

Total wall time per file is ~80-90s regardless of how many filter cases it covers. With 9 files, the suite runs in roughly 12-15 minutes.

## Why no positive-passthrough test in `gov_filter_test.sh`?

`gov.MsgUpdateParams` floors are designed to prevent gov from *weakening* itself. The natural positive test would *strengthen* a field (e.g. raise `voting_period` from 60s to 24h). Doing so would permanently slow every subsequent gov-proposal-based e2e test on the same chain, including the rest of this suite. Mint's passthrough test already proves the guardian-routing infrastructure works end-to-end; the negative cases here are what's specific to gov's filter.

## Why no MsgExec-authority test?

The keeper unit test [`TestExecRejectsWrongAuthority`](../../x/guardian/keeper/msg_server_test.go) covers the case where a non-gov address signs `MsgExec`. There's no clean CLI path to construct a `MsgExec` tx directly (guardian has no Tx autocli; the inner is a `google.protobuf.Any` not expressible as flags), so building this case at the e2e level would require hand-rolled tx-signing JSON. The unit test already exercises the same authority check on the same code path, so duplicating it in shell would add fragility without raising confidence.

## Prerequisites

- `sparkdreamd` binary on `PATH` (built with the `testparams` build tag for 60s voting periods).
- Chain running locally with the standard test genesis (`alice` and `bob` in the keyring, both holding stake).
- `jq` for JSON parsing.

## Running

```bash
# Whole suite
./test/guardian/run_all_tests.sh

# One test only
./test/guardian/run_all_tests.sh --only-mint_filter

# Skip the slowest one
./test/guardian/run_all_tests.sh --no-consensus_filter
```

Within the larger test harness, this suite is wired into [test/run_all_tests.sh](../run_all_tests.sh) and [test/run_parallel.sh](../run_parallel.sh) under `MODULE_ORDER`.

## Related Documentation

- [x/guardian/keeper/msg_server.go](../../x/guardian/keeper/msg_server.go) — the switch arms and filter functions
- [docs/x-identity-spec.md §14.6](../../docs/x-identity-spec.md) — design rationale for routing authority through guardian
- [docs/security-hardening.md](../../docs/security-hardening.md) — broader hardening posture

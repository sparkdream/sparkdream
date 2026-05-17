# x/service E2E test suite

End-to-end shell tests for the `x/service` module. Matches the layout
used by other modules (forum, federation): a `run_all_tests.sh`
runner with per-test `--no-X` flags, an account-setup script that
seeds the chain state needed by every test, and per-feature test
scripts.

## Prerequisites

- Local `sparkdreamd` chain running with the `testparams` build tag.
- `alice` account exists in the test keyring with SPARK + DREAM
  (genesis account; same precondition as every other test suite).
- `x/rep`, `x/commons`, and `x/distribution` modules functional —
  setup invites test accounts to x/rep and queries an existing
  Commons Council group policy address to use as the operator
  controller.

## Quick start

```
$ bash test/service/run_all_tests.sh
```

Use `--save-setup` / `--restore-setup` for fast iteration once the
snapshot exists (same pattern as other suites — see
`run_all_tests.sh --help`).

## What each test covers

| Script              | Coverage                                                                       |
|---------------------|--------------------------------------------------------------------------------|
| `setup_test_accounts.sh` | Fund accounts; invite to x/rep; submit single gov proposal that (a) enables the `test-akash` service type with short timing knobs and (b) lowers `min_reporter_trust_level` to `TRUST_LEVEL_NEW` so freshly-invited reporters can file. Exports `.test_env` |
| `register_test.sh`  | `MsgRegisterOperator` happy path + every spec §5.1 rejection: self-controller, non-group controller, denom mismatch, insufficient bond, duplicate registration |
| `lifecycle_test.sh` | `MsgUpdateMetadata`, `MsgTopUpBond`, `MsgUnbondOperator` → wait → `MsgClaimUnbondedBond`; verifies UNBONDING state, archived RETIRED record, bond return |
| `report_test.sh`    | `MsgReportOperator` happy path + rejections: non-member reporter; controller-member reporter; rate-limit cap; `Query/Report` and `Query/ReportsByOperator` shape verification |

## Not yet covered

The controller-signed paths (`MsgResolveReport`, `MsgContestSlash`,
`MsgResolveReportByJury`, `MsgFinalizeControllerTransfer`) require
the controller's signature, which in production is an x/commons
Group policy address rather than an EOA. Submitting these msgs
end-to-end requires wrapping them in a `tx commons submit-proposal`
flow (council vote) — substantially more orchestration than the
operator-signed paths above. These are good follow-ups; the
keeper-level state-machine logic is already unit-tested by the
existing `x/service/keeper/*_test.go` files.

Similarly, the full jury-resolver flow (`Open*Case → jurors vote →
TallyJuryVotes → council member submits ResolveByJury`) is a
multi-step orchestration across x/rep and x/commons and is out of
scope for the initial E2E pass.

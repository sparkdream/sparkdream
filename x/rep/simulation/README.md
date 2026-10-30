# x/rep simulation operations

Every operation delivers real transactions through the message handlers;
none writes a store or calls a keeper setter (keeper reads only).

- `env.go`: the operation wrapper, dry-run through `MsgServiceRouter()` on a
  cache of the block state (panics recovered), delivery with
  `GenAndDeliverTxWithRandFees` (small fees for x/commons seat holders), and
  `firstValid`, which delivers the first candidate the handler accepts or
  returns a NoOp with a reason. A delivered tx the handler rejects fails the
  simulation.
- `prereqs.go`: prerequisites built by delivering real messages (project ->
  initiative -> assignment -> submission).
- `council.go`, `groups.go`: messages only a group policy may sign
  (project completion, interim approval, member-report resolution, tag
  budgets, forced migrations) go through an x/commons proposal, one step
  (submit, vote, execute) per call.
- `jury.go`: seated sim jurors cast real `MsgSubmitJurorVote` txs.
- `migration_helpers.go`, `zk_keys.go`: key migration and recovery run for
  real, but the simulated genesis (`module/simulation.go`) stretches the
  migration delays past any run so no migration finalizes (a tombstone
  could no longer sign the SDK modules' simulated txs). ZK keys are derived
  from each sim account's own key.

NoOp-only by design: `MsgSubmitExpertTestimony` (no code path seats
expert witnesses) and `MsgSweepTombstone` (no tombstone exists).

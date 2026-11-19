# Chain upgrades

How a release that changes the state machine reaches a running chain without
a reset. The machinery is wired (handler registration, store loader, the
Technical Council's `MsgForceUpgrade`), and the first entry in
`upgrades.All`, `storage-pinned-index-rep-sessions`, is listed for the next release: the
x/blog v2 pinned post index, the x/session v2 migration that admits the
x/rep work and staking messages and the x/storage worker messages to the
session allowlist, and the new x/storage module (its store, the x/service v2
migration that writes the `storage-arweave` service type, and the x/commons
v3 migration that gives the Commons Council and its Operations Committee the
storage council messages, and the Technical and Ecosystem Councils the
messages to propose and vote as members of the Commons Supervisory Board).
The x/commons v4 migration, folded into the same entry, gives the three
councils and their Operations Committees the treasury staking messages
([x-commons-spec.md](x-commons-spec.md) §7.2); the x/gov tally change
(treasury stake has no voting power) ships with it and has no stored state
to migrate. The same migration removes every forbidden message, the raw
staking messages included, from every stored allowlist, and writes the
`TreasuryStakers` index for any group that already holds a delegation.
The veto-seat and golden-share recusal rules of x/commons ship with it and
need no stored state. It also carries one change with no migration: x/rep
refuses a `council_migration_delay` of zero or below
`recovery_migration_delay`. The delay's new default (7 days, 2 on testnet,
1 on devnet) is a genesis value, so a running network keeps its stored 21 days
until an x/gov `MsgUpdateParams` changes it. Until a
network applies it, earlier consensus-affecting releases shipped by resetting
the networks (or by swapping binaries at a `halt-height`, which works only
when replaying old blocks on the new binary gives the same results). A reset
needs no migrations: a fresh genesis starts every module at its current
`ConsensusVersion`.

## When a release needs a named upgrade

A release needs one when it changes what the chain computes from the same
blocks:

- new or changed messages, payload variants, validation or errors in any
  `x/*/keeper` or `x/*/types` path that runs in a transaction, BeginBlock or
  EndBlock;
- a changed proto field type or number on anything stored (this also needs a
  migration, see below);
- new params, stores or modules.

Simulation, REST gateway, CLI and docs changes do not. When unsure, add one:
an upgrade with nothing to migrate costs a coordinated restart and nothing
else.

## Adding an upgrade

1. Append an entry to `upgrades.All` in [app/upgrades/upgrades.go](../app/upgrades/upgrades.go).
   A release adds at most one entry: a binary that already holds the handler
   for a plan still pending panics with `BINARY UPDATED BEFORE TRIGGER`, so a
   second new entry could never be scheduled on the binary that carries it.
   Fold every consensus change of a release into its one upgrade until a
   network applies it, and name the upgrade for what it carries. Never
   rename or remove an entry once a network has applied it; a node replaying
   through that height needs the handler to still exist.
2. Handler: `RunMigrations` covers both "nothing to migrate" and "migrations
   registered on bumped module versions". For a stored-state change, bump the
   module's `ConsensusVersion`, register the migration in the module's
   `RegisterServices` (`cfg.RegisterMigration(types.ModuleName, from, fn)`), and
   keep `RunMigrations`. Write a custom handler only for cross-module work.
3. `StoreUpgrades`: list stores added, renamed or deleted (a new module's store
   key goes in `Added`). [app/upgrades.go](../app/upgrades.go) applies them when
   the node restarts at the upgrade height.
4. Params added to an existing module decode as zero on a running chain until
   a migration sets them. Set them in the migration, or prefer a constant when
   the value never needs governance (as `MaxSignInGrantsPerGranter` does).
5. `go test ./app/ -run Upgrade` checks that every listed upgrade has a handler
   and applies cleanly.

## Running an upgrade

1. **Release.** Ship the release as usual (prepare-release, images). The
   release binary must carry the handler before any node reaches the plan
   height.
2. **Schedule.** The Technical Council's standard policy submits
   `MsgForceUpgrade{plan: {name, height, info}}` through
   `sparkdreamd tx commons submit-proposal`. The council holds the lead
   developer's weight-3 seat and the Commons Council policy's seat, whose
   weight always equals all other members' together, under a 66% threshold.
   The plan executes only after the Commons Council passes its own proposal
   voting yes on it (the golden share,
   [test/commons/tech_upgrade_golden_share.sh](../test/commons/tech_upgrade_golden_share.sh)).
   Pick a height far enough out for both votes plus the image rollout. `info`
   may carry the release tag. Query the pending plan with
   `GET /cosmos/upgrade/v1beta1/current_plan`.
3. **Halt.** At the plan height every node running the old binary stops with
   `UPGRADE "<name>" NEEDED at height: <h>` and writes
   `data/upgrade-info.json`. Nothing advances until the new binary starts.
4. **Swap.** Restart every validator and sentry on the release image. The new
   binary reads `upgrade-info.json`, applies any store upgrades, runs the
   handler in the plan-height block, and the chain resumes once two-thirds of
   voting power is back.
5. **Check.** `GET /cosmos/upgrade/v1beta1/applied_plan/<name>` returns the
   height it ran at.

A node that starts the new binary before the plan height panics with
`BINARY UPDATED BEFORE TRIGGER! UPGRADE "<name>"`: put the old image back
until the halt. A node still on the old binary after the halt repeats the
`NEEDED` panic: give it the new image.

To change a pending plan, the Technical Council passes another
`MsgForceUpgrade`: x/upgrade keeps one plan and the new one replaces it. To
call it off, an x/gov proposal carries `/cosmos.upgrade.v1beta1.MsgCancelUpgrade`
(the council does not hold that message). As a last resort every node starts
with `--unsafe-skip-upgrades <height>`; the handler then never runs, so only
for a plan whose release changed nothing.

## Rehearsing an upgrade

`go test ./app/ -run Upgrade` runs the handler against state the test writes
itself. A rehearsal runs it on a node, against state the previous release
wrote. Do one before every release that carries an entry:

1. Build the previous release and start a chain from it. Take the source with
   `git archive <tag> | tar -x -C <dir>` and run
   `ignite chain init --build.tags testparams` there; this installs the old
   `sparkdreamd` and writes a fresh home. Start it.
2. Write state the migrations touch (for `storage-pinned-index-rep-sessions`:
   a pinned blog post).
3. Schedule the plan through the councils, as a network would:
   `UPGRADE_NAME=<name> UPGRADE_HEIGHT=<height> bash test/commons/tech_upgrade_golden_share.sh`.
   Pick a height about 120 blocks out.
4. Wait for `UPGRADE "<name>" NEEDED at height: <h>` in the node log, stop the
   node, and check that starting the old binary again repeats the message.
5. Build the current tree (`ignite chain build --build.tags testparams`) and
   start it on the same home. The log shows `applying upgrade` and one
   `migrating module` line per bumped module, and blocks continue.
6. Check `sparkdreamd query upgrade applied <name>`, the migrated state, and
   run the e2e suite of every module the release adds on the upgraded chain
   (`bash test/<module>/run_all_tests.sh --no-auto-snapshot`).

`storage-pinned-index-rep-sessions` was rehearsed this way from v1.0.58: the
old node halted at the plan height, the new one migrated blog, commons,
service and session, the post pinned before the upgrade appeared in
`list-pinned-posts`, the session allowlist grew from 76 to 91 types, both
Commons bodies held the storage council messages, and the x/storage suite
passed on the upgraded chain. That rehearsal predates the x/commons v4 migration
(treasury staking), which was folded in afterwards and is covered by
`TestUpgradeGrantsTreasuryStakingMsgs` only.

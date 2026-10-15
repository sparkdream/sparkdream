# Chain upgrades

How a release that changes the state machine reaches a running chain without
a reset. The machinery is wired (handler registration, store loader, the
Technical Council's `MsgForceUpgrade`) but no upgrade has been applied yet:
while the networks hold little content, consensus-affecting releases still
ship by resetting them (or by swapping binaries at a `halt-height`, which
works only when replaying old blocks on the new binary gives the same
results), and `upgrades.All` stays empty. Everything below applies from the
first release a running network upgrades to instead. A reset needs no
migrations: a fresh genesis starts every module at its current
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

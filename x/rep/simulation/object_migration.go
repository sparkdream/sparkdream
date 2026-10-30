package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgObjectMigration has the old address of an ACCEPTED council
// migration object to it, paying the objection bond; a jury is seated on the
// objection and its live sim jurors uphold it in the same call. An objection
// the jury rejects or cannot decide finalizes the migration, tombstoning a
// sim account the SDK modules' simulations still sign with, so the op only
// objects when the dry-run seats enough live jurors to carry the objection.
//
// Council migrations come from MsgForceMigration, which the HR body executes
// through an x/commons proposal: when there is nothing to object to, the op
// moves one forward instead (the new address accepts a REQUESTED council
// migration, or a forced-migration proposal advances one step).
func SimulateMsgObjectMigration(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgObjectMigration{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		council := func(status types.MigrationStatus) func(types.MemberMigration) bool {
			return func(m types.MemberMigration) bool {
				return m.Path == types.MigrationPath_MIGRATION_PATH_COUNCIL && m.Status == status
			}
		}
		accepted, err := e.migrations(council(types.MigrationStatus_MIGRATION_STATUS_ACCEPTED))
		if err != nil {
			return noOpErr(label, err)
		}
		reason := "no live old address can pay the objection bond"
		for _, m := range accepted {
			acc, ok := e.account(m.FromAddress)
			if !ok {
				continue
			}
			if e.usable(acc, e.params.MigrationObjectionBond) != "" {
				// Prerequisite: a member lends the objector the bond.
				if ok, err := e.fundAccount(acc, e.params.MigrationObjectionBond.MulRaw(2)); err != nil {
					return noOpErr(label, err)
				} else if !ok || e.usable(acc, e.params.MigrationObjectionBond) != "" {
					continue
				}
			}
			msg := &types.MsgObjectMigration{Member: m.FromAddress, MigrationId: m.Id, Reason: randomReason(e.r)}
			if why := e.objectionCarries(msg); why != "" {
				reason = why
				continue
			}
			opMsg, fops, err := e.deliver(acc, msg, e.params.MigrationObjectionBond)
			if err != nil || !opMsg.OK {
				return opMsg, fops, err
			}
			if err := e.upholdObjection(m.Id); err != nil {
				return opMsg, fops, err
			}
			return opMsg, fops, nil
		}
		if len(accepted) > 0 {
			return noOp(label, reason)
		}
		// Prerequisite: the new address accepts a REQUESTED council migration.
		acc, err := e.acceptCandidates(council(types.MigrationStatus_MIGRATION_STATUS_REQUESTED))
		if err != nil {
			return noOpErr(label, err)
		}
		if len(acc) > 0 {
			opMsg, fops, err := e.firstValid(&types.MsgAcceptMigration{}, acc, 4, "no council migration to accept")
			return relabelAs(opMsg, label, sdk.MsgTypeURL(&types.MsgAcceptMigration{}), "accepts"), fops, err
		}
		// Prerequisite: a forced (council) migration through the HR body.
		force := &types.MsgForceMigration{}
		bodies := e.groupsAllowing(sdk.MsgTypeURL(force))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgForceMigration")
		}
		return e.advanceInner(label, sdk.MsgTypeURL(force), bodies, decoder(func() *types.MsgForceMigration { return &types.MsgForceMigration{} }),
			e.buildForceMigration)
	})
}

// objectionCarries dry-runs an objection and reports why the jury it seats
// could not uphold it ("" when its live sim jurors can).
func (e *simEnv) objectionCarries(msg *types.MsgObjectMigration) string {
	cacheCtx, err := e.dryRunCtx(msg)
	if err != nil {
		return "handler would reject: " + err.Error()
	}
	m, err := e.k.MemberMigrations.Get(cacheCtx, msg.MigrationId)
	if err != nil {
		return "the objection opened no jury review"
	}
	rv, err := e.k.JuryReview.Get(cacheCtx, m.JuryReviewId)
	if err != nil || rv.RequiredVotes == 0 {
		return "no jury could be seated on the objection"
	}
	live := 0
	for _, j := range rv.Jurors {
		if acc, ok := e.account(j); ok && e.spendable(acc).IsPositive() {
			live++
		}
	}
	if commontypes.Saturate[uint32](live) < rv.RequiredVotes {
		return "too few live sim jurors would be seated to uphold the objection"
	}
	return ""
}

// upholdObjection has the jurors seated on the objection to migration id
// vote to uphold it until the review is tallied (the migration is then
// cancelled and the bond refunded).
func (e *simEnv) upholdObjection(id uint64) error {
	m, err := e.k.MemberMigrations.Get(e.ctx, id)
	if err != nil {
		return err
	}
	rv, err := e.k.JuryReview.Get(e.ctx, m.JuryReviewId)
	if err != nil {
		return err
	}
	_, err = e.voteJury(rv)
	return err
}

// buildForceMigration builds a MsgForceMigration signed by policy moving a
// live member (never a seat holder) to a fresh live sim account.
func (e *simEnv) buildForceMigration(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
	busy := map[string]bool{}
	for _, m := range taken {
		if t, ok := m.(*types.MsgForceMigration); ok {
			busy[t.Member], busy[t.NewAddress] = true, true
		}
	}
	srcs, err := e.migrationSources()
	if err != nil {
		return nil, "", err
	}
	for n, i := range e.r.Perm(len(srcs)) {
		if n >= 6 {
			break
		}
		ma := srcs[i]
		if busy[ma.addr()] || e.seated(ma.addr()) {
			continue
		}
		for _, to := range e.freshTargets(ma.addr(), types.MigrationPath_MIGRATION_PATH_COUNCIL) {
			if busy[to.Address.String()] {
				continue
			}
			msg := &types.MsgForceMigration{
				Authority: policy, Member: ma.addr(), NewAddress: to.Address.String(), EvidenceUri: randomURI(e.r),
			}
			if e.dryRun(msg) == nil {
				return msg, "", nil
			}
			break
		}
	}
	return nil, "no member can be force-migrated to a fresh account", nil
}

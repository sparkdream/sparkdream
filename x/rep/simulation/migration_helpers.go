package simulation

import (
	"bytes"

	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/types"
)

// Member key migration. Finalizing a migration tombstones the old address,
// and the SDK modules' own simulations keep signing with every sim account
// (a tombstone may sign almost nothing), so the simulated genesis stretches
// the migration delays far past any run (module/simulation.go): requests,
// acceptances, objections, cancellations and recovery reveals all run for
// real, and no migration finalizes.

// freshTargets returns the live non-member sim accounts that may become the
// new address of from on path (freshness and the other modules' vetoes).
func (e *simEnv) freshTargets(from string, path types.MigrationPath) []simtypes.Account {
	var out []simtypes.Account
	for _, acc := range e.nonMembers() {
		to := acc.Address.String()
		if to == from {
			continue
		}
		cacheCtx, _ := e.ctx.CacheContext()
		if e.k.CheckMigrationTarget(cacheCtx, from, to, path) == nil {
			out = append(out, acc)
		}
	}
	return out
}

// migrationSources returns the live, active members without a pending
// migration or an open report that may start one.
func (e *simEnv) migrationSources() ([]memberAcc, error) {
	return e.members(func(ma memberAcc) bool {
		if _, pending := e.k.GetPendingMigration(e.ctx, ma.addr()); pending {
			return false
		}
		return !e.k.HasOpenMemberReport(e.ctx, ma.addr())
	})
}

// recoverable reports whether member holds an active recovery commitment to
// the simulation's preimage for it (an onboarding commitment).
func (e *simEnv) recoverable(member string) bool {
	set, found := e.k.GetRecoveryCommitments(e.ctx, member)
	if !found {
		return false
	}
	want := types.RecoveryCommitmentHash(recoveryPreimage(member))
	idx := set.FindCommitment(want)
	return idx >= 0 && set.ActiveCommitment(set.Commitments[idx], e.ctx.BlockTime().Unix())
}

// sealFor reports whether sealed is the simulation's seal of member's
// recovery to newAddr.
func sealFor(sealed []byte, member, newAddr string) bool {
	return bytes.Equal(sealed, types.RecoverySeal(recoveryPreimage(member), member, newAddr))
}

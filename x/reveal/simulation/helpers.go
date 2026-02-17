package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// findContribution returns a random contribution with the given status, or nil if none exist.
func findContribution(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.ContributionStatus) (*types.Contribution, uint64, error) {
	var contributions []struct {
		id           uint64
		contribution types.Contribution
	}
	err := k.Contribution.Walk(ctx, nil, func(id uint64, contrib types.Contribution) (bool, error) {
		if contrib.Status == status {
			contributions = append(contributions, struct {
				id           uint64
				contribution types.Contribution
			}{id, contrib})
		}
		return false, nil
	})
	if err != nil || len(contributions) == 0 {
		return nil, 0, err
	}
	selected := contributions[r.Intn(len(contributions))]
	return &selected.contribution, selected.id, nil
}

// findContributionByContributor returns a random contribution owned by the given contributor.
func findContributionByContributor(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, contributor string, status types.ContributionStatus) (*types.Contribution, uint64, error) {
	var contributions []struct {
		id           uint64
		contribution types.Contribution
	}
	err := k.Contribution.Walk(ctx, nil, func(id uint64, contrib types.Contribution) (bool, error) {
		if contrib.Contributor == contributor && contrib.Status == status {
			contributions = append(contributions, struct {
				id           uint64
				contribution types.Contribution
			}{id, contrib})
		}
		return false, nil
	})
	if err != nil || len(contributions) == 0 {
		return nil, 0, err
	}
	selected := contributions[r.Intn(len(contributions))]
	return &selected.contribution, selected.id, nil
}

// findContributionWithTrancheStatus returns a random contribution that has a tranche in the given status.
func findContributionWithTrancheStatus(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, trancheStatus types.TrancheStatus) (*types.Contribution, uint64, uint32, error) {
	type match struct {
		id        uint64
		contrib   types.Contribution
		trancheID uint32
	}
	var matches []match
	err := k.Contribution.Walk(ctx, nil, func(id uint64, contrib types.Contribution) (bool, error) {
		for _, t := range contrib.Tranches {
			if t.Status == trancheStatus {
				matches = append(matches, match{id, contrib, t.Id})
			}
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, 0, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.contrib, selected.id, selected.trancheID, nil
}

// findRevealStake returns a random reveal stake, or nil if none exist.
func findRevealStake(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.RevealStake, uint64, error) {
	var stakes []struct {
		id    uint64
		stake types.RevealStake
	}
	err := k.RevealStake.Walk(ctx, nil, func(id uint64, stake types.RevealStake) (bool, error) {
		stakes = append(stakes, struct {
			id    uint64
			stake types.RevealStake
		}{id, stake})
		return false, nil
	})
	if err != nil || len(stakes) == 0 {
		return nil, 0, err
	}
	selected := stakes[r.Intn(len(stakes))]
	return &selected.stake, selected.id, nil
}

// findRevealStakeByStaker returns a random stake owned by the given staker.
func findRevealStakeByStaker(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, staker string) (*types.RevealStake, uint64, error) {
	var stakes []struct {
		id    uint64
		stake types.RevealStake
	}
	err := k.RevealStake.Walk(ctx, nil, func(id uint64, stake types.RevealStake) (bool, error) {
		if stake.Staker == staker {
			stakes = append(stakes, struct {
				id    uint64
				stake types.RevealStake
			}{id, stake})
		}
		return false, nil
	})
	if err != nil || len(stakes) == 0 {
		return nil, 0, err
	}
	selected := stakes[r.Intn(len(stakes))]
	return &selected.stake, selected.id, nil
}

// findWithdrawableStake returns a random stake whose tranche is in STAKING or BACKED status.
func findWithdrawableStake(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, staker string) (*types.RevealStake, uint64, error) {
	var stakes []struct {
		id    uint64
		stake types.RevealStake
	}
	err := k.RevealStake.Walk(ctx, nil, func(id uint64, stake types.RevealStake) (bool, error) {
		if stake.Staker != staker {
			return false, nil
		}
		contrib, err := k.Contribution.Get(ctx, stake.ContributionId)
		if err != nil {
			return false, nil
		}
		if int(stake.TrancheId) >= len(contrib.Tranches) {
			return false, nil
		}
		tranche := contrib.Tranches[stake.TrancheId]
		if tranche.Status == types.TrancheStatus_TRANCHE_STATUS_STAKING ||
			tranche.Status == types.TrancheStatus_TRANCHE_STATUS_BACKED {
			stakes = append(stakes, struct {
				id    uint64
				stake types.RevealStake
			}{id, stake})
		}
		return false, nil
	})
	if err != nil || len(stakes) == 0 {
		return nil, 0, err
	}
	selected := stakes[r.Intn(len(stakes))]
	return &selected.stake, selected.id, nil
}

// getOrCreateContribution creates or finds an existing contribution in PROPOSED status.
// It directly writes to the keeper stores, bypassing message validation that depends
// on external keepers (rep, commons) which are not wired during simulation.
func getOrCreateContribution(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, contributor string) (uint64, error) {
	// Try to find existing
	contrib, contribID, err := findContributionByContributor(r, ctx, k, contributor, types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED)
	if err == nil && contrib != nil {
		return contribID, nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	contribID, err = k.ContributionSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	numTranches := r.Intn(3) + 1 // 1-3 tranches
	totalValuation := math.NewInt(int64((r.Intn(40) + 10) * 1000)) // 10k-50k
	if totalValuation.GT(params.MaxTotalValuation) {
		totalValuation = params.MaxTotalValuation
	}

	tranches := makeTranches(r, numTranches, totalValuation)
	bondAmount := params.BondRate.MulInt(totalValuation).TruncateInt()

	newContrib := types.Contribution{
		Id:             contribID,
		Contributor:    contributor,
		ProjectName:    randomProjectName(r),
		Description:    "Simulation-generated contribution",
		Tranches:       tranches,
		CurrentTranche: 0,
		TotalValuation: totalValuation,
		BondAmount:     bondAmount,
		BondRemaining:  bondAmount,
		InitialLicense: randomLicense(r),
		FinalLicense:   "Apache-2.0",
		Status:         types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED,
		CreatedAt:      ctx.BlockHeight(),
		HoldbackAmount: math.ZeroInt(),
	}

	if err := k.Contribution.Set(ctx, contribID, newContrib); err != nil {
		return 0, err
	}
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(newContrib.Status), contribID)); err != nil {
		return 0, err
	}
	if err := k.ContributionsByContributor.Set(ctx, collections.Join(contributor, contribID)); err != nil {
		return 0, err
	}

	return contribID, nil
}

// getOrCreateApprovedContribution creates or finds an existing contribution in IN_PROGRESS status
// with its first tranche in STAKING status.
func getOrCreateApprovedContribution(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, contributor string) (uint64, error) {
	// Try to find existing IN_PROGRESS contribution
	contrib, contribID, err := findContribution(r, ctx, k, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS)
	if err == nil && contrib != nil {
		return contribID, nil
	}

	// Create a new one
	contribID, err = getOrCreateContribution(r, ctx, k, contributor)
	if err != nil {
		return 0, err
	}

	// Approve it (move to IN_PROGRESS)
	return approveContribution(ctx, k, contribID)
}

// approveContribution transitions a PROPOSED contribution to IN_PROGRESS.
func approveContribution(ctx sdk.Context, k keeper.Keeper, contribID uint64) (uint64, error) {
	contrib, err := k.Contribution.Get(ctx, contribID)
	if err != nil {
		return 0, err
	}

	if contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED {
		return contribID, nil // already approved
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	currentEpoch := ctx.BlockHeight()

	// Remove old status index
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
		return 0, err
	}

	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS
	contrib.ApprovedAt = currentEpoch

	// Set tranche 0 to STAKING with deadline
	if len(contrib.Tranches) > 0 {
		contrib.Tranches[0].Status = types.TrancheStatus_TRANCHE_STATUS_STAKING
		contrib.Tranches[0].StakeDeadline = currentEpoch + params.StakeDeadlineEpochs
	}

	if err := k.Contribution.Set(ctx, contribID, contrib); err != nil {
		return 0, err
	}
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
		return 0, err
	}

	return contribID, nil
}

// getOrCreateBackedContribution creates or finds a contribution with a BACKED tranche
// (stake threshold met, ready for reveal).
func getOrCreateBackedContribution(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, contributor string, staker string) (uint64, uint32, error) {
	// Try to find existing
	contrib, contribID, trancheID, err := findContributionWithTrancheStatus(r, ctx, k, types.TrancheStatus_TRANCHE_STATUS_BACKED)
	if err == nil && contrib != nil {
		return contribID, trancheID, nil
	}

	// Create an approved contribution
	contribID, err = getOrCreateApprovedContribution(r, ctx, k, contributor)
	if err != nil {
		return 0, 0, err
	}

	// Stake enough to reach threshold on tranche 0
	c, err := k.Contribution.Get(ctx, contribID)
	if err != nil {
		return 0, 0, err
	}

	if len(c.Tranches) == 0 {
		return 0, 0, fmt.Errorf("no tranches")
	}

	tranche := &c.Tranches[0]
	if tranche.Status == types.TrancheStatus_TRANCHE_STATUS_BACKED {
		return contribID, 0, nil
	}

	// Create a stake that fills the threshold
	remaining := tranche.StakeThreshold.Sub(tranche.DreamStaked)
	if remaining.IsPositive() {
		stakeID, err := k.StakeSeq.Next(ctx)
		if err != nil {
			return 0, 0, err
		}

		stake := types.RevealStake{
			Id:             stakeID,
			Staker:         staker,
			ContributionId: contribID,
			TrancheId:      0,
			Amount:         remaining,
			StakedAt:       ctx.BlockHeight(),
		}

		if err := k.RevealStake.Set(ctx, stakeID, stake); err != nil {
			return 0, 0, err
		}
		trancheKey := keeper.TrancheKey(contribID, 0)
		if err := k.StakesByTranche.Set(ctx, collections.Join(trancheKey, stakeID)); err != nil {
			return 0, 0, err
		}
		if err := k.StakesByStaker.Set(ctx, collections.Join(staker, stakeID)); err != nil {
			return 0, 0, err
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return 0, 0, err
		}

		tranche.DreamStaked = tranche.StakeThreshold
		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_BACKED
		tranche.BackedAt = ctx.BlockHeight()
		tranche.RevealDeadline = ctx.BlockHeight() + params.RevealDeadlineEpochs

		if err := k.Contribution.Set(ctx, contribID, c); err != nil {
			return 0, 0, err
		}
	}

	return contribID, 0, nil
}

// getOrCreateRevealedContribution creates or finds a contribution with a REVEALED tranche.
func getOrCreateRevealedContribution(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, contributor string, staker string) (uint64, uint32, error) {
	// Try to find existing
	contrib, contribID, trancheID, err := findContributionWithTrancheStatus(r, ctx, k, types.TrancheStatus_TRANCHE_STATUS_REVEALED)
	if err == nil && contrib != nil {
		return contribID, trancheID, nil
	}

	// Create a backed contribution and reveal it
	contribID, trancheID, err = getOrCreateBackedContribution(r, ctx, k, contributor, staker)
	if err != nil {
		return 0, 0, err
	}

	c, err := k.Contribution.Get(ctx, contribID)
	if err != nil {
		return 0, 0, err
	}

	if int(trancheID) >= len(c.Tranches) {
		return 0, 0, fmt.Errorf("tranche %d out of range", trancheID)
	}
	tranche := &c.Tranches[trancheID]

	if tranche.Status == types.TrancheStatus_TRANCHE_STATUS_REVEALED {
		return contribID, trancheID, nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, 0, err
	}

	tranche.CodeUri = randomURI(r, "code")
	tranche.DocsUri = randomURI(r, "docs")
	tranche.CommitHash = randomCommitHash(r)
	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_REVEALED
	tranche.RevealedAt = ctx.BlockHeight()
	tranche.VerificationDeadline = ctx.BlockHeight() + params.VerificationPeriodEpochs

	if err := k.Contribution.Set(ctx, contribID, c); err != nil {
		return 0, 0, err
	}

	return contribID, trancheID, nil
}

// getOrCreateDisputedContribution creates or finds a contribution with a DISPUTED tranche.
func getOrCreateDisputedContribution(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, contributor string, staker string) (uint64, uint32, error) {
	// Try to find existing
	contrib, contribID, trancheID, err := findContributionWithTrancheStatus(r, ctx, k, types.TrancheStatus_TRANCHE_STATUS_DISPUTED)
	if err == nil && contrib != nil {
		return contribID, trancheID, nil
	}

	// Create a revealed contribution and mark it disputed
	contribID, trancheID, err = getOrCreateRevealedContribution(r, ctx, k, contributor, staker)
	if err != nil {
		return 0, 0, err
	}

	c, err := k.Contribution.Get(ctx, contribID)
	if err != nil {
		return 0, 0, err
	}

	if int(trancheID) >= len(c.Tranches) {
		return 0, 0, fmt.Errorf("tranche %d out of range", trancheID)
	}

	c.Tranches[trancheID].Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED

	if err := k.Contribution.Set(ctx, contribID, c); err != nil {
		return 0, 0, err
	}

	return contribID, trancheID, nil
}

// createRevealStakeForTranche creates a stake on a specific tranche, writing directly to store.
func createRevealStakeForTranche(ctx sdk.Context, k keeper.Keeper, staker string, contribID uint64, trancheID uint32, amount math.Int) (uint64, error) {
	stakeID, err := k.StakeSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	stake := types.RevealStake{
		Id:             stakeID,
		Staker:         staker,
		ContributionId: contribID,
		TrancheId:      trancheID,
		Amount:         amount,
		StakedAt:       ctx.BlockHeight(),
	}

	if err := k.RevealStake.Set(ctx, stakeID, stake); err != nil {
		return 0, err
	}
	trancheKey := keeper.TrancheKey(contribID, trancheID)
	if err := k.StakesByTranche.Set(ctx, collections.Join(trancheKey, stakeID)); err != nil {
		return 0, err
	}
	if err := k.StakesByStaker.Set(ctx, collections.Join(staker, stakeID)); err != nil {
		return 0, err
	}

	return stakeID, nil
}

// makeTranches builds tranches whose stake thresholds sum to totalValuation.
func makeTranches(r *rand.Rand, numTranches int, totalValuation math.Int) []types.RevealTranche {
	tranches := make([]types.RevealTranche, numTranches)
	remaining := totalValuation

	for i := 0; i < numTranches; i++ {
		var threshold math.Int
		if i == numTranches-1 {
			threshold = remaining
		} else {
			// Distribute roughly evenly with some randomness
			base := remaining.QuoRaw(int64(numTranches - i))
			jitter := base.QuoRaw(4) // +/- 25%
			if jitter.IsPositive() && jitter.Int64() > 0 {
				threshold = base.SubRaw(jitter.Int64()).AddRaw(int64(r.Intn(int(jitter.Int64() * 2))))
			} else {
				threshold = base
			}
			if !threshold.IsPositive() {
				threshold = math.OneInt()
			}
			if threshold.GT(remaining) {
				threshold = remaining
			}
		}

		tranches[i] = types.RevealTranche{
			Id:             uint32(i),
			Name:           randomTrancheName(r),
			Description:    fmt.Sprintf("Tranche %d of simulation contribution", i),
			Components:     randomComponents(r),
			StakeThreshold: threshold,
			DreamStaked:    math.ZeroInt(),
			PreviewUri:     randomURI(r, "preview"),
			Status:         types.TrancheStatus_TRANCHE_STATUS_LOCKED,
		}

		remaining = remaining.Sub(threshold)
	}

	return tranches
}

// randomProjectName generates a random project name.
func randomProjectName(r *rand.Rand) string {
	names := []string{
		"aurora-engine", "zenith-core", "phoenix-sdk", "nebula-chain",
		"prism-net", "vortex-io", "cascade-db", "ember-node",
		"atlas-bridge", "horizon-relay", "pulse-api", "forge-tools",
	}
	return names[r.Intn(len(names))]
}

// randomTrancheName generates a random tranche name.
func randomTrancheName(r *rand.Rand) string {
	names := []string{
		"core-module", "api-layer", "storage-engine", "auth-service",
		"indexer", "cli-tools", "documentation", "test-suite",
		"migration-scripts", "monitoring-hooks", "plugin-system", "sdk-bindings",
	}
	return names[r.Intn(len(names))]
}

// randomLicense returns a random open-source license.
func randomLicense(r *rand.Rand) string {
	licenses := []string{"MIT", "Apache-2.0", "GPL-3.0", "BSD-3-Clause", "MPL-2.0"}
	return licenses[r.Intn(len(licenses))]
}

// randomComponents returns a random list of component names.
func randomComponents(r *rand.Rand) []string {
	pool := []string{
		"keeper", "types", "cli", "genesis", "module", "simulation",
		"ante", "middleware", "query", "msg_server",
	}
	n := r.Intn(3) + 1
	components := make([]string, n)
	for i := 0; i < n; i++ {
		components[i] = pool[r.Intn(len(pool))]
	}
	return components
}

// randomURI generates a random URI.
func randomURI(r *rand.Rand, prefix string) string {
	return fmt.Sprintf("ipfs://Qm%s/%s", simtypes.RandStringOfLength(r, 40), prefix)
}

// randomCommitHash generates a random git commit hash.
func randomCommitHash(r *rand.Rand) string {
	const hex = "0123456789abcdef"
	hash := make([]byte, 40)
	for i := range hash {
		hash[i] = hex[r.Intn(len(hex))]
	}
	return string(hash)
}

// randomDisputeVerdict returns a random non-unspecified dispute verdict.
func randomDisputeVerdict(r *rand.Rand) types.DisputeVerdict {
	verdicts := []types.DisputeVerdict{
		types.DisputeVerdict_DISPUTE_VERDICT_ACCEPT,
		types.DisputeVerdict_DISPUTE_VERDICT_IMPROVE,
		types.DisputeVerdict_DISPUTE_VERDICT_REJECT,
	}
	return verdicts[r.Intn(len(verdicts))]
}

// getAccountForAddress finds a sim account that matches the given address string.
func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// pickDifferentAccount returns a random sim account that is NOT the given address.
func pickDifferentAccount(r *rand.Rand, accs []simtypes.Account, exclude string) (simtypes.Account, bool) {
	filtered := make([]simtypes.Account, 0, len(accs))
	for _, acc := range accs {
		if acc.Address.String() != exclude {
			filtered = append(filtered, acc)
		}
	}
	if len(filtered) == 0 {
		return simtypes.Account{}, false
	}
	return filtered[r.Intn(len(filtered))], true
}

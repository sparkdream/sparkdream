package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func SimulateMsgVerify(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgVerify{}

		// Find or create a contribution with a REVEALED tranche
		simAccount, _ := simtypes.RandomAcc(r, accs)
		stakerAcc, found := pickDifferentAccount(r, accs, simAccount.Address.String())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no different account for staker"), nil, nil
		}

		contribID, trancheID, err := getOrCreateRevealedContribution(r, ctx, k, simAccount.Address.String(), stakerAcc.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get/create revealed contribution: "+err.Error()), nil, nil
		}

		contrib, err := k.Contribution.Get(ctx, contribID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get contribution"), nil, nil
		}

		// Voter must be different from contributor (self-vote prevention)
		voterAcc, found := pickDifferentAccount(r, accs, contrib.Contributor)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no available voter account"), nil, nil
		}

		// Voter must have a stake on this tranche to vote.
		// Ensure the staker (who backed the tranche) is the voter, or create a new stake.
		voterAddr := voterAcc.Address.String()

		// Check if voter already has a stake on this tranche
		hasStake := false
		stakeWeight := math.ZeroInt()
		trancheKey := keeper.TrancheKey(contribID, trancheID)
		_ = k.StakesByTranche.Walk(ctx,
			collections.NewPrefixedPairRange[string, uint64](trancheKey),
			func(key collections.Pair[string, uint64]) (bool, error) {
				s, err := k.RevealStake.Get(ctx, key.K2())
				if err != nil {
					return false, nil
				}
				if s.Staker == voterAddr {
					hasStake = true
					stakeWeight = stakeWeight.Add(s.Amount)
				}
				return false, nil
			},
		)

		if !hasStake {
			// Create a small stake for the voter on this tranche
			amount := math.NewInt(int64(r.Intn(500) + 100))
			_, err := createRevealStakeForTranche(ctx, k, voterAddr, contribID, trancheID, amount)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create voter stake: "+err.Error()), nil, nil
			}
			stakeWeight = amount
		}

		// Check if voter already voted
		vk := keeper.VoteKey(contribID, trancheID, voterAddr)
		hasVoted, err := k.Vote.Has(ctx, vk)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to check vote"), nil, nil
		}
		if hasVoted {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "voter already voted"), nil, nil
		}

		// Create the verification vote directly
		valueConfirmed := r.Intn(10) > 2 // 70% chance of confirming value
		qualityRating := uint32(r.Intn(5) + 1) // 1-5

		vote := types.VerificationVote{
			Voter:          voterAddr,
			ContributionId: contribID,
			TrancheId:      trancheID,
			ValueConfirmed: valueConfirmed,
			QualityRating:  qualityRating,
			Comments:       fmt.Sprintf("Simulation vote: quality=%d", qualityRating),
			StakeWeight:    stakeWeight,
			VotedAt:        ctx.BlockHeight(),
		}

		if err := k.Vote.Set(ctx, vk, vote); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save vote"), nil, nil
		}
		if err := k.VotesByTranche.Set(ctx, collections.Join(trancheKey, vk)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save tranche vote index"), nil, nil
		}
		if err := k.VotesByVoter.Set(ctx, collections.Join(voterAddr, vk)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save voter vote index"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

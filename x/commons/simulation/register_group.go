package simulation

import (
	"fmt"
	"math/rand"
	"strconv"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func SimulateMsgRegisterGroup(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	cdc codec.Codec,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Pick a random account to be the "Parent Council" (Authority)
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. STATE INJECTION (The Hack)
		// The msg_server requires the signer to be a registered ExtendedGroup Policy.
		// We inject a fake ExtendedGroup record where the PolicyAddress is our simAccount.
		// Use hyphens (valid) instead of underscores (invalid)
		fakeParentName := "sim-parent-" + simtypes.RandStringOfLength(r, 5)
		fakeParent := types.ExtendedGroup{
			GroupId:       uint64(simtypes.RandIntBetween(r, 1, 1000)),
			PolicyAddress: simAccount.Address.String(),
		}

		if err := k.ExtendedGroup.Set(ctx, fakeParentName, fakeParent); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterGroup{}), "failed to setup fake parent"), nil, err
		}

		// --- NEW: Update Index for O(1) Checks ---
		// The updated handler checks PolicyToName.Has(signer). We must inject this index entry.
		if err := k.PolicyToName.Set(ctx, simAccount.Address.String(), fakeParentName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterGroup{}), "failed to setup fake parent index"), nil, err
		}

		// --- NEW: Fee Bypass ---
		// Disable the ProposalFee for simulation to avoid "insufficient funds" errors
		// (Simulation accounts usually have 'stake', not 'uspark')
		if err := k.Params.Set(ctx, types.NewParams("")); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterGroup{}), "failed to bypass fee"), nil, err
		}

		// 3. Generate Random Members
		numMembers := simtypes.RandIntBetween(r, 1, 5)
		if len(accs) < numMembers {
			numMembers = len(accs)
		}

		var members []string
		var weights []string

		perm := r.Perm(len(accs))
		for i := 0; i < numMembers; i++ {
			acc := accs[perm[i]]
			members = append(members, acc.Address.String())
			weights = append(weights, "1")
		}

		// 4. Generate Random Policy
		var policyType, threshold string
		if r.Intn(2) == 0 {
			// Threshold Policy
			policyType = keeper.PolicyTypeThreshold
			tVal := simtypes.RandIntBetween(r, 1, numMembers+1)
			threshold = strconv.Itoa(tVal)
		} else {
			// Percentage Policy
			policyType = keeper.PolicyTypePercentage
			pVal := 0.01 + r.Float64()*(0.99)
			threshold = fmt.Sprintf("%.2f", pVal)
		}

		msg := &types.MsgRegisterGroup{
			Authority:          simAccount.Address.String(),
			Name:               "sim-group-" + simtypes.RandStringOfLength(r, 5),
			Description:        "Simulated Group " + simtypes.RandStringOfLength(r, 10),
			Members:            members,
			MemberWeights:      weights,
			MinMembers:         uint64(numMembers),
			MaxMembers:         uint64(numMembers + 2),
			TermDuration:       int64(simtypes.RandIntBetween(r, 86400, 86400*30)),
			VoteThreshold:      threshold,
			PolicyType:         policyType,
			VotingPeriod:       int64(simtypes.RandIntBetween(r, 3600, 86400)),
			MinExecutionPeriod: 0,
			UpdateCooldown:     int64(simtypes.RandIntBetween(r, 0, 3600)),
			FundingWeight:      uint64(simtypes.RandIntBetween(r, 0, 100)),
			MaxSpendPerEpoch:   "",
		}

		if r.Intn(2) == 0 {
			msg.MaxSpendPerEpoch = "1000stake"
		}

		// 5. Construct OperationInput (Updated for v0.53.4)
		// Ensure cdc is asserted to *codec.ProtoCodec
		var protoCdc *codec.ProtoCodec
		if cdc != nil {
			if pc, ok := cdc.(*codec.ProtoCodec); ok {
				protoCdc = pc
			}
		}

		opInput := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             protoCdc,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opInput)
	}
}

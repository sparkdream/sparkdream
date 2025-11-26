package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/ecosystem/keeper"
	"sparkdream/x/ecosystem/types"
)

// SimulateMsgSpend simulates a governance proposal to spend ecosystem funds.
func SimulateMsgSpend(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	gk *govkeeper.Keeper,
	k keeper.Keeper,
	cdc codec.Codec,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Get the Authority (Gov Module Account)
		govAddr := ak.GetModuleAddress(govtypes.ModuleName)

		// Sanity check: Ensure the keeper believes Gov is the authority
		// If k.GetAuthority() is strict, we might need to verify they match,
		// but typically we just construct the msg with the correct authority (Gov).

		// 2. Check Ecosystem Module Balance
		// We need funds in the module to actually spend.
		ecoModuleAddr := ak.GetModuleAddress(types.ModuleName)
		spendable := bk.SpendableCoins(ctx, ecoModuleAddr)
		if spendable.Empty() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpend{}), "ecosystem module has no funds"), nil, nil
		}

		// 3. Construct the Inner Message (MsgSpend)
		recipient, _ := simtypes.RandomAcc(r, accs)

		// Pick a random amount from the available balance
		amount := simtypes.RandSubsetCoins(r, spendable)
		if amount.Empty() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSpend{}), "generated empty spend amount"), nil, nil
		}

		msgSpend := &types.MsgSpend{
			Authority: govAddr.String(),
			Recipient: recipient.Address.String(),
			Amount:    amount,
		}

		// 4. Wrap it in a Governance Proposal (MsgSubmitProposal v1)
		// We need to marshal the message into an Any
		anyMsg, err := cdctypes.NewAnyWithValue(msgSpend)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msgSpend), "failed to wrap msg spend"), nil, err
		}

		// 5. Find a Proposer with enough funds for Deposit
		// We need the MinDeposit params to ensure the proposal is valid
		govParams, err := gk.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msgSpend), "failed to get gov params"), nil, err
		}
		minDeposit := govParams.MinDeposit

		var proposer simtypes.Account
		var found bool

		// Find an account that can pay the deposit
		for _, acc := range accs {
			balance := bk.SpendableCoins(ctx, acc.Address)
			if balance.IsAllGTE(minDeposit) {
				proposer = acc
				found = true
				break
			}
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&govv1.MsgSubmitProposal{}), "no account with enough funds for deposit"), nil, nil
		}

		// 6. Create the Proposal Message
		proposalMsg := &govv1.MsgSubmitProposal{
			Messages:       []*cdctypes.Any{anyMsg},
			InitialDeposit: minDeposit,
			Proposer:       proposer.Address.String(),
			Metadata:       "Community Spend Simulation",
			Title:          "Simulated Ecosystem Spend",
			Summary:        "Proposal to spend ecosystem funds via simulation",
		}

		// 7. Execute Transaction (Submit Proposal)
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             proposalMsg,
			CoinsSpentInMsg: minDeposit,
			Context:         ctx,
			SimAccount:      proposer,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName, // Attributed to ecosystem module for tracking
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}

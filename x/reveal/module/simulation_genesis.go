package reveal

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/reveal/types"
)

// simGenesisParams returns the simulated genesis params. Every reveal
// deadline counts blocks, and the production defaults run to months, so a
// simulation of a few hundred blocks would never see a stake, reveal,
// verification or dispute deadline fire, nor a tranche go DISPUTED (the only
// way to reach MsgResolveDispute). The simulation shortens them; amounts
// and rates keep their defaults.
func simGenesisParams() types.Params {
	p := types.DefaultParams()
	p.StakeDeadlineEpochs = 100
	p.RevealDeadlineEpochs = 30
	p.VerificationPeriodEpochs = 10
	p.DisputeResolutionEpochs = 120
	p.ProposalCooldownEpochs = 30
	return p
}

// simPayableProposalFee makes the simulated x/commons proposal fee payable.
//
// MsgApprove, MsgReject and MsgResolveDispute reach their handlers only as
// executed Commons Council proposals, and x/commons' ante handler requires
// the tx fee of a MsgSubmitProposal to cover params.proposal_fee. The
// x/commons default fee is denominated in a fixed "uspark" while the
// simulated chain's bond denom comes from x/identity (e.g.
// "uspark.sparkdream"), so no sim account could ever pay it. When the
// denoms differ, the same amount is re-denominated in the bond denom. The
// x/commons simulated genesis is generated before this module's (modules
// generate in name order); when it already uses the bond denom this is a
// no-op.
func simPayableProposalFee(simState *module.SimulationState) {
	bz, ok := simState.GenState[commonstypes.ModuleName]
	if !ok {
		return
	}
	var gs commonstypes.GenesisState
	if err := simState.Cdc.UnmarshalJSON(bz, &gs); err != nil {
		panic(err)
	}
	fee, err := sdk.ParseCoinsNormalized(gs.Params.ProposalFee)
	if err != nil || fee.IsZero() || fee.AmountOf(sdk.DefaultBondDenom).IsPositive() {
		return
	}
	gs.Params.ProposalFee = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, fee[0].Amount)).String()
	simState.GenState[commonstypes.ModuleName] = simState.Cdc.MustMarshalJSON(&gs)
}

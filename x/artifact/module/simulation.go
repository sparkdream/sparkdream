package artifact

import (
	"math/rand"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	artifactsimulation "sparkdream/x/artifact/simulation"
	"sparkdream/x/artifact/types"
)

// GenerateGenesisState creates a randomized GenState of the module. Class
// creation is member-gated and the simulator's accounts are rarely x/rep
// members, so genesis seeds one class of every shape owned by a random sim
// account: the mint, market, edition, Harberger and permanent-token
// operations then have something to act on from block 1.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	artifactGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	if len(simState.Accounts) > 0 {
		artifactGenesis.Classes = simGenesisClasses(simState)
		artifactGenesis.ClassSeq = uint64(len(artifactGenesis.Classes))
		// The EDITIONS class starts with one empty edition.
		artifactGenesis.Editions = []types.Edition{{
			ClassId: 2, Id: 1, Creator: artifactGenesis.Classes[1].Owner, Metadata: types.TokenMetadata{Name: "aurora potion"},
			Status: types.ContentStatus_CONTENT_STATUS_ACTIVE, RewardPerUnit: math.ZeroInt(), RewardsFunded: math.ZeroInt(),
		}}
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&artifactGenesis)
}

// simGenesisClasses builds the seeded classes (ids 1..5): unique
// transferable, editions, soulbound revocable, Harberger, permanent.
func simGenesisClasses(simState *module.SimulationState) []types.Class {
	zero := sdk.NewInt64Coin(sdk.DefaultBondDenom, 0)
	base := func(id uint64, name string, flags types.ClassFlags) types.Class {
		owner := simState.Accounts[simState.Rand.Intn(len(simState.Accounts))].Address.String()
		return types.Class{
			Id: id, Owner: owner, Creator: owner, Name: name, Symbol: "SIM", Flags: flags, NextTokenId: 1,
			MintPolicy:        types.MintPolicy{Price: zero, PriceStep: math.ZeroInt(), FloorPrice: math.ZeroInt()},
			PayoutAddress:     owner,
			RoyaltyRecipients: types.SoleRoyaltyRecipient(owner),
			Status:            types.ContentStatus_CONTENT_STATUS_ACTIVE,
		}
	}
	unique := base(1, "Phoenix Studies", types.ClassFlags{Transferable: true, TokenMetadataMutable: true})
	unique.RoyaltyBps = 300
	unique.MintPolicy.PublicMintEnabled = true
	unique.MintPolicy.Price = sdk.NewInt64Coin(sdk.DefaultBondDenom, 500_000)
	editions := base(2, "Aurora Potions", types.ClassFlags{Transferable: true})
	editions.TokenModel = types.TokenModel_TOKEN_MODEL_EDITIONS
	editions.NextTokenId, editions.Supply = 2, 1
	badges := base(3, "Zenith Badges", types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER})
	plots := base(4, "Aurora Plots", types.ClassFlags{Transferable: true})
	plots.Harberger = types.HarbergerPolicy{TaxBpsPerYear: 500, TaxPeriod: types.MinHarbergerPeriod, ForeclosureDuration: types.MinHarbergerPeriod * 4}
	plots.MintPolicy.PublicMintEnabled = true
	plots.MintPolicy.Price = sdk.NewInt64Coin(sdk.DefaultBondDenom, 1_000_000)
	wall := base(5, "Phoenix Wall", types.ClassFlags{Transferable: true, BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_NEITHER})
	return []types.Class{unique, editions, badges, plots, wall}
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// simOp pairs an operation weight key with its default weight and factory.
type simOp struct {
	key    string
	weight int
	op     func(am AppModule, simState module.SimulationState) simtypes.Operation
}

var simOps = []simOp{
	{"op_weight_msg_artifact_create_class", 20, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgCreateClass(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_mint", 60, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgMint(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_public_mint", 30, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgPublicMint(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_transfer", 50, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgTransfer(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_accept_incoming", 30, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgAcceptIncoming(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_reject_incoming", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRejectIncoming(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_set_receive_policy", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgSetReceivePolicy(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_burn", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgBurn(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_list", 30, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgList(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_delist", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgDelist(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_buy", 25, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgBuy(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_issue", 20, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgIssue(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_renew", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRenew(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_redeem", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRedeem(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_lock_royalty", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgLockRoyalty(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_reveal", 20, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgReveal(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_reveal_seed", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRevealSeed(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_propose_media", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgProposeMedia(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_accept_media", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgAcceptMedia(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_reject_media", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRejectMedia(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_set_displayed_media", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgSetDisplayedMedia(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_create_edition", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgCreateEdition(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_mint_units", 30, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgMintUnits(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_transfer_units", 25, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgTransferUnits(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_burn_units", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgBurnUnits(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_revoke_units", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRevokeUnits(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_list_units", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgListUnits(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_delist_units", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgDelistUnits(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_buy_units", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgBuyUnits(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_fund_edition", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgFundEdition(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_claim_edition_rewards", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgClaimEditionRewards(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_add_craft_recipe", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgAddCraftRecipe(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_remove_craft_recipe", 2, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRemoveCraftRecipe(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_craft", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgCraft(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_offer_rental", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgOfferRental(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_cancel_rental_offer", 3, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgCancelRentalOffer(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_accept_rental", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgAcceptRental(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_end_rental", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgEndRental(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_nest", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgNest(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_detach", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgDetach(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_set_harberger_price", 3, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgSetHarbergerPrice(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_force_buy", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgForceBuy(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_update_unit_listing", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgUpdateUnitListing(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_make_offer", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgMakeOffer(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_cancel_offer", 5, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgCancelOffer(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_accept_offer", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgAcceptOffer(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_start_auction", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgStartAuction(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_bid", 20, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgBid(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_settle_auction", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgSettleAuction(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_cancel_auction", 3, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgCancelAuction(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
}

// WeightedOperations returns the module operations with their respective
// weights. Class-management, moderation and params messages are exercised
// by unit and e2e tests instead (they need council or sentinel actors the
// simulator does not create).
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0, len(simOps))
	for _, o := range simOps {
		weight := o.weight
		simState.AppParams.GetOrGenerate(o.key, &weight, nil, func(_ *rand.Rand) { weight = o.weight })
		operations = append(operations, simulation.NewWeightedOperation(weight, o.op(am, simState)))
	}

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

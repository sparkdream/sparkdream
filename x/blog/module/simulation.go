package blog

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	blogsimulation "sparkdream/x/blog/simulation"
	"sparkdream/x/blog/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	blogGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&blogGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)

	const (
		opWeightMsgCreatePost     = "op_weight_msg_blog_create_post"
		opWeightMsgUpdatePost     = "op_weight_msg_blog_update_post"
		opWeightMsgDeletePost     = "op_weight_msg_blog_delete_post"
		opWeightMsgHidePost       = "op_weight_msg_blog_hide_post"
		opWeightMsgUnhidePost     = "op_weight_msg_blog_unhide_post"
		opWeightMsgCreateReply    = "op_weight_msg_blog_create_reply"
		opWeightMsgUpdateReply    = "op_weight_msg_blog_update_reply"
		opWeightMsgDeleteReply    = "op_weight_msg_blog_delete_reply"
		opWeightMsgHideReply      = "op_weight_msg_blog_hide_reply"
		opWeightMsgUnhideReply    = "op_weight_msg_blog_unhide_reply"
		opWeightMsgReact          = "op_weight_msg_blog_react"
		opWeightMsgRemoveReaction = "op_weight_msg_blog_remove_reaction"
		opWeightMsgPinPost        = "op_weight_msg_blog_pin_post"
		opWeightMsgPinReply       = "op_weight_msg_blog_pin_reply"

		defaultWeightMsgCreatePost     int = 100
		defaultWeightMsgUpdatePost     int = 100
		defaultWeightMsgDeletePost     int = 100
		defaultWeightMsgHidePost       int = 30
		defaultWeightMsgUnhidePost     int = 20
		defaultWeightMsgCreateReply    int = 80
		defaultWeightMsgUpdateReply    int = 50
		defaultWeightMsgDeleteReply    int = 50
		defaultWeightMsgHideReply      int = 30
		defaultWeightMsgUnhideReply    int = 20
		defaultWeightMsgReact          int = 80
		defaultWeightMsgRemoveReaction int = 30
		defaultWeightMsgPinPost        int = 15
		defaultWeightMsgPinReply       int = 15
	)

	var weightMsgCreatePost int
	simState.AppParams.GetOrGenerate(opWeightMsgCreatePost, &weightMsgCreatePost, nil,
		func(_ *rand.Rand) { weightMsgCreatePost = defaultWeightMsgCreatePost },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreatePost,
		blogsimulation.SimulateMsgCreatePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgUpdatePost int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdatePost, &weightMsgUpdatePost, nil,
		func(_ *rand.Rand) { weightMsgUpdatePost = defaultWeightMsgUpdatePost },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdatePost,
		blogsimulation.SimulateMsgUpdatePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgDeletePost int
	simState.AppParams.GetOrGenerate(opWeightMsgDeletePost, &weightMsgDeletePost, nil,
		func(_ *rand.Rand) { weightMsgDeletePost = defaultWeightMsgDeletePost },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeletePost,
		blogsimulation.SimulateMsgDeletePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgHidePost int
	simState.AppParams.GetOrGenerate(opWeightMsgHidePost, &weightMsgHidePost, nil,
		func(_ *rand.Rand) { weightMsgHidePost = defaultWeightMsgHidePost },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgHidePost,
		blogsimulation.SimulateMsgHidePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgUnhidePost int
	simState.AppParams.GetOrGenerate(opWeightMsgUnhidePost, &weightMsgUnhidePost, nil,
		func(_ *rand.Rand) { weightMsgUnhidePost = defaultWeightMsgUnhidePost },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnhidePost,
		blogsimulation.SimulateMsgUnhidePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgCreateReply int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateReply, &weightMsgCreateReply, nil,
		func(_ *rand.Rand) { weightMsgCreateReply = defaultWeightMsgCreateReply },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateReply,
		blogsimulation.SimulateMsgCreateReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgUpdateReply int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateReply, &weightMsgUpdateReply, nil,
		func(_ *rand.Rand) { weightMsgUpdateReply = defaultWeightMsgUpdateReply },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateReply,
		blogsimulation.SimulateMsgUpdateReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgDeleteReply int
	simState.AppParams.GetOrGenerate(opWeightMsgDeleteReply, &weightMsgDeleteReply, nil,
		func(_ *rand.Rand) { weightMsgDeleteReply = defaultWeightMsgDeleteReply },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeleteReply,
		blogsimulation.SimulateMsgDeleteReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgHideReply int
	simState.AppParams.GetOrGenerate(opWeightMsgHideReply, &weightMsgHideReply, nil,
		func(_ *rand.Rand) { weightMsgHideReply = defaultWeightMsgHideReply },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgHideReply,
		blogsimulation.SimulateMsgHideReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgUnhideReply int
	simState.AppParams.GetOrGenerate(opWeightMsgUnhideReply, &weightMsgUnhideReply, nil,
		func(_ *rand.Rand) { weightMsgUnhideReply = defaultWeightMsgUnhideReply },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnhideReply,
		blogsimulation.SimulateMsgUnhideReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgReact int
	simState.AppParams.GetOrGenerate(opWeightMsgReact, &weightMsgReact, nil,
		func(_ *rand.Rand) { weightMsgReact = defaultWeightMsgReact },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReact,
		blogsimulation.SimulateMsgReact(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgRemoveReaction int
	simState.AppParams.GetOrGenerate(opWeightMsgRemoveReaction, &weightMsgRemoveReaction, nil,
		func(_ *rand.Rand) { weightMsgRemoveReaction = defaultWeightMsgRemoveReaction },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemoveReaction,
		blogsimulation.SimulateMsgRemoveReaction(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgPinPost int
	simState.AppParams.GetOrGenerate(opWeightMsgPinPost, &weightMsgPinPost, nil,
		func(_ *rand.Rand) { weightMsgPinPost = defaultWeightMsgPinPost },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPinPost,
		blogsimulation.SimulateMsgPinPost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgPinReply int
	simState.AppParams.GetOrGenerate(opWeightMsgPinReply, &weightMsgPinReply, nil,
		func(_ *rand.Rand) { weightMsgPinReply = defaultWeightMsgPinReply },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPinReply,
		blogsimulation.SimulateMsgPinReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

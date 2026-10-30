package blog

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/blog/keeper"
	blogsimulation "sparkdream/x/blog/simulation"
	"sparkdream/x/blog/types"
)

// GenerateGenesisState creates a randomized GenState of the module: the
// default genesis (post and reply ids start at 1), with a media scan fee
// half the time so the simulated media writes exercise its burn.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	blogGenesis := types.DefaultGenesis()
	if simState.Rand.Intn(2) == 0 {
		blogGenesis.Params.MediaScanFee = math.NewInt(1_000)
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(blogGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the module's simulation operations with their
// respective weights. Every operation delivers a real transaction.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	type op struct {
		key    string
		weight int
		build  func(types.AuthKeeper, types.BankKeeper, keeper.Keeper, client.TxConfig) simtypes.Operation
	}
	ops := []op{
		{"op_weight_msg_blog_create_post", 100, blogsimulation.SimulateMsgCreatePost},
		{"op_weight_msg_blog_update_post", 60, blogsimulation.SimulateMsgUpdatePost},
		{"op_weight_msg_blog_delete_post", 20, blogsimulation.SimulateMsgDeletePost},
		{"op_weight_msg_blog_hide_post", 30, blogsimulation.SimulateMsgHidePost},
		{"op_weight_msg_blog_unhide_post", 20, blogsimulation.SimulateMsgUnhidePost},
		{"op_weight_msg_blog_create_reply", 80, blogsimulation.SimulateMsgCreateReply},
		{"op_weight_msg_blog_update_reply", 50, blogsimulation.SimulateMsgUpdateReply},
		{"op_weight_msg_blog_delete_reply", 30, blogsimulation.SimulateMsgDeleteReply},
		{"op_weight_msg_blog_hide_reply", 30, blogsimulation.SimulateMsgHideReply},
		{"op_weight_msg_blog_unhide_reply", 20, blogsimulation.SimulateMsgUnhideReply},
		{"op_weight_msg_blog_react", 80, blogsimulation.SimulateMsgReact},
		{"op_weight_msg_blog_remove_reaction", 30, blogsimulation.SimulateMsgRemoveReaction},
		{"op_weight_msg_blog_pin_post", 15, blogsimulation.SimulateMsgPinPost},
		{"op_weight_msg_blog_unpin_post", 10, blogsimulation.SimulateMsgUnpinPost},
		{"op_weight_msg_blog_pin_reply", 15, blogsimulation.SimulateMsgPinReply},
		{"op_weight_msg_blog_unpin_reply", 10, blogsimulation.SimulateMsgUnpinReply},
		{"op_weight_msg_blog_make_post_permanent", 15, blogsimulation.SimulateMsgMakePostPermanent},
		{"op_weight_msg_blog_make_reply_permanent", 15, blogsimulation.SimulateMsgMakeReplyPermanent},
		{"op_weight_msg_blog_update_operational_params", 10, blogsimulation.SimulateMsgUpdateOperationalParams},
	}
	operations := make([]simtypes.WeightedOperation, 0, len(ops))
	for _, o := range ops {
		weight := o.weight
		simState.AppParams.GetOrGenerate(o.key, &weight, nil, func(_ *rand.Rand) { weight = o.weight })
		operations = append(operations, simulation.NewWeightedOperation(
			weight,
			o.build(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
		))
	}
	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

package forum

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	forumsimulation "sparkdream/x/forum/simulation"
	"sparkdream/x/forum/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
// Tag seeding is the responsibility of x/rep, which owns the tag registry.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}

	forumGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&forumGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgCreatePost          = "op_weight_msg_forum"
		defaultWeightMsgCreatePost int = 100
	)

	var weightMsgCreatePost int
	simState.AppParams.GetOrGenerate(opWeightMsgCreatePost, &weightMsgCreatePost, nil,
		func(_ *rand.Rand) {
			weightMsgCreatePost = defaultWeightMsgCreatePost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreatePost,
		forumsimulation.SimulateMsgCreatePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgEditPost          = "op_weight_msg_forum"
		defaultWeightMsgEditPost int = 100
	)

	var weightMsgEditPost int
	simState.AppParams.GetOrGenerate(opWeightMsgEditPost, &weightMsgEditPost, nil,
		func(_ *rand.Rand) {
			weightMsgEditPost = defaultWeightMsgEditPost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgEditPost,
		forumsimulation.SimulateMsgEditPost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDeletePost          = "op_weight_msg_forum"
		defaultWeightMsgDeletePost int = 100
	)

	var weightMsgDeletePost int
	simState.AppParams.GetOrGenerate(opWeightMsgDeletePost, &weightMsgDeletePost, nil,
		func(_ *rand.Rand) {
			weightMsgDeletePost = defaultWeightMsgDeletePost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeletePost,
		forumsimulation.SimulateMsgDeletePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFreezeThread          = "op_weight_msg_forum"
		defaultWeightMsgFreezeThread int = 100
	)

	var weightMsgFreezeThread int
	simState.AppParams.GetOrGenerate(opWeightMsgFreezeThread, &weightMsgFreezeThread, nil,
		func(_ *rand.Rand) {
			weightMsgFreezeThread = defaultWeightMsgFreezeThread
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFreezeThread,
		forumsimulation.SimulateMsgFreezeThread(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnarchiveThread          = "op_weight_msg_forum"
		defaultWeightMsgUnarchiveThread int = 100
	)

	var weightMsgUnarchiveThread int
	simState.AppParams.GetOrGenerate(opWeightMsgUnarchiveThread, &weightMsgUnarchiveThread, nil,
		func(_ *rand.Rand) {
			weightMsgUnarchiveThread = defaultWeightMsgUnarchiveThread
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnarchiveThread,
		forumsimulation.SimulateMsgUnarchiveThread(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgPinPost          = "op_weight_msg_forum"
		defaultWeightMsgPinPost int = 100
	)

	var weightMsgPinPost int
	simState.AppParams.GetOrGenerate(opWeightMsgPinPost, &weightMsgPinPost, nil,
		func(_ *rand.Rand) {
			weightMsgPinPost = defaultWeightMsgPinPost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPinPost,
		forumsimulation.SimulateMsgPinPost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnpinPost          = "op_weight_msg_forum"
		defaultWeightMsgUnpinPost int = 100
	)

	var weightMsgUnpinPost int
	simState.AppParams.GetOrGenerate(opWeightMsgUnpinPost, &weightMsgUnpinPost, nil,
		func(_ *rand.Rand) {
			weightMsgUnpinPost = defaultWeightMsgUnpinPost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnpinPost,
		forumsimulation.SimulateMsgUnpinPost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgLockThread          = "op_weight_msg_forum"
		defaultWeightMsgLockThread int = 100
	)

	var weightMsgLockThread int
	simState.AppParams.GetOrGenerate(opWeightMsgLockThread, &weightMsgLockThread, nil,
		func(_ *rand.Rand) {
			weightMsgLockThread = defaultWeightMsgLockThread
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgLockThread,
		forumsimulation.SimulateMsgLockThread(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnlockThread          = "op_weight_msg_forum"
		defaultWeightMsgUnlockThread int = 100
	)

	var weightMsgUnlockThread int
	simState.AppParams.GetOrGenerate(opWeightMsgUnlockThread, &weightMsgUnlockThread, nil,
		func(_ *rand.Rand) {
			weightMsgUnlockThread = defaultWeightMsgUnlockThread
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnlockThread,
		forumsimulation.SimulateMsgUnlockThread(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgMoveThread          = "op_weight_msg_forum"
		defaultWeightMsgMoveThread int = 100
	)

	var weightMsgMoveThread int
	simState.AppParams.GetOrGenerate(opWeightMsgMoveThread, &weightMsgMoveThread, nil,
		func(_ *rand.Rand) {
			weightMsgMoveThread = defaultWeightMsgMoveThread
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgMoveThread,
		forumsimulation.SimulateMsgMoveThread(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFollowThread          = "op_weight_msg_forum"
		defaultWeightMsgFollowThread int = 100
	)

	var weightMsgFollowThread int
	simState.AppParams.GetOrGenerate(opWeightMsgFollowThread, &weightMsgFollowThread, nil,
		func(_ *rand.Rand) {
			weightMsgFollowThread = defaultWeightMsgFollowThread
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFollowThread,
		forumsimulation.SimulateMsgFollowThread(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnfollowThread          = "op_weight_msg_forum"
		defaultWeightMsgUnfollowThread int = 100
	)

	var weightMsgUnfollowThread int
	simState.AppParams.GetOrGenerate(opWeightMsgUnfollowThread, &weightMsgUnfollowThread, nil,
		func(_ *rand.Rand) {
			weightMsgUnfollowThread = defaultWeightMsgUnfollowThread
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnfollowThread,
		forumsimulation.SimulateMsgUnfollowThread(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpvotePost          = "op_weight_msg_forum"
		defaultWeightMsgUpvotePost int = 100
	)

	var weightMsgUpvotePost int
	simState.AppParams.GetOrGenerate(opWeightMsgUpvotePost, &weightMsgUpvotePost, nil,
		func(_ *rand.Rand) {
			weightMsgUpvotePost = defaultWeightMsgUpvotePost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpvotePost,
		forumsimulation.SimulateMsgUpvotePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDownvotePost          = "op_weight_msg_forum"
		defaultWeightMsgDownvotePost int = 100
	)

	var weightMsgDownvotePost int
	simState.AppParams.GetOrGenerate(opWeightMsgDownvotePost, &weightMsgDownvotePost, nil,
		func(_ *rand.Rand) {
			weightMsgDownvotePost = defaultWeightMsgDownvotePost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDownvotePost,
		forumsimulation.SimulateMsgDownvotePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFlagPost          = "op_weight_msg_forum"
		defaultWeightMsgFlagPost int = 100
	)

	var weightMsgFlagPost int
	simState.AppParams.GetOrGenerate(opWeightMsgFlagPost, &weightMsgFlagPost, nil,
		func(_ *rand.Rand) {
			weightMsgFlagPost = defaultWeightMsgFlagPost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFlagPost,
		forumsimulation.SimulateMsgFlagPost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDismissFlags          = "op_weight_msg_forum"
		defaultWeightMsgDismissFlags int = 100
	)

	var weightMsgDismissFlags int
	simState.AppParams.GetOrGenerate(opWeightMsgDismissFlags, &weightMsgDismissFlags, nil,
		func(_ *rand.Rand) {
			weightMsgDismissFlags = defaultWeightMsgDismissFlags
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDismissFlags,
		forumsimulation.SimulateMsgDismissFlags(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgHidePost          = "op_weight_msg_forum"
		defaultWeightMsgHidePost int = 100
	)

	var weightMsgHidePost int
	simState.AppParams.GetOrGenerate(opWeightMsgHidePost, &weightMsgHidePost, nil,
		func(_ *rand.Rand) {
			weightMsgHidePost = defaultWeightMsgHidePost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgHidePost,
		forumsimulation.SimulateMsgHidePost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAppealPost          = "op_weight_msg_forum"
		defaultWeightMsgAppealPost int = 100
	)

	var weightMsgAppealPost int
	simState.AppParams.GetOrGenerate(opWeightMsgAppealPost, &weightMsgAppealPost, nil,
		func(_ *rand.Rand) {
			weightMsgAppealPost = defaultWeightMsgAppealPost
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAppealPost,
		forumsimulation.SimulateMsgAppealPost(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAppealThreadLock          = "op_weight_msg_forum"
		defaultWeightMsgAppealThreadLock int = 100
	)

	var weightMsgAppealThreadLock int
	simState.AppParams.GetOrGenerate(opWeightMsgAppealThreadLock, &weightMsgAppealThreadLock, nil,
		func(_ *rand.Rand) {
			weightMsgAppealThreadLock = defaultWeightMsgAppealThreadLock
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAppealThreadLock,
		forumsimulation.SimulateMsgAppealThreadLock(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAppealThreadMove          = "op_weight_msg_forum"
		defaultWeightMsgAppealThreadMove int = 100
	)

	var weightMsgAppealThreadMove int
	simState.AppParams.GetOrGenerate(opWeightMsgAppealThreadMove, &weightMsgAppealThreadMove, nil,
		func(_ *rand.Rand) {
			weightMsgAppealThreadMove = defaultWeightMsgAppealThreadMove
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAppealThreadMove,
		forumsimulation.SimulateMsgAppealThreadMove(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateBounty          = "op_weight_msg_forum"
		defaultWeightMsgCreateBounty int = 100
	)

	var weightMsgCreateBounty int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateBounty, &weightMsgCreateBounty, nil,
		func(_ *rand.Rand) {
			weightMsgCreateBounty = defaultWeightMsgCreateBounty
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateBounty,
		forumsimulation.SimulateMsgCreateBounty(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAwardBounty          = "op_weight_msg_forum"
		defaultWeightMsgAwardBounty int = 100
	)

	var weightMsgAwardBounty int
	simState.AppParams.GetOrGenerate(opWeightMsgAwardBounty, &weightMsgAwardBounty, nil,
		func(_ *rand.Rand) {
			weightMsgAwardBounty = defaultWeightMsgAwardBounty
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAwardBounty,
		forumsimulation.SimulateMsgAwardBounty(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgIncreaseBounty          = "op_weight_msg_forum"
		defaultWeightMsgIncreaseBounty int = 100
	)

	var weightMsgIncreaseBounty int
	simState.AppParams.GetOrGenerate(opWeightMsgIncreaseBounty, &weightMsgIncreaseBounty, nil,
		func(_ *rand.Rand) {
			weightMsgIncreaseBounty = defaultWeightMsgIncreaseBounty
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgIncreaseBounty,
		forumsimulation.SimulateMsgIncreaseBounty(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelBounty          = "op_weight_msg_forum"
		defaultWeightMsgCancelBounty int = 100
	)

	var weightMsgCancelBounty int
	simState.AppParams.GetOrGenerate(opWeightMsgCancelBounty, &weightMsgCancelBounty, nil,
		func(_ *rand.Rand) {
			weightMsgCancelBounty = defaultWeightMsgCancelBounty
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelBounty,
		forumsimulation.SimulateMsgCancelBounty(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAssignBountyToReply          = "op_weight_msg_forum"
		defaultWeightMsgAssignBountyToReply int = 100
	)

	var weightMsgAssignBountyToReply int
	simState.AppParams.GetOrGenerate(opWeightMsgAssignBountyToReply, &weightMsgAssignBountyToReply, nil,
		func(_ *rand.Rand) {
			weightMsgAssignBountyToReply = defaultWeightMsgAssignBountyToReply
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAssignBountyToReply,
		forumsimulation.SimulateMsgAssignBountyToReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgPinReply          = "op_weight_msg_forum"
		defaultWeightMsgPinReply int = 100
	)

	var weightMsgPinReply int
	simState.AppParams.GetOrGenerate(opWeightMsgPinReply, &weightMsgPinReply, nil,
		func(_ *rand.Rand) {
			weightMsgPinReply = defaultWeightMsgPinReply
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPinReply,
		forumsimulation.SimulateMsgPinReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnpinReply          = "op_weight_msg_forum"
		defaultWeightMsgUnpinReply int = 100
	)

	var weightMsgUnpinReply int
	simState.AppParams.GetOrGenerate(opWeightMsgUnpinReply, &weightMsgUnpinReply, nil,
		func(_ *rand.Rand) {
			weightMsgUnpinReply = defaultWeightMsgUnpinReply
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnpinReply,
		forumsimulation.SimulateMsgUnpinReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDisputePin          = "op_weight_msg_forum"
		defaultWeightMsgDisputePin int = 100
	)

	var weightMsgDisputePin int
	simState.AppParams.GetOrGenerate(opWeightMsgDisputePin, &weightMsgDisputePin, nil,
		func(_ *rand.Rand) {
			weightMsgDisputePin = defaultWeightMsgDisputePin
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDisputePin,
		forumsimulation.SimulateMsgDisputePin(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgMarkAcceptedReply          = "op_weight_msg_forum"
		defaultWeightMsgMarkAcceptedReply int = 100
	)

	var weightMsgMarkAcceptedReply int
	simState.AppParams.GetOrGenerate(opWeightMsgMarkAcceptedReply, &weightMsgMarkAcceptedReply, nil,
		func(_ *rand.Rand) {
			weightMsgMarkAcceptedReply = defaultWeightMsgMarkAcceptedReply
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgMarkAcceptedReply,
		forumsimulation.SimulateMsgMarkAcceptedReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgConfirmProposedReply          = "op_weight_msg_forum"
		defaultWeightMsgConfirmProposedReply int = 100
	)

	var weightMsgConfirmProposedReply int
	simState.AppParams.GetOrGenerate(opWeightMsgConfirmProposedReply, &weightMsgConfirmProposedReply, nil,
		func(_ *rand.Rand) {
			weightMsgConfirmProposedReply = defaultWeightMsgConfirmProposedReply
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgConfirmProposedReply,
		forumsimulation.SimulateMsgConfirmProposedReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRejectProposedReply          = "op_weight_msg_forum"
		defaultWeightMsgRejectProposedReply int = 100
	)

	var weightMsgRejectProposedReply int
	simState.AppParams.GetOrGenerate(opWeightMsgRejectProposedReply, &weightMsgRejectProposedReply, nil,
		func(_ *rand.Rand) {
			weightMsgRejectProposedReply = defaultWeightMsgRejectProposedReply
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRejectProposedReply,
		forumsimulation.SimulateMsgRejectProposedReply(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetForumPaused          = "op_weight_msg_forum"
		defaultWeightMsgSetForumPaused int = 100
	)

	var weightMsgSetForumPaused int
	simState.AppParams.GetOrGenerate(opWeightMsgSetForumPaused, &weightMsgSetForumPaused, nil,
		func(_ *rand.Rand) {
			weightMsgSetForumPaused = defaultWeightMsgSetForumPaused
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetForumPaused,
		forumsimulation.SimulateMsgSetForumPaused(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetModerationPaused          = "op_weight_msg_forum"
		defaultWeightMsgSetModerationPaused int = 100
	)

	var weightMsgSetModerationPaused int
	simState.AppParams.GetOrGenerate(opWeightMsgSetModerationPaused, &weightMsgSetModerationPaused, nil,
		func(_ *rand.Rand) {
			weightMsgSetModerationPaused = defaultWeightMsgSetModerationPaused
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetModerationPaused,
		forumsimulation.SimulateMsgSetModerationPaused(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

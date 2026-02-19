package collect

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	collectsimulation "sparkdream/x/collect/simulation"
	"sparkdream/x/collect/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	collectGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&collectGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgCreateCollection          = "op_weight_msg_collect"
		defaultWeightMsgCreateCollection int = 100
	)

	var weightMsgCreateCollection int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateCollection, &weightMsgCreateCollection, nil,
		func(_ *rand.Rand) {
			weightMsgCreateCollection = defaultWeightMsgCreateCollection
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateCollection,
		collectsimulation.SimulateMsgCreateCollection(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateCollection          = "op_weight_msg_collect"
		defaultWeightMsgUpdateCollection int = 100
	)

	var weightMsgUpdateCollection int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateCollection, &weightMsgUpdateCollection, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateCollection = defaultWeightMsgUpdateCollection
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateCollection,
		collectsimulation.SimulateMsgUpdateCollection(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDeleteCollection          = "op_weight_msg_collect"
		defaultWeightMsgDeleteCollection int = 100
	)

	var weightMsgDeleteCollection int
	simState.AppParams.GetOrGenerate(opWeightMsgDeleteCollection, &weightMsgDeleteCollection, nil,
		func(_ *rand.Rand) {
			weightMsgDeleteCollection = defaultWeightMsgDeleteCollection
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeleteCollection,
		collectsimulation.SimulateMsgDeleteCollection(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAddItem          = "op_weight_msg_collect"
		defaultWeightMsgAddItem int = 100
	)

	var weightMsgAddItem int
	simState.AppParams.GetOrGenerate(opWeightMsgAddItem, &weightMsgAddItem, nil,
		func(_ *rand.Rand) {
			weightMsgAddItem = defaultWeightMsgAddItem
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAddItem,
		collectsimulation.SimulateMsgAddItem(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAddItems          = "op_weight_msg_collect"
		defaultWeightMsgAddItems int = 100
	)

	var weightMsgAddItems int
	simState.AppParams.GetOrGenerate(opWeightMsgAddItems, &weightMsgAddItems, nil,
		func(_ *rand.Rand) {
			weightMsgAddItems = defaultWeightMsgAddItems
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAddItems,
		collectsimulation.SimulateMsgAddItems(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateItem          = "op_weight_msg_collect"
		defaultWeightMsgUpdateItem int = 100
	)

	var weightMsgUpdateItem int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateItem, &weightMsgUpdateItem, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateItem = defaultWeightMsgUpdateItem
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateItem,
		collectsimulation.SimulateMsgUpdateItem(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRemoveItem          = "op_weight_msg_collect"
		defaultWeightMsgRemoveItem int = 100
	)

	var weightMsgRemoveItem int
	simState.AppParams.GetOrGenerate(opWeightMsgRemoveItem, &weightMsgRemoveItem, nil,
		func(_ *rand.Rand) {
			weightMsgRemoveItem = defaultWeightMsgRemoveItem
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemoveItem,
		collectsimulation.SimulateMsgRemoveItem(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRemoveItems          = "op_weight_msg_collect"
		defaultWeightMsgRemoveItems int = 100
	)

	var weightMsgRemoveItems int
	simState.AppParams.GetOrGenerate(opWeightMsgRemoveItems, &weightMsgRemoveItems, nil,
		func(_ *rand.Rand) {
			weightMsgRemoveItems = defaultWeightMsgRemoveItems
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemoveItems,
		collectsimulation.SimulateMsgRemoveItems(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgReorderItem          = "op_weight_msg_collect"
		defaultWeightMsgReorderItem int = 100
	)

	var weightMsgReorderItem int
	simState.AppParams.GetOrGenerate(opWeightMsgReorderItem, &weightMsgReorderItem, nil,
		func(_ *rand.Rand) {
			weightMsgReorderItem = defaultWeightMsgReorderItem
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReorderItem,
		collectsimulation.SimulateMsgReorderItem(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAddCollaborator          = "op_weight_msg_collect"
		defaultWeightMsgAddCollaborator int = 100
	)

	var weightMsgAddCollaborator int
	simState.AppParams.GetOrGenerate(opWeightMsgAddCollaborator, &weightMsgAddCollaborator, nil,
		func(_ *rand.Rand) {
			weightMsgAddCollaborator = defaultWeightMsgAddCollaborator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAddCollaborator,
		collectsimulation.SimulateMsgAddCollaborator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRemoveCollaborator          = "op_weight_msg_collect"
		defaultWeightMsgRemoveCollaborator int = 100
	)

	var weightMsgRemoveCollaborator int
	simState.AppParams.GetOrGenerate(opWeightMsgRemoveCollaborator, &weightMsgRemoveCollaborator, nil,
		func(_ *rand.Rand) {
			weightMsgRemoveCollaborator = defaultWeightMsgRemoveCollaborator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemoveCollaborator,
		collectsimulation.SimulateMsgRemoveCollaborator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateCollaboratorRole          = "op_weight_msg_collect"
		defaultWeightMsgUpdateCollaboratorRole int = 100
	)

	var weightMsgUpdateCollaboratorRole int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateCollaboratorRole, &weightMsgUpdateCollaboratorRole, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateCollaboratorRole = defaultWeightMsgUpdateCollaboratorRole
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateCollaboratorRole,
		collectsimulation.SimulateMsgUpdateCollaboratorRole(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRegisterCurator          = "op_weight_msg_collect"
		defaultWeightMsgRegisterCurator int = 100
	)

	var weightMsgRegisterCurator int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterCurator, &weightMsgRegisterCurator, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterCurator = defaultWeightMsgRegisterCurator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterCurator,
		collectsimulation.SimulateMsgRegisterCurator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnregisterCurator          = "op_weight_msg_collect"
		defaultWeightMsgUnregisterCurator int = 100
	)

	var weightMsgUnregisterCurator int
	simState.AppParams.GetOrGenerate(opWeightMsgUnregisterCurator, &weightMsgUnregisterCurator, nil,
		func(_ *rand.Rand) {
			weightMsgUnregisterCurator = defaultWeightMsgUnregisterCurator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnregisterCurator,
		collectsimulation.SimulateMsgUnregisterCurator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRateCollection          = "op_weight_msg_collect"
		defaultWeightMsgRateCollection int = 100
	)

	var weightMsgRateCollection int
	simState.AppParams.GetOrGenerate(opWeightMsgRateCollection, &weightMsgRateCollection, nil,
		func(_ *rand.Rand) {
			weightMsgRateCollection = defaultWeightMsgRateCollection
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRateCollection,
		collectsimulation.SimulateMsgRateCollection(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgChallengeReview          = "op_weight_msg_collect"
		defaultWeightMsgChallengeReview int = 100
	)

	var weightMsgChallengeReview int
	simState.AppParams.GetOrGenerate(opWeightMsgChallengeReview, &weightMsgChallengeReview, nil,
		func(_ *rand.Rand) {
			weightMsgChallengeReview = defaultWeightMsgChallengeReview
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgChallengeReview,
		collectsimulation.SimulateMsgChallengeReview(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRequestSponsorship          = "op_weight_msg_collect"
		defaultWeightMsgRequestSponsorship int = 100
	)

	var weightMsgRequestSponsorship int
	simState.AppParams.GetOrGenerate(opWeightMsgRequestSponsorship, &weightMsgRequestSponsorship, nil,
		func(_ *rand.Rand) {
			weightMsgRequestSponsorship = defaultWeightMsgRequestSponsorship
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRequestSponsorship,
		collectsimulation.SimulateMsgRequestSponsorship(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelSponsorshipRequest          = "op_weight_msg_collect"
		defaultWeightMsgCancelSponsorshipRequest int = 100
	)

	var weightMsgCancelSponsorshipRequest int
	simState.AppParams.GetOrGenerate(opWeightMsgCancelSponsorshipRequest, &weightMsgCancelSponsorshipRequest, nil,
		func(_ *rand.Rand) {
			weightMsgCancelSponsorshipRequest = defaultWeightMsgCancelSponsorshipRequest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelSponsorshipRequest,
		collectsimulation.SimulateMsgCancelSponsorshipRequest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSponsorCollection          = "op_weight_msg_collect"
		defaultWeightMsgSponsorCollection int = 100
	)

	var weightMsgSponsorCollection int
	simState.AppParams.GetOrGenerate(opWeightMsgSponsorCollection, &weightMsgSponsorCollection, nil,
		func(_ *rand.Rand) {
			weightMsgSponsorCollection = defaultWeightMsgSponsorCollection
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSponsorCollection,
		collectsimulation.SimulateMsgSponsorCollection(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpvoteContent          = "op_weight_msg_collect"
		defaultWeightMsgUpvoteContent int = 100
	)

	var weightMsgUpvoteContent int
	simState.AppParams.GetOrGenerate(opWeightMsgUpvoteContent, &weightMsgUpvoteContent, nil,
		func(_ *rand.Rand) {
			weightMsgUpvoteContent = defaultWeightMsgUpvoteContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpvoteContent,
		collectsimulation.SimulateMsgUpvoteContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDownvoteContent          = "op_weight_msg_collect"
		defaultWeightMsgDownvoteContent int = 100
	)

	var weightMsgDownvoteContent int
	simState.AppParams.GetOrGenerate(opWeightMsgDownvoteContent, &weightMsgDownvoteContent, nil,
		func(_ *rand.Rand) {
			weightMsgDownvoteContent = defaultWeightMsgDownvoteContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDownvoteContent,
		collectsimulation.SimulateMsgDownvoteContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFlagContent          = "op_weight_msg_collect"
		defaultWeightMsgFlagContent int = 100
	)

	var weightMsgFlagContent int
	simState.AppParams.GetOrGenerate(opWeightMsgFlagContent, &weightMsgFlagContent, nil,
		func(_ *rand.Rand) {
			weightMsgFlagContent = defaultWeightMsgFlagContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFlagContent,
		collectsimulation.SimulateMsgFlagContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgHideContent          = "op_weight_msg_collect"
		defaultWeightMsgHideContent int = 100
	)

	var weightMsgHideContent int
	simState.AppParams.GetOrGenerate(opWeightMsgHideContent, &weightMsgHideContent, nil,
		func(_ *rand.Rand) {
			weightMsgHideContent = defaultWeightMsgHideContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgHideContent,
		collectsimulation.SimulateMsgHideContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAppealHide          = "op_weight_msg_collect"
		defaultWeightMsgAppealHide int = 100
	)

	var weightMsgAppealHide int
	simState.AppParams.GetOrGenerate(opWeightMsgAppealHide, &weightMsgAppealHide, nil,
		func(_ *rand.Rand) {
			weightMsgAppealHide = defaultWeightMsgAppealHide
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAppealHide,
		collectsimulation.SimulateMsgAppealHide(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgEndorseCollection          = "op_weight_msg_collect"
		defaultWeightMsgEndorseCollection int = 100
	)

	var weightMsgEndorseCollection int
	simState.AppParams.GetOrGenerate(opWeightMsgEndorseCollection, &weightMsgEndorseCollection, nil,
		func(_ *rand.Rand) {
			weightMsgEndorseCollection = defaultWeightMsgEndorseCollection
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgEndorseCollection,
		collectsimulation.SimulateMsgEndorseCollection(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetSeekingEndorsement          = "op_weight_msg_collect"
		defaultWeightMsgSetSeekingEndorsement int = 100
	)

	var weightMsgSetSeekingEndorsement int
	simState.AppParams.GetOrGenerate(opWeightMsgSetSeekingEndorsement, &weightMsgSetSeekingEndorsement, nil,
		func(_ *rand.Rand) {
			weightMsgSetSeekingEndorsement = defaultWeightMsgSetSeekingEndorsement
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetSeekingEndorsement,
		collectsimulation.SimulateMsgSetSeekingEndorsement(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

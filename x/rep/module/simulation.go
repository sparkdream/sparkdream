package rep

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	repsimulation "sparkdream/x/rep/simulation"
	"sparkdream/x/rep/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	repGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&repGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgInviteMember          = "op_weight_msg_rep"
		defaultWeightMsgInviteMember int = 100
	)

	var weightMsgInviteMember int
	simState.AppParams.GetOrGenerate(opWeightMsgInviteMember, &weightMsgInviteMember, nil,
		func(_ *rand.Rand) {
			weightMsgInviteMember = defaultWeightMsgInviteMember
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgInviteMember,
		repsimulation.SimulateMsgInviteMember(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAcceptInvitation          = "op_weight_msg_rep"
		defaultWeightMsgAcceptInvitation int = 100
	)

	var weightMsgAcceptInvitation int
	simState.AppParams.GetOrGenerate(opWeightMsgAcceptInvitation, &weightMsgAcceptInvitation, nil,
		func(_ *rand.Rand) {
			weightMsgAcceptInvitation = defaultWeightMsgAcceptInvitation
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAcceptInvitation,
		repsimulation.SimulateMsgAcceptInvitation(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgTransferDream          = "op_weight_msg_rep"
		defaultWeightMsgTransferDream int = 100
	)

	var weightMsgTransferDream int
	simState.AppParams.GetOrGenerate(opWeightMsgTransferDream, &weightMsgTransferDream, nil,
		func(_ *rand.Rand) {
			weightMsgTransferDream = defaultWeightMsgTransferDream
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTransferDream,
		repsimulation.SimulateMsgTransferDream(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgProposeProject          = "op_weight_msg_rep"
		defaultWeightMsgProposeProject int = 100
	)

	var weightMsgProposeProject int
	simState.AppParams.GetOrGenerate(opWeightMsgProposeProject, &weightMsgProposeProject, nil,
		func(_ *rand.Rand) {
			weightMsgProposeProject = defaultWeightMsgProposeProject
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgProposeProject,
		repsimulation.SimulateMsgProposeProject(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveProjectBudget          = "op_weight_msg_rep"
		defaultWeightMsgApproveProjectBudget int = 100
	)

	var weightMsgApproveProjectBudget int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveProjectBudget, &weightMsgApproveProjectBudget, nil,
		func(_ *rand.Rand) {
			weightMsgApproveProjectBudget = defaultWeightMsgApproveProjectBudget
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveProjectBudget,
		repsimulation.SimulateMsgApproveProjectBudget(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelProject          = "op_weight_msg_rep"
		defaultWeightMsgCancelProject int = 100
	)

	var weightMsgCancelProject int
	simState.AppParams.GetOrGenerate(opWeightMsgCancelProject, &weightMsgCancelProject, nil,
		func(_ *rand.Rand) {
			weightMsgCancelProject = defaultWeightMsgCancelProject
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelProject,
		repsimulation.SimulateMsgCancelProject(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateInitiative          = "op_weight_msg_rep"
		defaultWeightMsgCreateInitiative int = 100
	)

	var weightMsgCreateInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateInitiative, &weightMsgCreateInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgCreateInitiative = defaultWeightMsgCreateInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateInitiative,
		repsimulation.SimulateMsgCreateInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAssignInitiative          = "op_weight_msg_rep"
		defaultWeightMsgAssignInitiative int = 100
	)

	var weightMsgAssignInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgAssignInitiative, &weightMsgAssignInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgAssignInitiative = defaultWeightMsgAssignInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAssignInitiative,
		repsimulation.SimulateMsgAssignInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitInitiativeWork          = "op_weight_msg_rep"
		defaultWeightMsgSubmitInitiativeWork int = 100
	)

	var weightMsgSubmitInitiativeWork int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitInitiativeWork, &weightMsgSubmitInitiativeWork, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitInitiativeWork = defaultWeightMsgSubmitInitiativeWork
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitInitiativeWork,
		repsimulation.SimulateMsgSubmitInitiativeWork(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveInitiative          = "op_weight_msg_rep"
		defaultWeightMsgApproveInitiative int = 100
	)

	var weightMsgApproveInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveInitiative, &weightMsgApproveInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgApproveInitiative = defaultWeightMsgApproveInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveInitiative,
		repsimulation.SimulateMsgApproveInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAbandonInitiative          = "op_weight_msg_rep"
		defaultWeightMsgAbandonInitiative int = 100
	)

	var weightMsgAbandonInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgAbandonInitiative, &weightMsgAbandonInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgAbandonInitiative = defaultWeightMsgAbandonInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAbandonInitiative,
		repsimulation.SimulateMsgAbandonInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCompleteInitiative          = "op_weight_msg_rep"
		defaultWeightMsgCompleteInitiative int = 100
	)

	var weightMsgCompleteInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgCompleteInitiative, &weightMsgCompleteInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgCompleteInitiative = defaultWeightMsgCompleteInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCompleteInitiative,
		repsimulation.SimulateMsgCompleteInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgStake          = "op_weight_msg_rep"
		defaultWeightMsgStake int = 100
	)

	var weightMsgStake int
	simState.AppParams.GetOrGenerate(opWeightMsgStake, &weightMsgStake, nil,
		func(_ *rand.Rand) {
			weightMsgStake = defaultWeightMsgStake
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgStake,
		repsimulation.SimulateMsgStake(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnstake          = "op_weight_msg_rep"
		defaultWeightMsgUnstake int = 100
	)

	var weightMsgUnstake int
	simState.AppParams.GetOrGenerate(opWeightMsgUnstake, &weightMsgUnstake, nil,
		func(_ *rand.Rand) {
			weightMsgUnstake = defaultWeightMsgUnstake
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnstake,
		repsimulation.SimulateMsgUnstake(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateChallenge          = "op_weight_msg_rep"
		defaultWeightMsgCreateChallenge int = 100
	)

	var weightMsgCreateChallenge int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateChallenge, &weightMsgCreateChallenge, nil,
		func(_ *rand.Rand) {
			weightMsgCreateChallenge = defaultWeightMsgCreateChallenge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateChallenge,
		repsimulation.SimulateMsgCreateChallenge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRespondToChallenge          = "op_weight_msg_rep"
		defaultWeightMsgRespondToChallenge int = 100
	)

	var weightMsgRespondToChallenge int
	simState.AppParams.GetOrGenerate(opWeightMsgRespondToChallenge, &weightMsgRespondToChallenge, nil,
		func(_ *rand.Rand) {
			weightMsgRespondToChallenge = defaultWeightMsgRespondToChallenge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRespondToChallenge,
		repsimulation.SimulateMsgRespondToChallenge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitJurorVote          = "op_weight_msg_rep"
		defaultWeightMsgSubmitJurorVote int = 100
	)

	var weightMsgSubmitJurorVote int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitJurorVote, &weightMsgSubmitJurorVote, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitJurorVote = defaultWeightMsgSubmitJurorVote
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitJurorVote,
		repsimulation.SimulateMsgSubmitJurorVote(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitExpertTestimony          = "op_weight_msg_rep"
		defaultWeightMsgSubmitExpertTestimony int = 100
	)

	var weightMsgSubmitExpertTestimony int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitExpertTestimony, &weightMsgSubmitExpertTestimony, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitExpertTestimony = defaultWeightMsgSubmitExpertTestimony
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitExpertTestimony,
		repsimulation.SimulateMsgSubmitExpertTestimony(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAssignInterim          = "op_weight_msg_rep"
		defaultWeightMsgAssignInterim int = 100
	)

	var weightMsgAssignInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgAssignInterim, &weightMsgAssignInterim, nil,
		func(_ *rand.Rand) {
			weightMsgAssignInterim = defaultWeightMsgAssignInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAssignInterim,
		repsimulation.SimulateMsgAssignInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitInterimWork          = "op_weight_msg_rep"
		defaultWeightMsgSubmitInterimWork int = 100
	)

	var weightMsgSubmitInterimWork int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitInterimWork, &weightMsgSubmitInterimWork, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitInterimWork = defaultWeightMsgSubmitInterimWork
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitInterimWork,
		repsimulation.SimulateMsgSubmitInterimWork(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveInterim          = "op_weight_msg_rep"
		defaultWeightMsgApproveInterim int = 100
	)

	var weightMsgApproveInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveInterim, &weightMsgApproveInterim, nil,
		func(_ *rand.Rand) {
			weightMsgApproveInterim = defaultWeightMsgApproveInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveInterim,
		repsimulation.SimulateMsgApproveInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAbandonInterim          = "op_weight_msg_rep"
		defaultWeightMsgAbandonInterim int = 100
	)

	var weightMsgAbandonInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgAbandonInterim, &weightMsgAbandonInterim, nil,
		func(_ *rand.Rand) {
			weightMsgAbandonInterim = defaultWeightMsgAbandonInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAbandonInterim,
		repsimulation.SimulateMsgAbandonInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCompleteInterim          = "op_weight_msg_rep"
		defaultWeightMsgCompleteInterim int = 100
	)

	var weightMsgCompleteInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgCompleteInterim, &weightMsgCompleteInterim, nil,
		func(_ *rand.Rand) {
			weightMsgCompleteInterim = defaultWeightMsgCompleteInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCompleteInterim,
		repsimulation.SimulateMsgCompleteInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateInterim          = "op_weight_msg_rep"
		defaultWeightMsgCreateInterim int = 100
	)

	var weightMsgCreateInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateInterim, &weightMsgCreateInterim, nil,
		func(_ *rand.Rand) {
			weightMsgCreateInterim = defaultWeightMsgCreateInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateInterim,
		repsimulation.SimulateMsgCreateInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

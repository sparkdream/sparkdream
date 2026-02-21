package vote

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	votesimulation "sparkdream/x/vote/simulation"
	"sparkdream/x/vote/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	voteGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&voteGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgRegisterVoter          = "op_weight_msg_vote"
		defaultWeightMsgRegisterVoter int = 100
	)

	var weightMsgRegisterVoter int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterVoter, &weightMsgRegisterVoter, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterVoter = defaultWeightMsgRegisterVoter
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterVoter,
		votesimulation.SimulateMsgRegisterVoter(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDeactivateVoter          = "op_weight_msg_vote"
		defaultWeightMsgDeactivateVoter int = 100
	)

	var weightMsgDeactivateVoter int
	simState.AppParams.GetOrGenerate(opWeightMsgDeactivateVoter, &weightMsgDeactivateVoter, nil,
		func(_ *rand.Rand) {
			weightMsgDeactivateVoter = defaultWeightMsgDeactivateVoter
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeactivateVoter,
		votesimulation.SimulateMsgDeactivateVoter(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRotateVoterKey          = "op_weight_msg_vote"
		defaultWeightMsgRotateVoterKey int = 100
	)

	var weightMsgRotateVoterKey int
	simState.AppParams.GetOrGenerate(opWeightMsgRotateVoterKey, &weightMsgRotateVoterKey, nil,
		func(_ *rand.Rand) {
			weightMsgRotateVoterKey = defaultWeightMsgRotateVoterKey
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRotateVoterKey,
		votesimulation.SimulateMsgRotateVoterKey(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateAnonymousProposal          = "op_weight_msg_vote"
		defaultWeightMsgCreateAnonymousProposal int = 100
	)

	var weightMsgCreateAnonymousProposal int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateAnonymousProposal, &weightMsgCreateAnonymousProposal, nil,
		func(_ *rand.Rand) {
			weightMsgCreateAnonymousProposal = defaultWeightMsgCreateAnonymousProposal
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateAnonymousProposal,
		votesimulation.SimulateMsgCreateAnonymousProposal(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateProposal          = "op_weight_msg_vote"
		defaultWeightMsgCreateProposal int = 100
	)

	var weightMsgCreateProposal int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateProposal, &weightMsgCreateProposal, nil,
		func(_ *rand.Rand) {
			weightMsgCreateProposal = defaultWeightMsgCreateProposal
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateProposal,
		votesimulation.SimulateMsgCreateProposal(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelProposal          = "op_weight_msg_vote"
		defaultWeightMsgCancelProposal int = 100
	)

	var weightMsgCancelProposal int
	simState.AppParams.GetOrGenerate(opWeightMsgCancelProposal, &weightMsgCancelProposal, nil,
		func(_ *rand.Rand) {
			weightMsgCancelProposal = defaultWeightMsgCancelProposal
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelProposal,
		votesimulation.SimulateMsgCancelProposal(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgVote          = "op_weight_msg_vote"
		defaultWeightMsgVote int = 100
	)

	var weightMsgVote int
	simState.AppParams.GetOrGenerate(opWeightMsgVote, &weightMsgVote, nil,
		func(_ *rand.Rand) {
			weightMsgVote = defaultWeightMsgVote
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgVote,
		votesimulation.SimulateMsgVote(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitSealedVote          = "op_weight_msg_vote"
		defaultWeightMsgSubmitSealedVote int = 100
	)

	var weightMsgSubmitSealedVote int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitSealedVote, &weightMsgSubmitSealedVote, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitSealedVote = defaultWeightMsgSubmitSealedVote
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitSealedVote,
		votesimulation.SimulateMsgSealedVote(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevealVote          = "op_weight_msg_vote"
		defaultWeightMsgRevealVote int = 100
	)

	var weightMsgRevealVote int
	simState.AppParams.GetOrGenerate(opWeightMsgRevealVote, &weightMsgRevealVote, nil,
		func(_ *rand.Rand) {
			weightMsgRevealVote = defaultWeightMsgRevealVote
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevealVote,
		votesimulation.SimulateMsgRevealVote(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitDecryptionShare          = "op_weight_msg_vote"
		defaultWeightMsgSubmitDecryptionShare int = 100
	)

	var weightMsgSubmitDecryptionShare int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitDecryptionShare, &weightMsgSubmitDecryptionShare, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitDecryptionShare = defaultWeightMsgSubmitDecryptionShare
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitDecryptionShare,
		votesimulation.SimulateMsgSubmitDecryptionShare(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRegisterTleShare          = "op_weight_msg_vote"
		defaultWeightMsgRegisterTleShare int = 100
	)

	var weightMsgRegisterTleShare int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterTleShare, &weightMsgRegisterTleShare, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterTleShare = defaultWeightMsgRegisterTleShare
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterTleShare,
		votesimulation.SimulateMsgRegisterTLEShare(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgStoreSrs          = "op_weight_msg_vote"
		defaultWeightMsgStoreSrs int = 100
	)

	var weightMsgStoreSrs int
	simState.AppParams.GetOrGenerate(opWeightMsgStoreSrs, &weightMsgStoreSrs, nil,
		func(_ *rand.Rand) {
			weightMsgStoreSrs = defaultWeightMsgStoreSrs
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgStoreSrs,
		votesimulation.SimulateMsgStoreSRS(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

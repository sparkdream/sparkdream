package session

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	sessionsimulation "sparkdream/x/session/simulation"
	"sparkdream/x/session/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	sessionGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&sessionGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgCreateSession          = "op_weight_msg_session"
		defaultWeightMsgCreateSession int = 100
	)

	var weightMsgCreateSession int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateSession, &weightMsgCreateSession, nil,
		func(_ *rand.Rand) {
			weightMsgCreateSession = defaultWeightMsgCreateSession
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateSession,
		sessionsimulation.SimulateMsgCreateSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevokeSession          = "op_weight_msg_session"
		defaultWeightMsgRevokeSession int = 100
	)

	var weightMsgRevokeSession int
	simState.AppParams.GetOrGenerate(opWeightMsgRevokeSession, &weightMsgRevokeSession, nil,
		func(_ *rand.Rand) {
			weightMsgRevokeSession = defaultWeightMsgRevokeSession
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevokeSession,
		sessionsimulation.SimulateMsgRevokeSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgExecSession          = "op_weight_msg_session"
		defaultWeightMsgExecSession int = 100
	)

	var weightMsgExecSession int
	simState.AppParams.GetOrGenerate(opWeightMsgExecSession, &weightMsgExecSession, nil,
		func(_ *rand.Rand) {
			weightMsgExecSession = defaultWeightMsgExecSession
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgExecSession,
		sessionsimulation.SimulateMsgExecSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

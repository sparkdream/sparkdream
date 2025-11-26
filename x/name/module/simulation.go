package name

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	namesimulation "sparkdream/x/name/simulation"
	"sparkdream/x/name/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	nameGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&nameGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgRegisterName          = "op_weight_msg_name"
		defaultWeightMsgRegisterName int = 100
	)

	var weightMsgRegisterName int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterName, &weightMsgRegisterName, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterName = defaultWeightMsgRegisterName
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterName,
		namesimulation.SimulateMsgRegisterName(am.authKeeper, am.bankKeeper, am.groupKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetPrimary          = "op_weight_msg_name"
		defaultWeightMsgSetPrimary int = 100
	)

	var weightMsgSetPrimary int
	simState.AppParams.GetOrGenerate(opWeightMsgSetPrimary, &weightMsgSetPrimary, nil,
		func(_ *rand.Rand) {
			weightMsgSetPrimary = defaultWeightMsgSetPrimary
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetPrimary,
		namesimulation.SimulateMsgSetPrimary(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFileDispute          = "op_weight_msg_name"
		defaultWeightMsgFileDispute int = 100
	)

	var weightMsgFileDispute int
	simState.AppParams.GetOrGenerate(opWeightMsgFileDispute, &weightMsgFileDispute, nil,
		func(_ *rand.Rand) {
			weightMsgFileDispute = defaultWeightMsgFileDispute
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFileDispute,
		namesimulation.SimulateMsgFileDispute(am.authKeeper, am.bankKeeper, am.commonsKeeper, am.groupKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgResolveDispute          = "op_weight_msg_name"
		defaultWeightMsgResolveDispute int = 100
	)

	var weightMsgResolveDispute int
	simState.AppParams.GetOrGenerate(opWeightMsgResolveDispute, &weightMsgResolveDispute, nil,
		func(_ *rand.Rand) {
			weightMsgResolveDispute = defaultWeightMsgResolveDispute
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgResolveDispute,
		namesimulation.SimulateMsgResolveDispute(am.authKeeper, am.bankKeeper, am.commonsKeeper, am.groupKeeper, am.keeper, am.cdc, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

package futarchy

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	futarchysimulation "sparkdream/x/futarchy/simulation"
	"sparkdream/x/futarchy/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	futarchyGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&futarchyGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgCreateMarket          = "op_weight_msg_futarchy"
		defaultWeightMsgCreateMarket int = 100
	)

	var weightMsgCreateMarket int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateMarket, &weightMsgCreateMarket, nil,
		func(_ *rand.Rand) {
			weightMsgCreateMarket = defaultWeightMsgCreateMarket
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateMarket,
		futarchysimulation.SimulateMsgCreateMarket(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgTrade          = "op_weight_msg_futarchy"
		defaultWeightMsgTrade int = 100
	)

	var weightMsgTrade int
	simState.AppParams.GetOrGenerate(opWeightMsgTrade, &weightMsgTrade, nil,
		func(_ *rand.Rand) {
			weightMsgTrade = defaultWeightMsgTrade
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTrade,
		futarchysimulation.SimulateMsgTrade(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRedeem          = "op_weight_msg_futarchy"
		defaultWeightMsgRedeem int = 100
	)

	var weightMsgRedeem int
	simState.AppParams.GetOrGenerate(opWeightMsgRedeem, &weightMsgRedeem, nil,
		func(_ *rand.Rand) {
			weightMsgRedeem = defaultWeightMsgRedeem
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRedeem,
		futarchysimulation.SimulateMsgRedeem(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

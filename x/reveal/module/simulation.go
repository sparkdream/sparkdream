package reveal

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	revealsimulation "sparkdream/x/reveal/simulation"
	"sparkdream/x/reveal/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	revealGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&revealGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgPropose          = "op_weight_msg_reveal"
		defaultWeightMsgPropose int = 100
	)

	var weightMsgPropose int
	simState.AppParams.GetOrGenerate(opWeightMsgPropose, &weightMsgPropose, nil,
		func(_ *rand.Rand) {
			weightMsgPropose = defaultWeightMsgPropose
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPropose,
		revealsimulation.SimulateMsgPropose(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApprove          = "op_weight_msg_reveal"
		defaultWeightMsgApprove int = 100
	)

	var weightMsgApprove int
	simState.AppParams.GetOrGenerate(opWeightMsgApprove, &weightMsgApprove, nil,
		func(_ *rand.Rand) {
			weightMsgApprove = defaultWeightMsgApprove
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApprove,
		revealsimulation.SimulateMsgApprove(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgReject          = "op_weight_msg_reveal"
		defaultWeightMsgReject int = 100
	)

	var weightMsgReject int
	simState.AppParams.GetOrGenerate(opWeightMsgReject, &weightMsgReject, nil,
		func(_ *rand.Rand) {
			weightMsgReject = defaultWeightMsgReject
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReject,
		revealsimulation.SimulateMsgReject(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgStake          = "op_weight_msg_reveal"
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
		revealsimulation.SimulateMsgStake(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgWithdraw          = "op_weight_msg_reveal"
		defaultWeightMsgWithdraw int = 100
	)

	var weightMsgWithdraw int
	simState.AppParams.GetOrGenerate(opWeightMsgWithdraw, &weightMsgWithdraw, nil,
		func(_ *rand.Rand) {
			weightMsgWithdraw = defaultWeightMsgWithdraw
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgWithdraw,
		revealsimulation.SimulateMsgWithdraw(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgReveal          = "op_weight_msg_reveal"
		defaultWeightMsgReveal int = 100
	)

	var weightMsgReveal int
	simState.AppParams.GetOrGenerate(opWeightMsgReveal, &weightMsgReveal, nil,
		func(_ *rand.Rand) {
			weightMsgReveal = defaultWeightMsgReveal
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReveal,
		revealsimulation.SimulateMsgReveal(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgVerify          = "op_weight_msg_reveal"
		defaultWeightMsgVerify int = 100
	)

	var weightMsgVerify int
	simState.AppParams.GetOrGenerate(opWeightMsgVerify, &weightMsgVerify, nil,
		func(_ *rand.Rand) {
			weightMsgVerify = defaultWeightMsgVerify
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgVerify,
		revealsimulation.SimulateMsgVerify(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancel          = "op_weight_msg_reveal"
		defaultWeightMsgCancel int = 100
	)

	var weightMsgCancel int
	simState.AppParams.GetOrGenerate(opWeightMsgCancel, &weightMsgCancel, nil,
		func(_ *rand.Rand) {
			weightMsgCancel = defaultWeightMsgCancel
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancel,
		revealsimulation.SimulateMsgCancel(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgResolveDispute          = "op_weight_msg_reveal"
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
		revealsimulation.SimulateMsgResolveDispute(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

package shield

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	shieldsimulation "sparkdream/x/shield/simulation"
	"sparkdream/x/shield/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	shieldGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&shieldGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgShieldedExec                  = "op_weight_msg_shield"
		defaultWeightMsgShieldedExec         int = 100
		opWeightMsgTriggerDkg                    = "op_weight_msg_trigger_dkg"
		defaultWeightMsgTriggerDkg           int = 5
		opWeightMsgRegisterShieldedOp            = "op_weight_msg_register_shielded_op"
		defaultWeightMsgRegisterShieldedOp   int = 5
		opWeightMsgDeregisterShieldedOp          = "op_weight_msg_deregister_shielded_op"
		defaultWeightMsgDeregisterShieldedOp int = 5
	)

	var weightMsgShieldedExec int
	simState.AppParams.GetOrGenerate(opWeightMsgShieldedExec, &weightMsgShieldedExec, nil,
		func(_ *rand.Rand) {
			weightMsgShieldedExec = defaultWeightMsgShieldedExec
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgShieldedExec,
		shieldsimulation.SimulateMsgShieldedExec(am.accountKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgTriggerDkg int
	simState.AppParams.GetOrGenerate(opWeightMsgTriggerDkg, &weightMsgTriggerDkg, nil,
		func(_ *rand.Rand) {
			weightMsgTriggerDkg = defaultWeightMsgTriggerDkg
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTriggerDkg,
		shieldsimulation.SimulateMsgTriggerDkg(am.accountKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgRegisterShieldedOp int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterShieldedOp, &weightMsgRegisterShieldedOp, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterShieldedOp = defaultWeightMsgRegisterShieldedOp
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterShieldedOp,
		shieldsimulation.SimulateMsgRegisterShieldedOp(am.accountKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	var weightMsgDeregisterShieldedOp int
	simState.AppParams.GetOrGenerate(opWeightMsgDeregisterShieldedOp, &weightMsgDeregisterShieldedOp, nil,
		func(_ *rand.Rand) {
			weightMsgDeregisterShieldedOp = defaultWeightMsgDeregisterShieldedOp
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeregisterShieldedOp,
		shieldsimulation.SimulateMsgDeregisterShieldedOp(am.accountKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

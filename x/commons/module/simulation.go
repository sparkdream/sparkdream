package commons

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commonssimulation "sparkdream/x/commons/simulation"
	"sparkdream/x/commons/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	commonsGenesis := types.GenesisState{
		Params: types.DefaultParams(),
		PolicyPermissionsMap: []types.PolicyPermissions{{
			PolicyAddress: "0",
		}, {
			PolicyAddress: "1",
		}}}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&commonsGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgSpendFromCommons          = "op_weight_msg_commons"
		defaultWeightMsgSpendFromCommons int = 100
	)

	var weightMsgSpendFromCommons int
	simState.AppParams.GetOrGenerate(opWeightMsgSpendFromCommons, &weightMsgSpendFromCommons, nil,
		func(_ *rand.Rand) {
			weightMsgSpendFromCommons = defaultWeightMsgSpendFromCommons
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSpendFromCommons,
		commonssimulation.SimulateMsgSpendFromCommons(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgEmergencyCancelGovProposal          = "op_weight_msg_commons"
		defaultWeightMsgEmergencyCancelGovProposal int = 100
	)

	var weightMsgEmergencyCancelGovProposal int
	simState.AppParams.GetOrGenerate(opWeightMsgEmergencyCancelGovProposal, &weightMsgEmergencyCancelGovProposal, nil,
		func(_ *rand.Rand) {
			weightMsgEmergencyCancelGovProposal = defaultWeightMsgEmergencyCancelGovProposal
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgEmergencyCancelGovProposal,
		commonssimulation.SimulateMsgEmergencyCancelGovProposal(am.authKeeper, am.bankKeeper, nil, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreatePolicyPermissions          = "op_weight_msg_commons"
		defaultWeightMsgCreatePolicyPermissions int = 100
	)

	var weightMsgCreatePolicyPermissions int
	simState.AppParams.GetOrGenerate(opWeightMsgCreatePolicyPermissions, &weightMsgCreatePolicyPermissions, nil,
		func(_ *rand.Rand) {
			weightMsgCreatePolicyPermissions = defaultWeightMsgCreatePolicyPermissions
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreatePolicyPermissions,
		commonssimulation.SimulateMsgCreatePolicyPermissions(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdatePolicyPermissions          = "op_weight_msg_commons"
		defaultWeightMsgUpdatePolicyPermissions int = 100
	)

	var weightMsgUpdatePolicyPermissions int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdatePolicyPermissions, &weightMsgUpdatePolicyPermissions, nil,
		func(_ *rand.Rand) {
			weightMsgUpdatePolicyPermissions = defaultWeightMsgUpdatePolicyPermissions
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdatePolicyPermissions,
		commonssimulation.SimulateMsgUpdatePolicyPermissions(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDeletePolicyPermissions          = "op_weight_msg_commons"
		defaultWeightMsgDeletePolicyPermissions int = 100
	)

	var weightMsgDeletePolicyPermissions int
	simState.AppParams.GetOrGenerate(opWeightMsgDeletePolicyPermissions, &weightMsgDeletePolicyPermissions, nil,
		func(_ *rand.Rand) {
			weightMsgDeletePolicyPermissions = defaultWeightMsgDeletePolicyPermissions
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeletePolicyPermissions,
		commonssimulation.SimulateMsgDeletePolicyPermissions(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRegisterGroup          = "op_weight_msg_commons"
		defaultWeightMsgRegisterGroup int = 100
	)

	var weightMsgRegisterGroup int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterGroup, &weightMsgRegisterGroup, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterGroup = defaultWeightMsgRegisterGroup
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterGroup,
		commonssimulation.SimulateMsgRegisterGroup(am.authKeeper, am.bankKeeper, am.keeper, am.cdc, simState.TxConfig),
	))
	const (
		opWeightMsgRenewGroup          = "op_weight_msg_commons"
		defaultWeightMsgRenewGroup int = 100
	)

	var weightMsgRenewGroup int
	simState.AppParams.GetOrGenerate(opWeightMsgRenewGroup, &weightMsgRenewGroup, nil,
		func(_ *rand.Rand) {
			weightMsgRenewGroup = defaultWeightMsgRenewGroup
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRenewGroup,
		commonssimulation.SimulateMsgRenewGroup(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateGroupMembers          = "op_weight_msg_commons"
		defaultWeightMsgUpdateGroupMembers int = 100
	)

	var weightMsgUpdateGroupMembers int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateGroupMembers, &weightMsgUpdateGroupMembers, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateGroupMembers = defaultWeightMsgUpdateGroupMembers
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateGroupMembers,
		commonssimulation.SimulateMsgUpdateGroupMembers(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateGroupConfig          = "op_weight_msg_commons"
		defaultWeightMsgUpdateGroupConfig int = 100
	)

	var weightMsgUpdateGroupConfig int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateGroupConfig, &weightMsgUpdateGroupConfig, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateGroupConfig = defaultWeightMsgUpdateGroupConfig
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateGroupConfig,
		commonssimulation.SimulateMsgUpdateGroupConfig(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgForceUpgrade          = "op_weight_msg_commons"
		defaultWeightMsgForceUpgrade int = 100
	)

	var weightMsgForceUpgrade int
	simState.AppParams.GetOrGenerate(opWeightMsgForceUpgrade, &weightMsgForceUpgrade, nil,
		func(_ *rand.Rand) {
			weightMsgForceUpgrade = defaultWeightMsgForceUpgrade
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgForceUpgrade,
		commonssimulation.SimulateMsgForceUpgrade(am.authKeeper, am.bankKeeper, am.keeper, am.cdc, simState.TxConfig),
	))
	const (
		opWeightMsgDeleteGroup          = "op_weight_msg_commons"
		defaultWeightMsgDeleteGroup int = 100
	)

	var weightMsgDeleteGroup int
	simState.AppParams.GetOrGenerate(opWeightMsgDeleteGroup, &weightMsgDeleteGroup, nil,
		func(_ *rand.Rand) {
			weightMsgDeleteGroup = defaultWeightMsgDeleteGroup
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeleteGroup,
		commonssimulation.SimulateMsgDeleteGroup(am.authKeeper, am.bankKeeper, am.keeper, am.cdc, simState.TxConfig),
	))
	const (
		opWeightMsgVetoGroupProposals          = "op_weight_msg_commons"
		defaultWeightMsgVetoGroupProposals int = 100
	)

	var weightMsgVetoGroupProposals int
	simState.AppParams.GetOrGenerate(opWeightMsgVetoGroupProposals, &weightMsgVetoGroupProposals, nil,
		func(_ *rand.Rand) {
			weightMsgVetoGroupProposals = defaultWeightMsgVetoGroupProposals
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgVetoGroupProposals,
		commonssimulation.SimulateMsgVetoGroupProposals(am.authKeeper, am.bankKeeper, am.keeper, am.cdc, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

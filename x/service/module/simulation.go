package service

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	servicesimulation "sparkdream/x/service/simulation"
	"sparkdream/x/service/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	serviceGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&serviceGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgRegisterOperator          = "op_weight_msg_service"
		defaultWeightMsgRegisterOperator int = 100
	)

	var weightMsgRegisterOperator int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterOperator, &weightMsgRegisterOperator, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterOperator = defaultWeightMsgRegisterOperator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterOperator,
		servicesimulation.SimulateMsgRegisterOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateMetadata          = "op_weight_msg_service"
		defaultWeightMsgUpdateMetadata int = 100
	)

	var weightMsgUpdateMetadata int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateMetadata, &weightMsgUpdateMetadata, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateMetadata = defaultWeightMsgUpdateMetadata
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateMetadata,
		servicesimulation.SimulateMsgUpdateMetadata(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnbondOperator          = "op_weight_msg_service"
		defaultWeightMsgUnbondOperator int = 100
	)

	var weightMsgUnbondOperator int
	simState.AppParams.GetOrGenerate(opWeightMsgUnbondOperator, &weightMsgUnbondOperator, nil,
		func(_ *rand.Rand) {
			weightMsgUnbondOperator = defaultWeightMsgUnbondOperator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnbondOperator,
		servicesimulation.SimulateMsgUnbondOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgClaimUnbondedBond          = "op_weight_msg_service"
		defaultWeightMsgClaimUnbondedBond int = 100
	)

	var weightMsgClaimUnbondedBond int
	simState.AppParams.GetOrGenerate(opWeightMsgClaimUnbondedBond, &weightMsgClaimUnbondedBond, nil,
		func(_ *rand.Rand) {
			weightMsgClaimUnbondedBond = defaultWeightMsgClaimUnbondedBond
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgClaimUnbondedBond,
		servicesimulation.SimulateMsgClaimUnbondedBond(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgTopUpBond          = "op_weight_msg_service"
		defaultWeightMsgTopUpBond int = 100
	)

	var weightMsgTopUpBond int
	simState.AppParams.GetOrGenerate(opWeightMsgTopUpBond, &weightMsgTopUpBond, nil,
		func(_ *rand.Rand) {
			weightMsgTopUpBond = defaultWeightMsgTopUpBond
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTopUpBond,
		servicesimulation.SimulateMsgTopUpBond(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgReportOperator          = "op_weight_msg_service"
		defaultWeightMsgReportOperator int = 100
	)

	var weightMsgReportOperator int
	simState.AppParams.GetOrGenerate(opWeightMsgReportOperator, &weightMsgReportOperator, nil,
		func(_ *rand.Rand) {
			weightMsgReportOperator = defaultWeightMsgReportOperator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReportOperator,
		servicesimulation.SimulateMsgReportOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgResolveReport          = "op_weight_msg_service"
		defaultWeightMsgResolveReport int = 100
	)

	var weightMsgResolveReport int
	simState.AppParams.GetOrGenerate(opWeightMsgResolveReport, &weightMsgResolveReport, nil,
		func(_ *rand.Rand) {
			weightMsgResolveReport = defaultWeightMsgResolveReport
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgResolveReport,
		servicesimulation.SimulateMsgResolveReport(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgContestSlash          = "op_weight_msg_service"
		defaultWeightMsgContestSlash int = 100
	)

	var weightMsgContestSlash int
	simState.AppParams.GetOrGenerate(opWeightMsgContestSlash, &weightMsgContestSlash, nil,
		func(_ *rand.Rand) {
			weightMsgContestSlash = defaultWeightMsgContestSlash
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgContestSlash,
		servicesimulation.SimulateMsgContestSlash(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgResolveReportByJury          = "op_weight_msg_service"
		defaultWeightMsgResolveReportByJury int = 100
	)

	var weightMsgResolveReportByJury int
	simState.AppParams.GetOrGenerate(opWeightMsgResolveReportByJury, &weightMsgResolveReportByJury, nil,
		func(_ *rand.Rand) {
			weightMsgResolveReportByJury = defaultWeightMsgResolveReportByJury
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgResolveReportByJury,
		servicesimulation.SimulateMsgResolveReportByJury(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgOpenControllerTransferCase          = "op_weight_msg_service"
		defaultWeightMsgOpenControllerTransferCase int = 100
	)

	var weightMsgOpenControllerTransferCase int
	simState.AppParams.GetOrGenerate(opWeightMsgOpenControllerTransferCase, &weightMsgOpenControllerTransferCase, nil,
		func(_ *rand.Rand) {
			weightMsgOpenControllerTransferCase = defaultWeightMsgOpenControllerTransferCase
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgOpenControllerTransferCase,
		servicesimulation.SimulateMsgOpenControllerTransferCase(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFinalizeControllerTransfer          = "op_weight_msg_service"
		defaultWeightMsgFinalizeControllerTransfer int = 100
	)

	var weightMsgFinalizeControllerTransfer int
	simState.AppParams.GetOrGenerate(opWeightMsgFinalizeControllerTransfer, &weightMsgFinalizeControllerTransfer, nil,
		func(_ *rand.Rand) {
			weightMsgFinalizeControllerTransfer = defaultWeightMsgFinalizeControllerTransfer
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFinalizeControllerTransfer,
		servicesimulation.SimulateMsgFinalizeControllerTransfer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

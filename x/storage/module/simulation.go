package storage

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	storagesimulation "sparkdream/x/storage/simulation"
	"sparkdream/x/storage/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	// The default genesis: default params and the arweave backend. Operators
	// and scanners are registered and approved by delivered transactions.
	storageGenesis := *types.DefaultGenesis()
	// Windows of a few blocks, so a simulation of a few hundred blocks sees
	// claims lapse, completions settle, holds burn and requests expire. They
	// must stay inside the unbonding period the x/service simulation gives
	// every service type (30 blocks): 2 x 4 + 2 x (2 + 4) + 6 = 26.
	p := &storageGenesis.Params
	p.RequestTtlBlocks = 40
	p.ClaimWindowBlocks = 4
	p.SettleWindowBlocks = 2
	p.DisputeWindowBlocks = 4
	p.FlagHoldBlocks = 6
	p.MinUnreachableBlocks = 2
	p.TerminalRetentionBlocks = 10
	p.CounterWindowBlocks = 50
	p.DeadlineRetryBlocks = 2
	if err := p.Validate(); err != nil {
		panic(err)
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&storageGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgRequestPermanentCopy          = "op_weight_msg_storage"
		defaultWeightMsgRequestPermanentCopy int = 100
	)

	var weightMsgRequestPermanentCopy int
	simState.AppParams.GetOrGenerate(opWeightMsgRequestPermanentCopy, &weightMsgRequestPermanentCopy, nil,
		func(_ *rand.Rand) {
			weightMsgRequestPermanentCopy = defaultWeightMsgRequestPermanentCopy
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRequestPermanentCopy,
		storagesimulation.SimulateMsgRequestPermanentCopy(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAddSource          = "op_weight_msg_storage"
		defaultWeightMsgAddSource int = 100
	)

	var weightMsgAddSource int
	simState.AppParams.GetOrGenerate(opWeightMsgAddSource, &weightMsgAddSource, nil,
		func(_ *rand.Rand) {
			weightMsgAddSource = defaultWeightMsgAddSource
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAddSource,
		storagesimulation.SimulateMsgAddSource(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelRequest          = "op_weight_msg_storage"
		defaultWeightMsgCancelRequest int = 100
	)

	var weightMsgCancelRequest int
	simState.AppParams.GetOrGenerate(opWeightMsgCancelRequest, &weightMsgCancelRequest, nil,
		func(_ *rand.Rand) {
			weightMsgCancelRequest = defaultWeightMsgCancelRequest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelRequest,
		storagesimulation.SimulateMsgCancelRequest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDisputeCompletion          = "op_weight_msg_storage"
		defaultWeightMsgDisputeCompletion int = 100
	)

	var weightMsgDisputeCompletion int
	simState.AppParams.GetOrGenerate(opWeightMsgDisputeCompletion, &weightMsgDisputeCompletion, nil,
		func(_ *rand.Rand) {
			weightMsgDisputeCompletion = defaultWeightMsgDisputeCompletion
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDisputeCompletion,
		storagesimulation.SimulateMsgDisputeCompletion(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgClaimRequest          = "op_weight_msg_storage"
		defaultWeightMsgClaimRequest int = 100
	)

	var weightMsgClaimRequest int
	simState.AppParams.GetOrGenerate(opWeightMsgClaimRequest, &weightMsgClaimRequest, nil,
		func(_ *rand.Rand) {
			weightMsgClaimRequest = defaultWeightMsgClaimRequest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgClaimRequest,
		storagesimulation.SimulateMsgClaimRequest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgExtendClaim          = "op_weight_msg_storage"
		defaultWeightMsgExtendClaim int = 100
	)

	var weightMsgExtendClaim int
	simState.AppParams.GetOrGenerate(opWeightMsgExtendClaim, &weightMsgExtendClaim, nil,
		func(_ *rand.Rand) {
			weightMsgExtendClaim = defaultWeightMsgExtendClaim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgExtendClaim,
		storagesimulation.SimulateMsgExtendClaim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCompleteRequest          = "op_weight_msg_storage"
		defaultWeightMsgCompleteRequest int = 100
	)

	var weightMsgCompleteRequest int
	simState.AppParams.GetOrGenerate(opWeightMsgCompleteRequest, &weightMsgCompleteRequest, nil,
		func(_ *rand.Rand) {
			weightMsgCompleteRequest = defaultWeightMsgCompleteRequest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCompleteRequest,
		storagesimulation.SimulateMsgCompleteRequest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDeclineRequest          = "op_weight_msg_storage"
		defaultWeightMsgDeclineRequest int = 100
	)

	var weightMsgDeclineRequest int
	simState.AppParams.GetOrGenerate(opWeightMsgDeclineRequest, &weightMsgDeclineRequest, nil,
		func(_ *rand.Rand) {
			weightMsgDeclineRequest = defaultWeightMsgDeclineRequest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeclineRequest,
		storagesimulation.SimulateMsgDeclineRequest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFlagRemoved          = "op_weight_msg_storage"
		defaultWeightMsgFlagRemoved int = 100
	)

	var weightMsgFlagRemoved int
	simState.AppParams.GetOrGenerate(opWeightMsgFlagRemoved, &weightMsgFlagRemoved, nil,
		func(_ *rand.Rand) {
			weightMsgFlagRemoved = defaultWeightMsgFlagRemoved
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFlagRemoved,
		storagesimulation.SimulateMsgFlagRemoved(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnflagRemoved          = "op_weight_msg_storage"
		defaultWeightMsgUnflagRemoved int = 100
	)

	var weightMsgUnflagRemoved int
	simState.AppParams.GetOrGenerate(opWeightMsgUnflagRemoved, &weightMsgUnflagRemoved, nil,
		func(_ *rand.Rand) {
			weightMsgUnflagRemoved = defaultWeightMsgUnflagRemoved
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnflagRemoved,
		storagesimulation.SimulateMsgUnflagRemoved(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgVoidCompletion          = "op_weight_msg_storage"
		defaultWeightMsgVoidCompletion int = 100
	)

	var weightMsgVoidCompletion int
	simState.AppParams.GetOrGenerate(opWeightMsgVoidCompletion, &weightMsgVoidCompletion, nil,
		func(_ *rand.Rand) {
			weightMsgVoidCompletion = defaultWeightMsgVoidCompletion
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgVoidCompletion,
		storagesimulation.SimulateMsgVoidCompletion(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveOperator          = "op_weight_msg_storage"
		defaultWeightMsgApproveOperator int = 100
	)

	var weightMsgApproveOperator int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveOperator, &weightMsgApproveOperator, nil,
		func(_ *rand.Rand) {
			weightMsgApproveOperator = defaultWeightMsgApproveOperator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveOperator,
		storagesimulation.SimulateMsgApproveOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRemoveOperator          = "op_weight_msg_storage"
		defaultWeightMsgRemoveOperator int = 100
	)

	var weightMsgRemoveOperator int
	simState.AppParams.GetOrGenerate(opWeightMsgRemoveOperator, &weightMsgRemoveOperator, nil,
		func(_ *rand.Rand) {
			weightMsgRemoveOperator = defaultWeightMsgRemoveOperator
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemoveOperator,
		storagesimulation.SimulateMsgRemoveOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveScanner          = "op_weight_msg_storage"
		defaultWeightMsgApproveScanner int = 100
	)

	var weightMsgApproveScanner int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveScanner, &weightMsgApproveScanner, nil,
		func(_ *rand.Rand) {
			weightMsgApproveScanner = defaultWeightMsgApproveScanner
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveScanner,
		storagesimulation.SimulateMsgApproveScanner(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRemoveScanner          = "op_weight_msg_storage"
		defaultWeightMsgRemoveScanner int = 100
	)

	var weightMsgRemoveScanner int
	simState.AppParams.GetOrGenerate(opWeightMsgRemoveScanner, &weightMsgRemoveScanner, nil,
		func(_ *rand.Rand) {
			weightMsgRemoveScanner = defaultWeightMsgRemoveScanner
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemoveScanner,
		storagesimulation.SimulateMsgRemoveScanner(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateBackend          = "op_weight_msg_storage"
		defaultWeightMsgUpdateBackend int = 100
	)

	var weightMsgUpdateBackend int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateBackend, &weightMsgUpdateBackend, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateBackend = defaultWeightMsgUpdateBackend
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateBackend,
		storagesimulation.SimulateMsgUpdateBackend(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateOperationalParams          = "op_weight_msg_storage"
		defaultWeightMsgUpdateOperationalParams int = 100
	)

	var weightMsgUpdateOperationalParams int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateOperationalParams, &weightMsgUpdateOperationalParams, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateOperationalParams = defaultWeightMsgUpdateOperationalParams
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateOperationalParams,
		storagesimulation.SimulateMsgUpdateOperationalParams(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

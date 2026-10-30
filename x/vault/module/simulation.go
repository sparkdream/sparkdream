package vault

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	vaultsimulation "sparkdream/x/vault/simulation"
	"sparkdream/x/vault/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	vaultGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&vaultGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgCreateVault          = "op_weight_msg_vault"
		defaultWeightMsgCreateVault int = 100
	)

	var weightMsgCreateVault int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateVault, &weightMsgCreateVault, nil,
		func(_ *rand.Rand) {
			weightMsgCreateVault = defaultWeightMsgCreateVault
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateVault,
		vaultsimulation.SimulateMsgCreateVault(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgPropose          = "op_weight_msg_vault"
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
		vaultsimulation.SimulateMsgPropose(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApprove          = "op_weight_msg_vault"
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
		vaultsimulation.SimulateMsgApprove(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevokeApproval          = "op_weight_msg_vault"
		defaultWeightMsgRevokeApproval int = 100
	)

	var weightMsgRevokeApproval int
	simState.AppParams.GetOrGenerate(opWeightMsgRevokeApproval, &weightMsgRevokeApproval, nil,
		func(_ *rand.Rand) {
			weightMsgRevokeApproval = defaultWeightMsgRevokeApproval
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevokeApproval,
		vaultsimulation.SimulateMsgRevokeApproval(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgExecute          = "op_weight_msg_vault"
		defaultWeightMsgExecute int = 100
	)

	var weightMsgExecute int
	simState.AppParams.GetOrGenerate(opWeightMsgExecute, &weightMsgExecute, nil,
		func(_ *rand.Rand) {
			weightMsgExecute = defaultWeightMsgExecute
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgExecute,
		vaultsimulation.SimulateMsgExecute(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelProposal          = "op_weight_msg_vault"
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
		vaultsimulation.SimulateMsgCancelProposal(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateVault          = "op_weight_msg_vault"
		defaultWeightMsgUpdateVault int = 100
	)

	var weightMsgUpdateVault int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateVault, &weightMsgUpdateVault, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateVault = defaultWeightMsgUpdateVault
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateVault,
		vaultsimulation.SimulateMsgUpdateVault(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAcceptVaultSigner          = "op_weight_msg_vault"
		defaultWeightMsgAcceptVaultSigner int = 100
	)

	var weightMsgAcceptVaultSigner int
	simState.AppParams.GetOrGenerate(opWeightMsgAcceptVaultSigner, &weightMsgAcceptVaultSigner, nil,
		func(_ *rand.Rand) {
			weightMsgAcceptVaultSigner = defaultWeightMsgAcceptVaultSigner
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAcceptVaultSigner,
		vaultsimulation.SimulateMsgAcceptVaultSigner(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgLeaveVault          = "op_weight_msg_vault"
		defaultWeightMsgLeaveVault int = 100
	)

	var weightMsgLeaveVault int
	simState.AppParams.GetOrGenerate(opWeightMsgLeaveVault, &weightMsgLeaveVault, nil,
		func(_ *rand.Rand) {
			weightMsgLeaveVault = defaultWeightMsgLeaveVault
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgLeaveVault,
		vaultsimulation.SimulateMsgLeaveVault(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

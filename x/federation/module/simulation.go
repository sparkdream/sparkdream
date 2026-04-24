package federation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	federationsimulation "sparkdream/x/federation/simulation"
	"sparkdream/x/federation/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	federationGenesis := types.GenesisState{
		Params: types.DefaultParams(),
		PortId: types.PortID,
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&federationGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgRegisterPeer          = "op_weight_msg_federation"
		defaultWeightMsgRegisterPeer int = 100
	)

	var weightMsgRegisterPeer int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterPeer, &weightMsgRegisterPeer, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterPeer = defaultWeightMsgRegisterPeer
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterPeer,
		federationsimulation.SimulateMsgRegisterPeer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRemovePeer          = "op_weight_msg_federation"
		defaultWeightMsgRemovePeer int = 100
	)

	var weightMsgRemovePeer int
	simState.AppParams.GetOrGenerate(opWeightMsgRemovePeer, &weightMsgRemovePeer, nil,
		func(_ *rand.Rand) {
			weightMsgRemovePeer = defaultWeightMsgRemovePeer
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemovePeer,
		federationsimulation.SimulateMsgRemovePeer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSuspendPeer          = "op_weight_msg_federation"
		defaultWeightMsgSuspendPeer int = 100
	)

	var weightMsgSuspendPeer int
	simState.AppParams.GetOrGenerate(opWeightMsgSuspendPeer, &weightMsgSuspendPeer, nil,
		func(_ *rand.Rand) {
			weightMsgSuspendPeer = defaultWeightMsgSuspendPeer
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSuspendPeer,
		federationsimulation.SimulateMsgSuspendPeer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgResumePeer          = "op_weight_msg_federation"
		defaultWeightMsgResumePeer int = 100
	)

	var weightMsgResumePeer int
	simState.AppParams.GetOrGenerate(opWeightMsgResumePeer, &weightMsgResumePeer, nil,
		func(_ *rand.Rand) {
			weightMsgResumePeer = defaultWeightMsgResumePeer
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgResumePeer,
		federationsimulation.SimulateMsgResumePeer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdatePeerPolicy          = "op_weight_msg_federation"
		defaultWeightMsgUpdatePeerPolicy int = 100
	)

	var weightMsgUpdatePeerPolicy int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdatePeerPolicy, &weightMsgUpdatePeerPolicy, nil,
		func(_ *rand.Rand) {
			weightMsgUpdatePeerPolicy = defaultWeightMsgUpdatePeerPolicy
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdatePeerPolicy,
		federationsimulation.SimulateMsgUpdatePeerPolicy(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRegisterBridge          = "op_weight_msg_federation"
		defaultWeightMsgRegisterBridge int = 100
	)

	var weightMsgRegisterBridge int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterBridge, &weightMsgRegisterBridge, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterBridge = defaultWeightMsgRegisterBridge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterBridge,
		federationsimulation.SimulateMsgRegisterBridge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevokeBridge          = "op_weight_msg_federation"
		defaultWeightMsgRevokeBridge int = 100
	)

	var weightMsgRevokeBridge int
	simState.AppParams.GetOrGenerate(opWeightMsgRevokeBridge, &weightMsgRevokeBridge, nil,
		func(_ *rand.Rand) {
			weightMsgRevokeBridge = defaultWeightMsgRevokeBridge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevokeBridge,
		federationsimulation.SimulateMsgRevokeBridge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSlashBridge          = "op_weight_msg_federation"
		defaultWeightMsgSlashBridge int = 100
	)

	var weightMsgSlashBridge int
	simState.AppParams.GetOrGenerate(opWeightMsgSlashBridge, &weightMsgSlashBridge, nil,
		func(_ *rand.Rand) {
			weightMsgSlashBridge = defaultWeightMsgSlashBridge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSlashBridge,
		federationsimulation.SimulateMsgSlashBridge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateBridge          = "op_weight_msg_federation"
		defaultWeightMsgUpdateBridge int = 100
	)

	var weightMsgUpdateBridge int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateBridge, &weightMsgUpdateBridge, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateBridge = defaultWeightMsgUpdateBridge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateBridge,
		federationsimulation.SimulateMsgUpdateBridge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnbondBridge          = "op_weight_msg_federation"
		defaultWeightMsgUnbondBridge int = 100
	)

	var weightMsgUnbondBridge int
	simState.AppParams.GetOrGenerate(opWeightMsgUnbondBridge, &weightMsgUnbondBridge, nil,
		func(_ *rand.Rand) {
			weightMsgUnbondBridge = defaultWeightMsgUnbondBridge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnbondBridge,
		federationsimulation.SimulateMsgUnbondBridge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgTopUpBridgeStake          = "op_weight_msg_federation"
		defaultWeightMsgTopUpBridgeStake int = 100
	)

	var weightMsgTopUpBridgeStake int
	simState.AppParams.GetOrGenerate(opWeightMsgTopUpBridgeStake, &weightMsgTopUpBridgeStake, nil,
		func(_ *rand.Rand) {
			weightMsgTopUpBridgeStake = defaultWeightMsgTopUpBridgeStake
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTopUpBridgeStake,
		federationsimulation.SimulateMsgTopUpBridgeStake(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitFederatedContent          = "op_weight_msg_federation"
		defaultWeightMsgSubmitFederatedContent int = 100
	)

	var weightMsgSubmitFederatedContent int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitFederatedContent, &weightMsgSubmitFederatedContent, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitFederatedContent = defaultWeightMsgSubmitFederatedContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitFederatedContent,
		federationsimulation.SimulateMsgSubmitFederatedContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFederateContent          = "op_weight_msg_federation"
		defaultWeightMsgFederateContent int = 100
	)

	var weightMsgFederateContent int
	simState.AppParams.GetOrGenerate(opWeightMsgFederateContent, &weightMsgFederateContent, nil,
		func(_ *rand.Rand) {
			weightMsgFederateContent = defaultWeightMsgFederateContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFederateContent,
		federationsimulation.SimulateMsgFederateContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAttestOutbound          = "op_weight_msg_federation"
		defaultWeightMsgAttestOutbound int = 100
	)

	var weightMsgAttestOutbound int
	simState.AppParams.GetOrGenerate(opWeightMsgAttestOutbound, &weightMsgAttestOutbound, nil,
		func(_ *rand.Rand) {
			weightMsgAttestOutbound = defaultWeightMsgAttestOutbound
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAttestOutbound,
		federationsimulation.SimulateMsgAttestOutbound(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgModerateContent          = "op_weight_msg_federation"
		defaultWeightMsgModerateContent int = 100
	)

	var weightMsgModerateContent int
	simState.AppParams.GetOrGenerate(opWeightMsgModerateContent, &weightMsgModerateContent, nil,
		func(_ *rand.Rand) {
			weightMsgModerateContent = defaultWeightMsgModerateContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgModerateContent,
		federationsimulation.SimulateMsgModerateContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgLinkIdentity          = "op_weight_msg_federation"
		defaultWeightMsgLinkIdentity int = 100
	)

	var weightMsgLinkIdentity int
	simState.AppParams.GetOrGenerate(opWeightMsgLinkIdentity, &weightMsgLinkIdentity, nil,
		func(_ *rand.Rand) {
			weightMsgLinkIdentity = defaultWeightMsgLinkIdentity
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgLinkIdentity,
		federationsimulation.SimulateMsgLinkIdentity(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnlinkIdentity          = "op_weight_msg_federation"
		defaultWeightMsgUnlinkIdentity int = 100
	)

	var weightMsgUnlinkIdentity int
	simState.AppParams.GetOrGenerate(opWeightMsgUnlinkIdentity, &weightMsgUnlinkIdentity, nil,
		func(_ *rand.Rand) {
			weightMsgUnlinkIdentity = defaultWeightMsgUnlinkIdentity
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnlinkIdentity,
		federationsimulation.SimulateMsgUnlinkIdentity(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgConfirmIdentityLink          = "op_weight_msg_federation"
		defaultWeightMsgConfirmIdentityLink int = 100
	)

	var weightMsgConfirmIdentityLink int
	simState.AppParams.GetOrGenerate(opWeightMsgConfirmIdentityLink, &weightMsgConfirmIdentityLink, nil,
		func(_ *rand.Rand) {
			weightMsgConfirmIdentityLink = defaultWeightMsgConfirmIdentityLink
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgConfirmIdentityLink,
		federationsimulation.SimulateMsgConfirmIdentityLink(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRequestReputationAttestation          = "op_weight_msg_federation"
		defaultWeightMsgRequestReputationAttestation int = 100
	)

	var weightMsgRequestReputationAttestation int
	simState.AppParams.GetOrGenerate(opWeightMsgRequestReputationAttestation, &weightMsgRequestReputationAttestation, nil,
		func(_ *rand.Rand) {
			weightMsgRequestReputationAttestation = defaultWeightMsgRequestReputationAttestation
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRequestReputationAttestation,
		federationsimulation.SimulateMsgRequestReputationAttestation(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgVerifyContent          = "op_weight_msg_federation"
		defaultWeightMsgVerifyContent int = 100
	)

	var weightMsgVerifyContent int
	simState.AppParams.GetOrGenerate(opWeightMsgVerifyContent, &weightMsgVerifyContent, nil,
		func(_ *rand.Rand) {
			weightMsgVerifyContent = defaultWeightMsgVerifyContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgVerifyContent,
		federationsimulation.SimulateMsgVerifyContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgChallengeVerification          = "op_weight_msg_federation"
		defaultWeightMsgChallengeVerification int = 100
	)

	var weightMsgChallengeVerification int
	simState.AppParams.GetOrGenerate(opWeightMsgChallengeVerification, &weightMsgChallengeVerification, nil,
		func(_ *rand.Rand) {
			weightMsgChallengeVerification = defaultWeightMsgChallengeVerification
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgChallengeVerification,
		federationsimulation.SimulateMsgChallengeVerification(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitArbiterHash          = "op_weight_msg_federation"
		defaultWeightMsgSubmitArbiterHash int = 100
	)

	var weightMsgSubmitArbiterHash int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitArbiterHash, &weightMsgSubmitArbiterHash, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitArbiterHash = defaultWeightMsgSubmitArbiterHash
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitArbiterHash,
		federationsimulation.SimulateMsgSubmitArbiterHash(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgEscalateChallenge          = "op_weight_msg_federation"
		defaultWeightMsgEscalateChallenge int = 100
	)

	var weightMsgEscalateChallenge int
	simState.AppParams.GetOrGenerate(opWeightMsgEscalateChallenge, &weightMsgEscalateChallenge, nil,
		func(_ *rand.Rand) {
			weightMsgEscalateChallenge = defaultWeightMsgEscalateChallenge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgEscalateChallenge,
		federationsimulation.SimulateMsgEscalateChallenge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateOperationalParams          = "op_weight_msg_federation"
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
		federationsimulation.SimulateMsgUpdateOperationalParams(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

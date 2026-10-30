package rep

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	repsimulation "sparkdream/x/rep/simulation"
	"sparkdream/x/rep/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	// Seed common tags so initiative/project/interim simulations can reference
	// them — the tag registry validates every tag used in those messages.
	seededTags := []string{"backend", "frontend", "design", "devops", "documentation", "testing"}
	tagMap := make([]types.Tag, 0, len(seededTags))
	for _, name := range seededTags {
		tagMap = append(tagMap, types.Tag{Name: name})
	}
	params := simGenesisParams()
	repGenesis := types.GenesisState{
		Params:    params,
		TagMap:    tagMap,
		MemberMap: simGenesisMembers(simState, params, seededTags),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&repGenesis)
}

// simYear is a year in seconds.
const simYear = int64(365 * 24 * 3600)

// simGenesisParams returns the params of the simulated genesis: the
// defaults, with two adjustments for the simulator's clock (5,000-10,000
// seconds per block, so a 200-block run spans two to three weeks):
//
//   - Epochs of 20 blocks, a two-epoch review period and challenge response
//     window and a one-epoch challenge period, so initiatives reach review,
//     completion and challenge deadlines within a run instead of after
//     14,400-block epochs; bounty contributions reclaimable after 30 blocks
//     instead of 14,400.
//   - Migration delays of a century. Finalizing a migration tombstones the
//     old address, and the SDK modules' simulations keep signing with every
//     sim account (a tombstone may sign almost nothing), so the rep
//     simulation requests, accepts, objects to, cancels and recovers
//     migrations for real but none finalizes. The accept window and the
//     recovery-commit TTL are stretched to 60 days so a request or a seal
//     outlives the few blocks until its accept or reveal.
func simGenesisParams() types.Params {
	params := types.DefaultParams()
	params.EpochBlocks = 20
	params.DefaultReviewPeriodEpochs = 2
	params.DefaultChallengePeriodEpochs = 1
	params.InitiativeBountyReclaimDelay = 30
	params.ChallengeResponseDeadlineEpochs = 2
	params.KeyMigrationDelay = 100 * simYear
	params.RecoveryMigrationDelay = 100 * simYear
	params.CouncilMigrationDelay = 100 * simYear
	params.RecoveryChangeDelay = 200 * simYear
	params.MigrationAcceptWindow = 60 * 24 * 3600
	params.RecoveryCommitTtl = 60 * 24 * 3600
	return params
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgInviteMember          = "op_weight_msg_rep"
		defaultWeightMsgInviteMember int = 100
	)

	var weightMsgInviteMember int
	simState.AppParams.GetOrGenerate(opWeightMsgInviteMember, &weightMsgInviteMember, nil,
		func(_ *rand.Rand) {
			weightMsgInviteMember = defaultWeightMsgInviteMember
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgInviteMember,
		repsimulation.SimulateMsgInviteMember(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAcceptInvitation          = "op_weight_msg_rep"
		defaultWeightMsgAcceptInvitation int = 100
	)

	var weightMsgAcceptInvitation int
	simState.AppParams.GetOrGenerate(opWeightMsgAcceptInvitation, &weightMsgAcceptInvitation, nil,
		func(_ *rand.Rand) {
			weightMsgAcceptInvitation = defaultWeightMsgAcceptInvitation
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAcceptInvitation,
		repsimulation.SimulateMsgAcceptInvitation(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgTransferDream          = "op_weight_msg_rep"
		defaultWeightMsgTransferDream int = 100
	)

	var weightMsgTransferDream int
	simState.AppParams.GetOrGenerate(opWeightMsgTransferDream, &weightMsgTransferDream, nil,
		func(_ *rand.Rand) {
			weightMsgTransferDream = defaultWeightMsgTransferDream
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTransferDream,
		repsimulation.SimulateMsgTransferDream(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgProposeProject          = "op_weight_msg_rep"
		defaultWeightMsgProposeProject int = 100
	)

	var weightMsgProposeProject int
	simState.AppParams.GetOrGenerate(opWeightMsgProposeProject, &weightMsgProposeProject, nil,
		func(_ *rand.Rand) {
			weightMsgProposeProject = defaultWeightMsgProposeProject
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgProposeProject,
		repsimulation.SimulateMsgProposeProject(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveProjectBudget          = "op_weight_msg_rep"
		defaultWeightMsgApproveProjectBudget int = 100
	)

	var weightMsgApproveProjectBudget int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveProjectBudget, &weightMsgApproveProjectBudget, nil,
		func(_ *rand.Rand) {
			weightMsgApproveProjectBudget = defaultWeightMsgApproveProjectBudget
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveProjectBudget,
		repsimulation.SimulateMsgApproveProjectBudget(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelProject          = "op_weight_msg_rep"
		defaultWeightMsgCancelProject int = 100
	)

	var weightMsgCancelProject int
	simState.AppParams.GetOrGenerate(opWeightMsgCancelProject, &weightMsgCancelProject, nil,
		func(_ *rand.Rand) {
			weightMsgCancelProject = defaultWeightMsgCancelProject
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelProject,
		repsimulation.SimulateMsgCancelProject(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateInitiative          = "op_weight_msg_rep"
		defaultWeightMsgCreateInitiative int = 100
	)

	var weightMsgCreateInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateInitiative, &weightMsgCreateInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgCreateInitiative = defaultWeightMsgCreateInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateInitiative,
		repsimulation.SimulateMsgCreateInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAssignInitiative          = "op_weight_msg_rep"
		defaultWeightMsgAssignInitiative int = 100
	)

	var weightMsgAssignInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgAssignInitiative, &weightMsgAssignInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgAssignInitiative = defaultWeightMsgAssignInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAssignInitiative,
		repsimulation.SimulateMsgAssignInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitInitiativeWork          = "op_weight_msg_rep"
		defaultWeightMsgSubmitInitiativeWork int = 100
	)

	var weightMsgSubmitInitiativeWork int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitInitiativeWork, &weightMsgSubmitInitiativeWork, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitInitiativeWork = defaultWeightMsgSubmitInitiativeWork
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitInitiativeWork,
		repsimulation.SimulateMsgSubmitInitiativeWork(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveInitiative          = "op_weight_msg_rep"
		defaultWeightMsgApproveInitiative int = 100
	)

	var weightMsgApproveInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveInitiative, &weightMsgApproveInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgApproveInitiative = defaultWeightMsgApproveInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveInitiative,
		repsimulation.SimulateMsgApproveInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnassignInitiative          = "op_weight_msg_rep"
		defaultWeightMsgUnassignInitiative int = 100
	)

	var weightMsgUnassignInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgUnassignInitiative, &weightMsgUnassignInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgUnassignInitiative = defaultWeightMsgUnassignInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnassignInitiative,
		repsimulation.SimulateMsgUnassignInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCompleteInitiative          = "op_weight_msg_rep"
		defaultWeightMsgCompleteInitiative int = 100
	)

	var weightMsgCompleteInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgCompleteInitiative, &weightMsgCompleteInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgCompleteInitiative = defaultWeightMsgCompleteInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCompleteInitiative,
		repsimulation.SimulateMsgCompleteInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgStake          = "op_weight_msg_rep"
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
		repsimulation.SimulateMsgStake(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUnstake          = "op_weight_msg_rep"
		defaultWeightMsgUnstake int = 100
	)

	var weightMsgUnstake int
	simState.AppParams.GetOrGenerate(opWeightMsgUnstake, &weightMsgUnstake, nil,
		func(_ *rand.Rand) {
			weightMsgUnstake = defaultWeightMsgUnstake
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUnstake,
		repsimulation.SimulateMsgUnstake(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateChallenge          = "op_weight_msg_rep"
		defaultWeightMsgCreateChallenge int = 100
	)

	var weightMsgCreateChallenge int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateChallenge, &weightMsgCreateChallenge, nil,
		func(_ *rand.Rand) {
			weightMsgCreateChallenge = defaultWeightMsgCreateChallenge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateChallenge,
		repsimulation.SimulateMsgCreateChallenge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRespondToChallenge          = "op_weight_msg_rep"
		defaultWeightMsgRespondToChallenge int = 100
	)

	var weightMsgRespondToChallenge int
	simState.AppParams.GetOrGenerate(opWeightMsgRespondToChallenge, &weightMsgRespondToChallenge, nil,
		func(_ *rand.Rand) {
			weightMsgRespondToChallenge = defaultWeightMsgRespondToChallenge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRespondToChallenge,
		repsimulation.SimulateMsgRespondToChallenge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitJurorVote          = "op_weight_msg_rep"
		defaultWeightMsgSubmitJurorVote int = 100
	)

	var weightMsgSubmitJurorVote int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitJurorVote, &weightMsgSubmitJurorVote, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitJurorVote = defaultWeightMsgSubmitJurorVote
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitJurorVote,
		repsimulation.SimulateMsgSubmitJurorVote(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitExpertTestimony          = "op_weight_msg_rep"
		defaultWeightMsgSubmitExpertTestimony int = 100
	)

	var weightMsgSubmitExpertTestimony int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitExpertTestimony, &weightMsgSubmitExpertTestimony, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitExpertTestimony = defaultWeightMsgSubmitExpertTestimony
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitExpertTestimony,
		repsimulation.SimulateMsgSubmitExpertTestimony(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAssignInterim          = "op_weight_msg_rep"
		defaultWeightMsgAssignInterim int = 100
	)

	var weightMsgAssignInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgAssignInterim, &weightMsgAssignInterim, nil,
		func(_ *rand.Rand) {
			weightMsgAssignInterim = defaultWeightMsgAssignInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAssignInterim,
		repsimulation.SimulateMsgAssignInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSubmitInterimWork          = "op_weight_msg_rep"
		defaultWeightMsgSubmitInterimWork int = 100
	)

	var weightMsgSubmitInterimWork int
	simState.AppParams.GetOrGenerate(opWeightMsgSubmitInterimWork, &weightMsgSubmitInterimWork, nil,
		func(_ *rand.Rand) {
			weightMsgSubmitInterimWork = defaultWeightMsgSubmitInterimWork
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSubmitInterimWork,
		repsimulation.SimulateMsgSubmitInterimWork(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgApproveInterim          = "op_weight_msg_rep"
		defaultWeightMsgApproveInterim int = 100
	)

	var weightMsgApproveInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgApproveInterim, &weightMsgApproveInterim, nil,
		func(_ *rand.Rand) {
			weightMsgApproveInterim = defaultWeightMsgApproveInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgApproveInterim,
		repsimulation.SimulateMsgApproveInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAbandonInterim          = "op_weight_msg_rep"
		defaultWeightMsgAbandonInterim int = 100
	)

	var weightMsgAbandonInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgAbandonInterim, &weightMsgAbandonInterim, nil,
		func(_ *rand.Rand) {
			weightMsgAbandonInterim = defaultWeightMsgAbandonInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAbandonInterim,
		repsimulation.SimulateMsgAbandonInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCompleteInterim          = "op_weight_msg_rep"
		defaultWeightMsgCompleteInterim int = 100
	)

	var weightMsgCompleteInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgCompleteInterim, &weightMsgCompleteInterim, nil,
		func(_ *rand.Rand) {
			weightMsgCompleteInterim = defaultWeightMsgCompleteInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCompleteInterim,
		repsimulation.SimulateMsgCompleteInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateInterim          = "op_weight_msg_rep"
		defaultWeightMsgCreateInterim int = 100
	)

	var weightMsgCreateInterim int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateInterim, &weightMsgCreateInterim, nil,
		func(_ *rand.Rand) {
			weightMsgCreateInterim = defaultWeightMsgCreateInterim
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateInterim,
		repsimulation.SimulateMsgCreateInterim(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgChallengeContent          = "op_weight_msg_rep"
		defaultWeightMsgChallengeContent int = 50
	)

	var weightMsgChallengeContent int
	simState.AppParams.GetOrGenerate(opWeightMsgChallengeContent, &weightMsgChallengeContent, nil,
		func(_ *rand.Rand) {
			weightMsgChallengeContent = defaultWeightMsgChallengeContent
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgChallengeContent,
		repsimulation.SimulateMsgChallengeContent(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRespondToContentChallenge          = "op_weight_msg_rep"
		defaultWeightMsgRespondToContentChallenge int = 50
	)

	var weightMsgRespondToContentChallenge int
	simState.AppParams.GetOrGenerate(opWeightMsgRespondToContentChallenge, &weightMsgRespondToContentChallenge, nil,
		func(_ *rand.Rand) {
			weightMsgRespondToContentChallenge = defaultWeightMsgRespondToContentChallenge
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRespondToContentChallenge,
		repsimulation.SimulateMsgRespondToContentChallenge(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRegisterZkPublicKey          = "op_weight_msg_rep"
		defaultWeightMsgRegisterZkPublicKey int = 100
	)

	var weightMsgRegisterZkPublicKey int
	simState.AppParams.GetOrGenerate(opWeightMsgRegisterZkPublicKey, &weightMsgRegisterZkPublicKey, nil,
		func(_ *rand.Rand) {
			weightMsgRegisterZkPublicKey = defaultWeightMsgRegisterZkPublicKey
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRegisterZkPublicKey,
		repsimulation.SimulateMsgRegisterZkPublicKey(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgReportTag          = "op_weight_msg_rep"
		defaultWeightMsgReportTag int = 100
	)

	var weightMsgReportTag int
	simState.AppParams.GetOrGenerate(opWeightMsgReportTag, &weightMsgReportTag, nil,
		func(_ *rand.Rand) {
			weightMsgReportTag = defaultWeightMsgReportTag
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReportTag,
		repsimulation.SimulateMsgReportTag(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgResolveTagReport          = "op_weight_msg_rep"
		defaultWeightMsgResolveTagReport int = 100
	)

	var weightMsgResolveTagReport int
	simState.AppParams.GetOrGenerate(opWeightMsgResolveTagReport, &weightMsgResolveTagReport, nil,
		func(_ *rand.Rand) {
			weightMsgResolveTagReport = defaultWeightMsgResolveTagReport
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgResolveTagReport,
		repsimulation.SimulateMsgResolveTagReport(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateTagBudget          = "op_weight_msg_rep"
		defaultWeightMsgCreateTagBudget int = 100
	)

	var weightMsgCreateTagBudget int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateTagBudget, &weightMsgCreateTagBudget, nil,
		func(_ *rand.Rand) {
			weightMsgCreateTagBudget = defaultWeightMsgCreateTagBudget
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateTagBudget,
		repsimulation.SimulateMsgCreateTagBudget(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAwardFromTagBudget          = "op_weight_msg_rep"
		defaultWeightMsgAwardFromTagBudget int = 100
	)

	var weightMsgAwardFromTagBudget int
	simState.AppParams.GetOrGenerate(opWeightMsgAwardFromTagBudget, &weightMsgAwardFromTagBudget, nil,
		func(_ *rand.Rand) {
			weightMsgAwardFromTagBudget = defaultWeightMsgAwardFromTagBudget
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAwardFromTagBudget,
		repsimulation.SimulateMsgAwardFromTagBudget(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgTopUpTagBudget          = "op_weight_msg_rep"
		defaultWeightMsgTopUpTagBudget int = 100
	)

	var weightMsgTopUpTagBudget int
	simState.AppParams.GetOrGenerate(opWeightMsgTopUpTagBudget, &weightMsgTopUpTagBudget, nil,
		func(_ *rand.Rand) {
			weightMsgTopUpTagBudget = defaultWeightMsgTopUpTagBudget
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTopUpTagBudget,
		repsimulation.SimulateMsgTopUpTagBudget(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgToggleTagBudget          = "op_weight_msg_rep"
		defaultWeightMsgToggleTagBudget int = 100
	)

	var weightMsgToggleTagBudget int
	simState.AppParams.GetOrGenerate(opWeightMsgToggleTagBudget, &weightMsgToggleTagBudget, nil,
		func(_ *rand.Rand) {
			weightMsgToggleTagBudget = defaultWeightMsgToggleTagBudget
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgToggleTagBudget,
		repsimulation.SimulateMsgToggleTagBudget(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgWithdrawTagBudget          = "op_weight_msg_rep"
		defaultWeightMsgWithdrawTagBudget int = 100
	)

	var weightMsgWithdrawTagBudget int
	simState.AppParams.GetOrGenerate(opWeightMsgWithdrawTagBudget, &weightMsgWithdrawTagBudget, nil,
		func(_ *rand.Rand) {
			weightMsgWithdrawTagBudget = defaultWeightMsgWithdrawTagBudget
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgWithdrawTagBudget,
		repsimulation.SimulateMsgWithdrawTagBudget(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgReportMember          = "op_weight_msg_rep"
		defaultWeightMsgReportMember int = 100
	)
	var weightMsgReportMember int
	simState.AppParams.GetOrGenerate(opWeightMsgReportMember, &weightMsgReportMember, nil,
		func(_ *rand.Rand) {
			weightMsgReportMember = defaultWeightMsgReportMember
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReportMember,
		repsimulation.SimulateMsgReportMember(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgCosignMemberReport          = "op_weight_msg_rep"
		defaultWeightMsgCosignMemberReport int = 100
	)
	var weightMsgCosignMemberReport int
	simState.AppParams.GetOrGenerate(opWeightMsgCosignMemberReport, &weightMsgCosignMemberReport, nil,
		func(_ *rand.Rand) {
			weightMsgCosignMemberReport = defaultWeightMsgCosignMemberReport
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCosignMemberReport,
		repsimulation.SimulateMsgCosignMemberReport(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgResolveMemberReport          = "op_weight_msg_rep"
		defaultWeightMsgResolveMemberReport int = 100
	)
	var weightMsgResolveMemberReport int
	simState.AppParams.GetOrGenerate(opWeightMsgResolveMemberReport, &weightMsgResolveMemberReport, nil,
		func(_ *rand.Rand) {
			weightMsgResolveMemberReport = defaultWeightMsgResolveMemberReport
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgResolveMemberReport,
		repsimulation.SimulateMsgResolveMemberReport(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgDefendMemberReport          = "op_weight_msg_rep"
		defaultWeightMsgDefendMemberReport int = 100
	)
	var weightMsgDefendMemberReport int
	simState.AppParams.GetOrGenerate(opWeightMsgDefendMemberReport, &weightMsgDefendMemberReport, nil,
		func(_ *rand.Rand) {
			weightMsgDefendMemberReport = defaultWeightMsgDefendMemberReport
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDefendMemberReport,
		repsimulation.SimulateMsgDefendMemberReport(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgAppealGovAction          = "op_weight_msg_rep"
		defaultWeightMsgAppealGovAction int = 100
	)
	var weightMsgAppealGovAction int
	simState.AppParams.GetOrGenerate(opWeightMsgAppealGovAction, &weightMsgAppealGovAction, nil,
		func(_ *rand.Rand) {
			weightMsgAppealGovAction = defaultWeightMsgAppealGovAction
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAppealGovAction,
		repsimulation.SimulateMsgAppealGovAction(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCloseInitiative          = "op_weight_msg_rep"
		defaultWeightMsgCloseInitiative int = 100
	)

	var weightMsgCloseInitiative int
	simState.AppParams.GetOrGenerate(opWeightMsgCloseInitiative, &weightMsgCloseInitiative, nil,
		func(_ *rand.Rand) {
			weightMsgCloseInitiative = defaultWeightMsgCloseInitiative
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCloseInitiative,
		repsimulation.SimulateMsgCloseInitiative(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgFundInitiativeBounty          = "op_weight_msg_rep"
		defaultWeightMsgFundInitiativeBounty int = 100
	)

	var weightMsgFundInitiativeBounty int
	simState.AppParams.GetOrGenerate(opWeightMsgFundInitiativeBounty, &weightMsgFundInitiativeBounty, nil,
		func(_ *rand.Rand) {
			weightMsgFundInitiativeBounty = defaultWeightMsgFundInitiativeBounty
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFundInitiativeBounty,
		repsimulation.SimulateMsgFundInitiativeBounty(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgReclaimInitiativeBounty          = "op_weight_msg_rep"
		defaultWeightMsgReclaimInitiativeBounty int = 100
	)

	var weightMsgReclaimInitiativeBounty int
	simState.AppParams.GetOrGenerate(opWeightMsgReclaimInitiativeBounty, &weightMsgReclaimInitiativeBounty, nil,
		func(_ *rand.Rand) {
			weightMsgReclaimInitiativeBounty = defaultWeightMsgReclaimInitiativeBounty
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReclaimInitiativeBounty,
		repsimulation.SimulateMsgReclaimInitiativeBounty(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevokeInvitation          = "op_weight_msg_rep"
		defaultWeightMsgRevokeInvitation int = 100
	)

	var weightMsgRevokeInvitation int
	simState.AppParams.GetOrGenerate(opWeightMsgRevokeInvitation, &weightMsgRevokeInvitation, nil,
		func(_ *rand.Rand) {
			weightMsgRevokeInvitation = defaultWeightMsgRevokeInvitation
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevokeInvitation,
		repsimulation.SimulateMsgRevokeInvitation(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRequestMigration          = "op_weight_msg_rep"
		defaultWeightMsgRequestMigration int = 100
	)

	var weightMsgRequestMigration int
	simState.AppParams.GetOrGenerate(opWeightMsgRequestMigration, &weightMsgRequestMigration, nil,
		func(_ *rand.Rand) {
			weightMsgRequestMigration = defaultWeightMsgRequestMigration
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRequestMigration,
		repsimulation.SimulateMsgRequestMigration(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAcceptMigration          = "op_weight_msg_rep"
		defaultWeightMsgAcceptMigration int = 100
	)

	var weightMsgAcceptMigration int
	simState.AppParams.GetOrGenerate(opWeightMsgAcceptMigration, &weightMsgAcceptMigration, nil,
		func(_ *rand.Rand) {
			weightMsgAcceptMigration = defaultWeightMsgAcceptMigration
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAcceptMigration,
		repsimulation.SimulateMsgAcceptMigration(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCancelMigration          = "op_weight_msg_rep"
		defaultWeightMsgCancelMigration int = 100
	)

	var weightMsgCancelMigration int
	simState.AppParams.GetOrGenerate(opWeightMsgCancelMigration, &weightMsgCancelMigration, nil,
		func(_ *rand.Rand) {
			weightMsgCancelMigration = defaultWeightMsgCancelMigration
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelMigration,
		repsimulation.SimulateMsgCancelMigration(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetRecoveryCommitment          = "op_weight_msg_rep"
		defaultWeightMsgSetRecoveryCommitment int = 100
	)

	var weightMsgSetRecoveryCommitment int
	simState.AppParams.GetOrGenerate(opWeightMsgSetRecoveryCommitment, &weightMsgSetRecoveryCommitment, nil,
		func(_ *rand.Rand) {
			weightMsgSetRecoveryCommitment = defaultWeightMsgSetRecoveryCommitment
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetRecoveryCommitment,
		repsimulation.SimulateMsgSetRecoveryCommitment(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRemoveRecoveryCommitment          = "op_weight_msg_rep"
		defaultWeightMsgRemoveRecoveryCommitment int = 100
	)

	var weightMsgRemoveRecoveryCommitment int
	simState.AppParams.GetOrGenerate(opWeightMsgRemoveRecoveryCommitment, &weightMsgRemoveRecoveryCommitment, nil,
		func(_ *rand.Rand) {
			weightMsgRemoveRecoveryCommitment = defaultWeightMsgRemoveRecoveryCommitment
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRemoveRecoveryCommitment,
		repsimulation.SimulateMsgRemoveRecoveryCommitment(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCommitRecovery          = "op_weight_msg_rep"
		defaultWeightMsgCommitRecovery int = 100
	)

	var weightMsgCommitRecovery int
	simState.AppParams.GetOrGenerate(opWeightMsgCommitRecovery, &weightMsgCommitRecovery, nil,
		func(_ *rand.Rand) {
			weightMsgCommitRecovery = defaultWeightMsgCommitRecovery
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCommitRecovery,
		repsimulation.SimulateMsgCommitRecovery(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevealRecovery          = "op_weight_msg_rep"
		defaultWeightMsgRevealRecovery int = 100
	)

	var weightMsgRevealRecovery int
	simState.AppParams.GetOrGenerate(opWeightMsgRevealRecovery, &weightMsgRevealRecovery, nil,
		func(_ *rand.Rand) {
			weightMsgRevealRecovery = defaultWeightMsgRevealRecovery
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevealRecovery,
		repsimulation.SimulateMsgRevealRecovery(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgObjectMigration          = "op_weight_msg_rep"
		defaultWeightMsgObjectMigration int = 100
	)

	var weightMsgObjectMigration int
	simState.AppParams.GetOrGenerate(opWeightMsgObjectMigration, &weightMsgObjectMigration, nil,
		func(_ *rand.Rand) {
			weightMsgObjectMigration = defaultWeightMsgObjectMigration
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgObjectMigration,
		repsimulation.SimulateMsgObjectMigration(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSweepTombstone          = "op_weight_msg_rep"
		defaultWeightMsgSweepTombstone int = 100
	)

	var weightMsgSweepTombstone int
	simState.AppParams.GetOrGenerate(opWeightMsgSweepTombstone, &weightMsgSweepTombstone, nil,
		func(_ *rand.Rand) {
			weightMsgSweepTombstone = defaultWeightMsgSweepTombstone
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSweepTombstone,
		repsimulation.SimulateMsgSweepTombstone(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgClearZkPublicKey          = "op_weight_msg_rep"
		defaultWeightMsgClearZkPublicKey int = 5
	)

	var weightMsgClearZkPublicKey int
	simState.AppParams.GetOrGenerate(opWeightMsgClearZkPublicKey, &weightMsgClearZkPublicKey, nil,
		func(_ *rand.Rand) {
			weightMsgClearZkPublicKey = defaultWeightMsgClearZkPublicKey
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgClearZkPublicKey,
		repsimulation.SimulateMsgClearZkPublicKey(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCompleteProject          = "op_weight_msg_rep"
		defaultWeightMsgCompleteProject int = 100
	)

	var weightMsgCompleteProject int
	simState.AppParams.GetOrGenerate(opWeightMsgCompleteProject, &weightMsgCompleteProject, nil,
		func(_ *rand.Rand) {
			weightMsgCompleteProject = defaultWeightMsgCompleteProject
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCompleteProject,
		repsimulation.SimulateMsgCompleteProject(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgInviteExpertWitness          = "op_weight_msg_rep"
		defaultWeightMsgInviteExpertWitness int = 100
	)

	var weightMsgInviteExpertWitness int
	simState.AppParams.GetOrGenerate(opWeightMsgInviteExpertWitness, &weightMsgInviteExpertWitness, nil,
		func(_ *rand.Rand) {
			weightMsgInviteExpertWitness = defaultWeightMsgInviteExpertWitness
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgInviteExpertWitness,
		repsimulation.SimulateMsgInviteExpertWitness(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

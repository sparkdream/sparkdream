package season

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	seasonsimulation "sparkdream/x/season/simulation"
	"sparkdream/x/season/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}

	// Create initial Season (required by x/rep and other modules)
	initialSeason := &types.Season{
		Number:               1,
		Name:                 "Genesis Season",
		Theme:                "Beginning",
		StartBlock:           1,
		EndBlock:             1000000, // Far future for simulation
		Status:               types.SeasonStatus_SEASON_STATUS_ACTIVE,
		ExtensionsCount:      0,
		TotalExtensionEpochs: 0,
		OriginalEndBlock:     1000000,
	}

	seasonGenesis := types.GenesisState{
		Params: types.DefaultParams(),
		Season: initialSeason,
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&seasonGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgSetDisplayName          = "op_weight_msg_season"
		defaultWeightMsgSetDisplayName int = 100
	)

	var weightMsgSetDisplayName int
	simState.AppParams.GetOrGenerate(opWeightMsgSetDisplayName, &weightMsgSetDisplayName, nil,
		func(_ *rand.Rand) {
			weightMsgSetDisplayName = defaultWeightMsgSetDisplayName
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetDisplayName,
		seasonsimulation.SimulateMsgSetDisplayName(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetUsername          = "op_weight_msg_season"
		defaultWeightMsgSetUsername int = 100
	)

	var weightMsgSetUsername int
	simState.AppParams.GetOrGenerate(opWeightMsgSetUsername, &weightMsgSetUsername, nil,
		func(_ *rand.Rand) {
			weightMsgSetUsername = defaultWeightMsgSetUsername
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetUsername,
		seasonsimulation.SimulateMsgSetUsername(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetDisplayTitle          = "op_weight_msg_season"
		defaultWeightMsgSetDisplayTitle int = 100
	)

	var weightMsgSetDisplayTitle int
	simState.AppParams.GetOrGenerate(opWeightMsgSetDisplayTitle, &weightMsgSetDisplayTitle, nil,
		func(_ *rand.Rand) {
			weightMsgSetDisplayTitle = defaultWeightMsgSetDisplayTitle
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetDisplayTitle,
		seasonsimulation.SimulateMsgSetDisplayTitle(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateGuild          = "op_weight_msg_season"
		defaultWeightMsgCreateGuild int = 100
	)

	var weightMsgCreateGuild int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateGuild, &weightMsgCreateGuild, nil,
		func(_ *rand.Rand) {
			weightMsgCreateGuild = defaultWeightMsgCreateGuild
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateGuild,
		seasonsimulation.SimulateMsgCreateGuild(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgJoinGuild          = "op_weight_msg_season"
		defaultWeightMsgJoinGuild int = 100
	)

	var weightMsgJoinGuild int
	simState.AppParams.GetOrGenerate(opWeightMsgJoinGuild, &weightMsgJoinGuild, nil,
		func(_ *rand.Rand) {
			weightMsgJoinGuild = defaultWeightMsgJoinGuild
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgJoinGuild,
		seasonsimulation.SimulateMsgJoinGuild(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgLeaveGuild          = "op_weight_msg_season"
		defaultWeightMsgLeaveGuild int = 100
	)

	var weightMsgLeaveGuild int
	simState.AppParams.GetOrGenerate(opWeightMsgLeaveGuild, &weightMsgLeaveGuild, nil,
		func(_ *rand.Rand) {
			weightMsgLeaveGuild = defaultWeightMsgLeaveGuild
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgLeaveGuild,
		seasonsimulation.SimulateMsgLeaveGuild(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgTransferGuildFounder          = "op_weight_msg_season"
		defaultWeightMsgTransferGuildFounder int = 100
	)

	var weightMsgTransferGuildFounder int
	simState.AppParams.GetOrGenerate(opWeightMsgTransferGuildFounder, &weightMsgTransferGuildFounder, nil,
		func(_ *rand.Rand) {
			weightMsgTransferGuildFounder = defaultWeightMsgTransferGuildFounder
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgTransferGuildFounder,
		seasonsimulation.SimulateMsgTransferGuildFounder(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDissolveGuild          = "op_weight_msg_season"
		defaultWeightMsgDissolveGuild int = 100
	)

	var weightMsgDissolveGuild int
	simState.AppParams.GetOrGenerate(opWeightMsgDissolveGuild, &weightMsgDissolveGuild, nil,
		func(_ *rand.Rand) {
			weightMsgDissolveGuild = defaultWeightMsgDissolveGuild
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDissolveGuild,
		seasonsimulation.SimulateMsgDissolveGuild(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgPromoteToOfficer          = "op_weight_msg_season"
		defaultWeightMsgPromoteToOfficer int = 100
	)

	var weightMsgPromoteToOfficer int
	simState.AppParams.GetOrGenerate(opWeightMsgPromoteToOfficer, &weightMsgPromoteToOfficer, nil,
		func(_ *rand.Rand) {
			weightMsgPromoteToOfficer = defaultWeightMsgPromoteToOfficer
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPromoteToOfficer,
		seasonsimulation.SimulateMsgPromoteToOfficer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDemoteOfficer          = "op_weight_msg_season"
		defaultWeightMsgDemoteOfficer int = 100
	)

	var weightMsgDemoteOfficer int
	simState.AppParams.GetOrGenerate(opWeightMsgDemoteOfficer, &weightMsgDemoteOfficer, nil,
		func(_ *rand.Rand) {
			weightMsgDemoteOfficer = defaultWeightMsgDemoteOfficer
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDemoteOfficer,
		seasonsimulation.SimulateMsgDemoteOfficer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgInviteToGuild          = "op_weight_msg_season"
		defaultWeightMsgInviteToGuild int = 100
	)

	var weightMsgInviteToGuild int
	simState.AppParams.GetOrGenerate(opWeightMsgInviteToGuild, &weightMsgInviteToGuild, nil,
		func(_ *rand.Rand) {
			weightMsgInviteToGuild = defaultWeightMsgInviteToGuild
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgInviteToGuild,
		seasonsimulation.SimulateMsgInviteToGuild(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAcceptGuildInvite          = "op_weight_msg_season"
		defaultWeightMsgAcceptGuildInvite int = 100
	)

	var weightMsgAcceptGuildInvite int
	simState.AppParams.GetOrGenerate(opWeightMsgAcceptGuildInvite, &weightMsgAcceptGuildInvite, nil,
		func(_ *rand.Rand) {
			weightMsgAcceptGuildInvite = defaultWeightMsgAcceptGuildInvite
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAcceptGuildInvite,
		seasonsimulation.SimulateMsgAcceptGuildInvite(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevokeGuildInvite          = "op_weight_msg_season"
		defaultWeightMsgRevokeGuildInvite int = 100
	)

	var weightMsgRevokeGuildInvite int
	simState.AppParams.GetOrGenerate(opWeightMsgRevokeGuildInvite, &weightMsgRevokeGuildInvite, nil,
		func(_ *rand.Rand) {
			weightMsgRevokeGuildInvite = defaultWeightMsgRevokeGuildInvite
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevokeGuildInvite,
		seasonsimulation.SimulateMsgRevokeGuildInvite(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetGuildInviteOnly          = "op_weight_msg_season"
		defaultWeightMsgSetGuildInviteOnly int = 100
	)

	var weightMsgSetGuildInviteOnly int
	simState.AppParams.GetOrGenerate(opWeightMsgSetGuildInviteOnly, &weightMsgSetGuildInviteOnly, nil,
		func(_ *rand.Rand) {
			weightMsgSetGuildInviteOnly = defaultWeightMsgSetGuildInviteOnly
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetGuildInviteOnly,
		seasonsimulation.SimulateMsgSetGuildInviteOnly(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgUpdateGuildDescription          = "op_weight_msg_season"
		defaultWeightMsgUpdateGuildDescription int = 100
	)

	var weightMsgUpdateGuildDescription int
	simState.AppParams.GetOrGenerate(opWeightMsgUpdateGuildDescription, &weightMsgUpdateGuildDescription, nil,
		func(_ *rand.Rand) {
			weightMsgUpdateGuildDescription = defaultWeightMsgUpdateGuildDescription
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateGuildDescription,
		seasonsimulation.SimulateMsgUpdateGuildDescription(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgKickFromGuild          = "op_weight_msg_season"
		defaultWeightMsgKickFromGuild int = 100
	)

	var weightMsgKickFromGuild int
	simState.AppParams.GetOrGenerate(opWeightMsgKickFromGuild, &weightMsgKickFromGuild, nil,
		func(_ *rand.Rand) {
			weightMsgKickFromGuild = defaultWeightMsgKickFromGuild
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgKickFromGuild,
		seasonsimulation.SimulateMsgKickFromGuild(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgClaimGuildFounder          = "op_weight_msg_season"
		defaultWeightMsgClaimGuildFounder int = 100
	)

	var weightMsgClaimGuildFounder int
	simState.AppParams.GetOrGenerate(opWeightMsgClaimGuildFounder, &weightMsgClaimGuildFounder, nil,
		func(_ *rand.Rand) {
			weightMsgClaimGuildFounder = defaultWeightMsgClaimGuildFounder
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgClaimGuildFounder,
		seasonsimulation.SimulateMsgClaimGuildFounder(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgStartQuest          = "op_weight_msg_season"
		defaultWeightMsgStartQuest int = 100
	)

	var weightMsgStartQuest int
	simState.AppParams.GetOrGenerate(opWeightMsgStartQuest, &weightMsgStartQuest, nil,
		func(_ *rand.Rand) {
			weightMsgStartQuest = defaultWeightMsgStartQuest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgStartQuest,
		seasonsimulation.SimulateMsgStartQuest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgClaimQuestReward          = "op_weight_msg_season"
		defaultWeightMsgClaimQuestReward int = 100
	)

	var weightMsgClaimQuestReward int
	simState.AppParams.GetOrGenerate(opWeightMsgClaimQuestReward, &weightMsgClaimQuestReward, nil,
		func(_ *rand.Rand) {
			weightMsgClaimQuestReward = defaultWeightMsgClaimQuestReward
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgClaimQuestReward,
		seasonsimulation.SimulateMsgClaimQuestReward(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAbandonQuest          = "op_weight_msg_season"
		defaultWeightMsgAbandonQuest int = 100
	)

	var weightMsgAbandonQuest int
	simState.AppParams.GetOrGenerate(opWeightMsgAbandonQuest, &weightMsgAbandonQuest, nil,
		func(_ *rand.Rand) {
			weightMsgAbandonQuest = defaultWeightMsgAbandonQuest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAbandonQuest,
		seasonsimulation.SimulateMsgAbandonQuest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgCreateQuest          = "op_weight_msg_season"
		defaultWeightMsgCreateQuest int = 100
	)

	var weightMsgCreateQuest int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateQuest, &weightMsgCreateQuest, nil,
		func(_ *rand.Rand) {
			weightMsgCreateQuest = defaultWeightMsgCreateQuest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateQuest,
		seasonsimulation.SimulateMsgCreateQuest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgDeactivateQuest          = "op_weight_msg_season"
		defaultWeightMsgDeactivateQuest int = 100
	)

	var weightMsgDeactivateQuest int
	simState.AppParams.GetOrGenerate(opWeightMsgDeactivateQuest, &weightMsgDeactivateQuest, nil,
		func(_ *rand.Rand) {
			weightMsgDeactivateQuest = defaultWeightMsgDeactivateQuest
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeactivateQuest,
		seasonsimulation.SimulateMsgDeactivateQuest(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgExtendSeason          = "op_weight_msg_season"
		defaultWeightMsgExtendSeason int = 100
	)

	var weightMsgExtendSeason int
	simState.AppParams.GetOrGenerate(opWeightMsgExtendSeason, &weightMsgExtendSeason, nil,
		func(_ *rand.Rand) {
			weightMsgExtendSeason = defaultWeightMsgExtendSeason
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgExtendSeason,
		seasonsimulation.SimulateMsgExtendSeason(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSetNextSeasonInfo          = "op_weight_msg_season"
		defaultWeightMsgSetNextSeasonInfo int = 100
	)

	var weightMsgSetNextSeasonInfo int
	simState.AppParams.GetOrGenerate(opWeightMsgSetNextSeasonInfo, &weightMsgSetNextSeasonInfo, nil,
		func(_ *rand.Rand) {
			weightMsgSetNextSeasonInfo = defaultWeightMsgSetNextSeasonInfo
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetNextSeasonInfo,
		seasonsimulation.SimulateMsgSetNextSeasonInfo(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAbortSeasonTransition          = "op_weight_msg_season"
		defaultWeightMsgAbortSeasonTransition int = 100
	)

	var weightMsgAbortSeasonTransition int
	simState.AppParams.GetOrGenerate(opWeightMsgAbortSeasonTransition, &weightMsgAbortSeasonTransition, nil,
		func(_ *rand.Rand) {
			weightMsgAbortSeasonTransition = defaultWeightMsgAbortSeasonTransition
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAbortSeasonTransition,
		seasonsimulation.SimulateMsgAbortSeasonTransition(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRetrySeasonTransition          = "op_weight_msg_season"
		defaultWeightMsgRetrySeasonTransition int = 100
	)

	var weightMsgRetrySeasonTransition int
	simState.AppParams.GetOrGenerate(opWeightMsgRetrySeasonTransition, &weightMsgRetrySeasonTransition, nil,
		func(_ *rand.Rand) {
			weightMsgRetrySeasonTransition = defaultWeightMsgRetrySeasonTransition
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRetrySeasonTransition,
		seasonsimulation.SimulateMsgRetrySeasonTransition(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgSkipTransitionPhase          = "op_weight_msg_season"
		defaultWeightMsgSkipTransitionPhase int = 100
	)

	var weightMsgSkipTransitionPhase int
	simState.AppParams.GetOrGenerate(opWeightMsgSkipTransitionPhase, &weightMsgSkipTransitionPhase, nil,
		func(_ *rand.Rand) {
			weightMsgSkipTransitionPhase = defaultWeightMsgSkipTransitionPhase
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSkipTransitionPhase,
		seasonsimulation.SimulateMsgSkipTransitionPhase(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgReportDisplayName          = "op_weight_msg_season"
		defaultWeightMsgReportDisplayName int = 100
	)

	var weightMsgReportDisplayName int
	simState.AppParams.GetOrGenerate(opWeightMsgReportDisplayName, &weightMsgReportDisplayName, nil,
		func(_ *rand.Rand) {
			weightMsgReportDisplayName = defaultWeightMsgReportDisplayName
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgReportDisplayName,
		seasonsimulation.SimulateMsgReportDisplayName(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgAppealDisplayNameModeration          = "op_weight_msg_season"
		defaultWeightMsgAppealDisplayNameModeration int = 100
	)

	var weightMsgAppealDisplayNameModeration int
	simState.AppParams.GetOrGenerate(opWeightMsgAppealDisplayNameModeration, &weightMsgAppealDisplayNameModeration, nil,
		func(_ *rand.Rand) {
			weightMsgAppealDisplayNameModeration = defaultWeightMsgAppealDisplayNameModeration
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAppealDisplayNameModeration,
		seasonsimulation.SimulateMsgAppealDisplayNameModeration(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

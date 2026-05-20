package session

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	sessionsimulation "sparkdream/x/session/simulation"
	"sparkdream/x/session/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	sessionGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&sessionGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the all the gov module operations with their respective weights.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)
	const (
		opWeightMsgCreateSession          = "op_weight_msg_session"
		defaultWeightMsgCreateSession int = 100
	)

	var weightMsgCreateSession int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateSession, &weightMsgCreateSession, nil,
		func(_ *rand.Rand) {
			weightMsgCreateSession = defaultWeightMsgCreateSession
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateSession,
		sessionsimulation.SimulateMsgCreateSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgRevokeSession          = "op_weight_msg_session"
		defaultWeightMsgRevokeSession int = 100
	)

	var weightMsgRevokeSession int
	simState.AppParams.GetOrGenerate(opWeightMsgRevokeSession, &weightMsgRevokeSession, nil,
		func(_ *rand.Rand) {
			weightMsgRevokeSession = defaultWeightMsgRevokeSession
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevokeSession,
		sessionsimulation.SimulateMsgRevokeSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))
	const (
		opWeightMsgExecSession          = "op_weight_msg_session"
		defaultWeightMsgExecSession int = 100
	)

	var weightMsgExecSession int
	simState.AppParams.GetOrGenerate(opWeightMsgExecSession, &weightMsgExecSession, nil,
		func(_ *rand.Rand) {
			weightMsgExecSession = defaultWeightMsgExecSession
		},
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgExecSession,
		sessionsimulation.SimulateMsgExecSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	// --- Unified grant-registry operations (P3-P8) ---
	// Each registration follows the same shape: a single `op_weight_msg_session`
	// label (the param-shaping is unique-per-handler; the shared label is just
	// a default-weight bucket) plus a non-zero default weight. Default weights
	// are tuned so creation outpaces destruction (Create=80, Claim/Pull=60,
	// Revoke/Decline=20, Retry=10 — Retry only fires on a paused oneshot,
	// which is rare in random sims).

	const (
		opWeightMsgCreateGrant          = "op_weight_msg_session"
		defaultWeightMsgCreateGrant int = 80
	)
	var weightMsgCreateGrant int
	simState.AppParams.GetOrGenerate(opWeightMsgCreateGrant, &weightMsgCreateGrant, nil,
		func(_ *rand.Rand) { weightMsgCreateGrant = defaultWeightMsgCreateGrant },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateGrant,
		sessionsimulation.SimulateMsgCreateGrant(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgClaimRecurringPull          = "op_weight_msg_session"
		defaultWeightMsgClaimRecurringPull int = 60
	)
	var weightMsgClaimRecurringPull int
	simState.AppParams.GetOrGenerate(opWeightMsgClaimRecurringPull, &weightMsgClaimRecurringPull, nil,
		func(_ *rand.Rand) { weightMsgClaimRecurringPull = defaultWeightMsgClaimRecurringPull },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgClaimRecurringPull,
		sessionsimulation.SimulateMsgClaimRecurringPull(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgPullAllowance          = "op_weight_msg_session"
		defaultWeightMsgPullAllowance int = 60
	)
	var weightMsgPullAllowance int
	simState.AppParams.GetOrGenerate(opWeightMsgPullAllowance, &weightMsgPullAllowance, nil,
		func(_ *rand.Rand) { weightMsgPullAllowance = defaultWeightMsgPullAllowance },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPullAllowance,
		sessionsimulation.SimulateMsgPullAllowance(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgRevokeGrant          = "op_weight_msg_session"
		defaultWeightMsgRevokeGrant int = 20
	)
	var weightMsgRevokeGrant int
	simState.AppParams.GetOrGenerate(opWeightMsgRevokeGrant, &weightMsgRevokeGrant, nil,
		func(_ *rand.Rand) { weightMsgRevokeGrant = defaultWeightMsgRevokeGrant },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRevokeGrant,
		sessionsimulation.SimulateMsgRevokeGrant(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgDeclineGrant          = "op_weight_msg_session"
		defaultWeightMsgDeclineGrant int = 20
	)
	var weightMsgDeclineGrant int
	simState.AppParams.GetOrGenerate(opWeightMsgDeclineGrant, &weightMsgDeclineGrant, nil,
		func(_ *rand.Rand) { weightMsgDeclineGrant = defaultWeightMsgDeclineGrant },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeclineGrant,
		sessionsimulation.SimulateMsgDeclineGrant(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	const (
		opWeightMsgRetryScheduledOneshot          = "op_weight_msg_session"
		defaultWeightMsgRetryScheduledOneshot int = 10
	)
	var weightMsgRetryScheduledOneshot int
	simState.AppParams.GetOrGenerate(opWeightMsgRetryScheduledOneshot, &weightMsgRetryScheduledOneshot, nil,
		func(_ *rand.Rand) { weightMsgRetryScheduledOneshot = defaultWeightMsgRetryScheduledOneshot },
	)
	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRetryScheduledOneshot,
		sessionsimulation.SimulateMsgRetryScheduledOneshot(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig),
	))

	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

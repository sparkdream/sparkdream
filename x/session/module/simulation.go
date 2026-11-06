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

// WeightedOperations returns the module's simulation operations with their
// weights. Each weight has its own app-params key (op_weight_msg_<name>),
// so a -Params file can weight the operations one by one.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	type opDef struct {
		key    string
		weight int
		op     simtypes.Operation
	}
	defs := []opDef{
		{"op_weight_msg_create_session", 40, sessionsimulation.SimulateMsgCreateSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_revoke_session", 15, sessionsimulation.SimulateMsgRevokeSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_exec_session", 60, sessionsimulation.SimulateMsgExecSession(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_create_grant", 80, sessionsimulation.SimulateMsgCreateGrant(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_claim_recurring_pull", 50, sessionsimulation.SimulateMsgClaimRecurringPull(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_pull_allowance", 50, sessionsimulation.SimulateMsgPullAllowance(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_revoke_grant", 20, sessionsimulation.SimulateMsgRevokeGrant(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_decline_grant", 20, sessionsimulation.SimulateMsgDeclineGrant(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_retry_scheduled_oneshot", 25, sessionsimulation.SimulateMsgRetryScheduledOneshot(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_accept_grant", 30, sessionsimulation.SimulateMsgAcceptGrant(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_disallow_granter", 10, sessionsimulation.SimulateMsgDisallowGranter(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
	}
	operations := make([]simtypes.WeightedOperation, 0, len(defs))
	for _, d := range defs {
		weight := d.weight
		simState.AppParams.GetOrGenerate(d.key, &weight, nil, func(_ *rand.Rand) { weight = d.weight })
		operations = append(operations, simulation.NewWeightedOperation(weight, d.op))
	}
	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

package service

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	servicesimulation "sparkdream/x/service/simulation"
	"sparkdream/x/service/types"
)

// simMinBondUspark is the min_bond of every service type in the simulated
// genesis: 10 SPARK.
const simMinBondUspark = int64(10_000_000)

// Simulated-genesis windows, in blocks. Production windows run to days and
// months; these let a 200-block simulation reach every timed path: claims
// after unbonding and partial withdrawals, slashes contested inside the
// contest window, reports that time out, juries that must vote before
// their deadline (max_escalated_blocks / 2), cooldowns that lapse.
const (
	simUnbondingBlocks        = int64(30)
	simContestWindowBlocks    = int64(20)
	simMaxPendingBlocks       = int64(60)
	simMaxEscalatedBlocks     = int64(120)
	simRefileCooldownBlocks   = int64(60)
	simTier1CooldownBlocks    = int64(25)
	simTier1WindowBlocks      = int64(100)
	simUnderfundedGraceBlocks = int64(40)
	simReporterWindowBlocks   = int64(60)
	simCheckpointLagBlocks    = int64(80)
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	// Seed the service types DefaultGenesis seeds (federation bridges and the
	// content scanner), so operators and bridges can register. Their
	// production min_bond (1000 SPARK for a bridge) is lowered to an amount
	// every sim account holds: the random initial stake is at least 1 SPARK
	// and almost always far more. Nothing is drawn from simState.Rand, so
	// the genesis of the modules generated after this one is not shifted.
	serviceGenesis := *types.DefaultGenesis()
	p := &serviceGenesis.Params
	p.DefaultUnbondingPeriodBlocks = simUnbondingBlocks
	p.ReportContestWindowBlocks = simContestWindowBlocks
	p.MaxPendingBlocks = simMaxPendingBlocks
	p.MaxEscalatedBlocks = simMaxEscalatedBlocks
	p.ReportRefileCooldownBlocks = simRefileCooldownBlocks
	p.DefaultTier1CooldownBlocks = simTier1CooldownBlocks
	p.DefaultTier1WindowBlocks = simTier1WindowBlocks
	p.DefaultUnderfundedGraceBlocks = simUnderfundedGraceBlocks
	p.ReporterRateLimitWindowBlocks = simReporterWindowBlocks
	for i := range serviceGenesis.ServiceTypes {
		cfg := &serviceGenesis.ServiceTypes[i]
		cfg.MinBondAmount = math.NewInt(simMinBondUspark)
		cfg.UnbondingPeriodBlocks = simUnbondingBlocks
		cfg.Tier1CooldownBlocks = simTier1CooldownBlocks
		cfg.Tier1WindowBlocks = simTier1WindowBlocks
		cfg.UnderfundedGraceBlocks = simUnderfundedGraceBlocks
		if cfg.CheckpointMaxLagBlocks > 0 {
			cfg.CheckpointMaxLagBlocks = simCheckpointLagBlocks
		}
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&serviceGenesis)
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
		{"op_weight_msg_register_operator", 60, servicesimulation.SimulateMsgRegisterOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_update_metadata", 25, servicesimulation.SimulateMsgUpdateMetadata(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_unbond_operator", 20, servicesimulation.SimulateMsgUnbondOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_claim_unbonded_bond", 40, servicesimulation.SimulateMsgClaimUnbondedBond(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_top_up_bond", 30, servicesimulation.SimulateMsgTopUpBond(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_report_operator", 40, servicesimulation.SimulateMsgReportOperator(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_resolve_report", 60, servicesimulation.SimulateMsgResolveReport(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_contest_slash", 40, servicesimulation.SimulateMsgContestSlash(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_resolve_report_by_jury", 60, servicesimulation.SimulateMsgResolveReportByJury(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_open_controller_transfer_case", 25, servicesimulation.SimulateMsgOpenControllerTransferCase(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_finalize_controller_transfer", 60, servicesimulation.SimulateMsgFinalizeControllerTransfer(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_submit_checkpoint", 40, servicesimulation.SimulateMsgSubmitCheckpoint(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_withdraw_bond", 30, servicesimulation.SimulateMsgWithdrawBond(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
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

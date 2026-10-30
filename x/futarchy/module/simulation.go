package futarchy

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	futarchysimulation "sparkdream/x/futarchy/simulation"
	"sparkdream/x/futarchy/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	accs := make([]string, len(simState.Accounts))
	for i, acc := range simState.Accounts {
		accs[i] = acc.Address.String()
	}
	futarchyGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&futarchyGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the module's simulation operations with their
// weights. Each weight has its own app-params key (op_weight_msg_<name>),
// so a -Params file can weight the operations one by one. MsgCancelMarket
// and MsgUpdateParams are signed by the x/gov authority alone and have no
// operation.
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	type opDef struct {
		key    string
		weight int
		op     simtypes.Operation
	}
	defs := []opDef{
		{"op_weight_msg_create_market", 40, futarchysimulation.SimulateMsgCreateMarket(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_trade", 100, futarchysimulation.SimulateMsgTrade(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_futarchy_redeem", 60, futarchysimulation.SimulateMsgRedeem(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_withdraw_liquidity", 10, futarchysimulation.SimulateMsgWithdrawLiquidity(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
		{"op_weight_msg_futarchy_update_operational_params", 15, futarchysimulation.SimulateMsgUpdateOperationalParams(am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig)},
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

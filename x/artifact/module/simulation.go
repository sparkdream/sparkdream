package artifact

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	artifactsimulation "sparkdream/x/artifact/simulation"
	"sparkdream/x/artifact/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	artifactGenesis := types.GenesisState{
		Params: types.DefaultParams(),
	}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&artifactGenesis)
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// simOp pairs an operation weight key with its default weight and factory.
type simOp struct {
	key    string
	weight int
	op     func(am AppModule, simState module.SimulationState) simtypes.Operation
}

var simOps = []simOp{
	{"op_weight_msg_artifact_create_class", 20, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgCreateClass(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_mint", 60, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgMint(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_public_mint", 30, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgPublicMint(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_transfer", 50, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgTransfer(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_accept_incoming", 30, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgAcceptIncoming(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_reject_incoming", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgRejectIncoming(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_set_receive_policy", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgSetReceivePolicy(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_burn", 15, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgBurn(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_list", 30, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgList(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_delist", 10, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgDelist(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
	{"op_weight_msg_artifact_buy", 25, func(am AppModule, s module.SimulationState) simtypes.Operation {
		return artifactsimulation.SimulateMsgBuy(am.authKeeper, am.bankKeeper, am.keeper, s.TxConfig)
	}},
}

// WeightedOperations returns the module operations with their respective
// weights. Class-management, moderation and params messages are exercised
// by unit and e2e tests instead (they need council or sentinel actors the
// simulator does not create).
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0, len(simOps))
	for _, o := range simOps {
		weight := o.weight
		simState.AppParams.GetOrGenerate(o.key, &weight, nil, func(_ *rand.Rand) { weight = o.weight })
		operations = append(operations, simulation.NewWeightedOperation(weight, o.op(am, simState)))
	}
	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

package name

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"time"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commontypes "sparkdream/x/common/types"
	namesimulation "sparkdream/x/name/simulation"
	"sparkdream/x/name/types"
)

// GenerateGenesisState creates a randomized GenState of the module.
//
// The simulation shortens two clocks so a 200-block run reaches them (the
// SDK simulator advances block time by 5000-10000 seconds a block): the
// owner's dispute contest window (dispute_timeout_blocks, which the
// BeginBlocker auto-resolution and the upheld resolutions need closed) and
// the owner inactivity window (expiration_duration, which scavenging
// needs). Both sit below the compiled operational-param floors, which bind
// MsgUpdateOperationalParams only; genesis validation accepts them.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	r := simGenesisRand(simState, types.ModuleName)
	params := types.DefaultParams()
	params.DisputeTimeoutBlocks = commontypes.Saturate[uint64](15 + r.Intn(46)) // 15-60 blocks
	params.ExpirationDuration = time.Duration(3+r.Intn(10)) * 24 * time.Hour    // 3-12 days
	nameGenesis := types.GenesisState{Params: params}
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&nameGenesis)
}

// simGenesisRand returns a source for the simulated genesis seeded from the
// sim accounts (themselves drawn from the simulation seed) and tag. It never
// draws from simState.Rand, so the genesis of the modules generated after
// this one, and the whole run that follows, is not shifted by it.
func simGenesisRand(simState *module.SimulationState, tag string) *rand.Rand {
	h := sha256.New()
	h.Write([]byte(tag))
	for _, acc := range simState.Accounts {
		h.Write(acc.Address)
	}
	return rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(h.Sum(nil)[:8]) >> 1))) //nolint:gosec // simulation only
}

// RegisterStoreDecoder registers a decoder.
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// WeightedOperations returns the module's simulation operations with their
// weights. Every op delivers real transactions (x/name/simulation).
func (am AppModule) WeightedOperations(simState module.SimulationState) []simtypes.WeightedOperation {
	ak, bk, k, txGen := am.authKeeper, am.bankKeeper, am.keeper, simState.TxConfig
	ops := []struct {
		key    string
		weight int
		op     simtypes.Operation
	}{
		{"op_weight_msg_register_name", 60, namesimulation.SimulateMsgRegisterName(ak, bk, k, txGen)},
		{"op_weight_msg_set_primary", 30, namesimulation.SimulateMsgSetPrimary(ak, bk, k, txGen)},
		{"op_weight_msg_update_name", 30, namesimulation.SimulateMsgUpdateName(ak, bk, k, txGen)},
		{"op_weight_msg_set_display_name", 30, namesimulation.SimulateMsgSetDisplayName(ak, bk, k, txGen)},
		{"op_weight_msg_set_target", 25, namesimulation.SimulateMsgSetTarget(ak, bk, k, txGen)},
		{"op_weight_msg_accept_target", 25, namesimulation.SimulateMsgAcceptTarget(ak, bk, k, txGen)},
		{"op_weight_msg_transfer_name", 20, namesimulation.SimulateMsgTransferName(ak, bk, k, txGen)},
		{"op_weight_msg_file_dispute", 20, namesimulation.SimulateMsgFileDispute(ak, bk, k, txGen)},
		{"op_weight_msg_contest_dispute", 20, namesimulation.SimulateMsgContestDispute(ak, bk, k, txGen)},
		{"op_weight_msg_resolve_dispute", 20, namesimulation.SimulateMsgResolveDispute(ak, bk, k, txGen)},
	}
	operations := make([]simtypes.WeightedOperation, 0, len(ops))
	for _, o := range ops {
		weight := o.weight
		simState.AppParams.GetOrGenerate(o.key, &weight, nil, func(_ *rand.Rand) { weight = o.weight })
		operations = append(operations, simulation.NewWeightedOperation(weight, o.op))
	}
	return operations
}

// ProposalMsgs returns msgs used for governance proposals for simulations.
func (am AppModule) ProposalMsgs(simState module.SimulationState) []simtypes.WeightedProposalMsg {
	return []simtypes.WeightedProposalMsg{}
}

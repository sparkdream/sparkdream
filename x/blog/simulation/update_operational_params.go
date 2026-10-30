package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	commontypes "sparkdream/x/common/types"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgUpdateOperationalParams moves an x/commons proposal carrying a
// MsgUpdateOperationalParams one step (submit, vote through, execute; see
// council.go). The proposal retunes the blog's fees and daily rate limits
// within their bounds, keeping every other operational param.
func SimulateMsgUpdateOperationalParams(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUpdateOperationalParams{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, decodeOperationalParams, e.buildOperationalParams)
	})
}

// decodeOperationalParams decodes a proposal message carrying a
// MsgUpdateOperationalParams.
func decodeOperationalParams(a *codectypes.Any) (sdk.Msg, bool) {
	m := &types.MsgUpdateOperationalParams{}
	if a == nil || m.Unmarshal(a.Value) != nil {
		return nil, false
	}
	return m, true
}

// buildOperationalParams builds a MsgUpdateOperationalParams signed by
// policy, unless an open proposal already carries one.
func (e *simEnv) buildOperationalParams(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
	if len(taken) > 0 {
		return nil, "an operational params proposal is already open", nil
	}
	op := e.params.ExtractOperationalParams()
	op.CostPerByteAmount = randAmount(e, 50, 200, e.params.MaxCostPerByteAmount)
	op.ReactionFeeAmount = randAmount(e, 20, 100, e.params.MaxReactionFeeAmount)
	op.MaxPostsPerDay = commontypes.Saturate[uint32](5 + e.r.Intn(16))       // 5-20
	op.MaxRepliesPerDay = commontypes.Saturate[uint32](20 + e.r.Intn(61))    // 20-80
	op.MaxReactionsPerDay = commontypes.Saturate[uint32](50 + e.r.Intn(101)) // 50-150
	op.MaxPinsPerDay = commontypes.Saturate[uint32](10 + e.r.Intn(21))       // 10-30
	return &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: op}, "", nil
}

// randAmount draws an amount in [lo, hi], capped at ceiling when set.
func randAmount(e *simEnv, lo, hi int64, ceiling math.Int) math.Int {
	amt := math.NewInt(lo + e.r.Int63n(hi-lo+1))
	if !ceiling.IsNil() && ceiling.IsPositive() && amt.GT(ceiling) {
		return ceiling
	}
	return amt
}

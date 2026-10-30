package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	commonskeeper "sparkdream/x/commons/keeper"
	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
)

// opParamsBodies is the body list of SimulateMsgUpdateOperationalParams: the
// bodies whose policies x/futarchy accepts (commons / operations).
var opParamsBodies = []string{"Commons Operations Committee", "Commons Council"}

// SimulateMsgUpdateOperationalParams moves a proposal carrying a futarchy
// MsgUpdateOperationalParams one step (council.go). The new values keep
// max_duration and max_redemption_delay at or above what the x/commons
// confidence-vote markets need, so the module keeps creating them.
func SimulateMsgUpdateOperationalParams(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	label := &types.MsgUpdateOperationalParams{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			bodies = opParamsBodies
		}
		decode := func(a *codectypes.Any) (sdk.Msg, bool) {
			var m types.MsgUpdateOperationalParams
			if err := m.Unmarshal(a.Value); err != nil {
				return nil, false
			}
			return &m, true
		}
		build := func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			if len(taken) > 0 {
				return nil, "a params proposal is already open", nil
			}
			op := types.FutarchyOperationalParams{
				TradingFeeBps:      commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](types.MaxTradingFeeBps) + 1)),
				MaxDuration:        commonskeeper.GovernanceMarketDurationBlocks + e.r.Int63n(types.DefaultMaxDuration),
				MaxRedemptionDelay: commonskeeper.GovernanceMarketRedemptionBlocks + e.r.Int63n(types.DefaultMaxRedemptionDelay),
			}
			return &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: op}, "", nil
		}
		return e.advance(label, bodies, decode, build)
	})
}

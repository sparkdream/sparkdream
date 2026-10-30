package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgUpdateOperationalParams changes one benign operational param
// through an executed x/commons proposal (council.go), one step per call;
// one proposal is open at a time. The message replaces every operational
// field, so it starts from the live params and changes one of: quest XP
// ceiling (within HardMaxQuestXpReward), max quest objectives, max pending
// guild invites, guild invite TTL, or the vote / proposal XP awards. The
// clock (epoch_blocks), season timing and transition knobs are never
// touched: x/rep's pools run on epoch_blocks, and a shorter season would end
// mid-run (a transition resets everyone's reputation).
func SimulateMsgUpdateOperationalParams(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgUpdateOperationalParams{}, decodeAs[types.MsgUpdateOperationalParams](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			if len(taken) > 0 {
				return nil, "an operational params change is already proposed", nil
			}
			params, err := e.k.Params.Get(e.ctx)
			if err != nil {
				return nil, "", err
			}
			op := params.ExtractOperationalParams()
			switch e.r.Intn(5) {
			case 0:
				op.MaxQuestXpReward = 50 + commontypes.Saturate[uint64](e.r.Int63n(commontypes.Saturate[int64](types.HardMaxQuestXpReward-50)+1))
			case 1:
				op.MaxQuestObjectives = commontypes.Saturate[uint32](3 + e.r.Intn(8))
			case 2:
				op.MaxPendingInvites = commontypes.Saturate[uint32](5 + e.r.Intn(16))
			case 3:
				op.GuildInviteTtlEpochs = commontypes.Saturate[uint64](7 + e.r.Intn(24))
			default:
				op.XpVoteCast = commontypes.Saturate[uint64](1 + e.r.Intn(10))
				op.XpProposalCreated = commontypes.Saturate[uint64](5 + e.r.Intn(20))
			}
			return &types.MsgUpdateOperationalParams{Authority: policy, OperationalParams: op}, "", nil
		})
}

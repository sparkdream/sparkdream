package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgResolveUnappealedModeration upholds an unappealed display-name
// moderation whose appeal window has passed through an executed x/commons
// proposal (council.go). The BeginBlocker auto-resolves such a record in
// the first block past its window, so the message is reachable only for a
// record whose automatic resolution failed (SEA-09), and within a 200-block
// run (display_name_appeal_period_blocks is about a week) normally not at
// all; the op then NoOps.
func SimulateMsgResolveUnappealedModeration(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgResolveUnappealedModeration{}, decodeAs[types.MsgResolveUnappealedModeration](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			pending := map[string]bool{}
			for _, m := range taken {
				pending[m.(*types.MsgResolveUnappealedModeration).Member] = true
			}
			params, err := e.k.Params.Get(e.ctx)
			if err != nil {
				return nil, "", err
			}
			window := commontypes.Saturate[int64](params.DisplayNameAppealPeriodBlocks)
			var members []string
			if err := e.k.DisplayNameModeration.Walk(e.ctx, nil, func(member string, m types.DisplayNameModeration) (bool, error) {
				if m.Active && m.AppealChallengeId == "" && !pending[member] && e.ctx.BlockHeight() > m.ModeratedAt+window {
					members = append(members, member)
				}
				return false, nil
			}); err != nil {
				return nil, "", err
			}
			member, ok := pick(e.r, members)
			if !ok {
				return nil, "no unappealed moderation past its appeal window", nil
			}
			return &types.MsgResolveUnappealedModeration{Authority: policy, Member: member}, "", nil
		})
}

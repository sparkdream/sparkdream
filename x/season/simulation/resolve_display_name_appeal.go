package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgResolveDisplayNameAppeal resolves an appealed display-name
// moderation (filed by SimulateMsgReportDisplayName and appealed by
// SimulateMsgAppealDisplayNameModeration) through an executed x/commons
// proposal (council.go), one step per call, with a random verdict. The
// handler's DREAM stake settlement is covered by the dry-run.
func SimulateMsgResolveDisplayNameAppeal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgResolveDisplayNameAppeal{}, decodeAs[types.MsgResolveDisplayNameAppeal](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			pending := map[string]bool{}
			for _, m := range taken {
				pending[m.(*types.MsgResolveDisplayNameAppeal).Member] = true
			}
			var members []string
			if err := e.k.DisplayNameModeration.Walk(e.ctx, nil, func(member string, m types.DisplayNameModeration) (bool, error) {
				if m.Active && m.AppealChallengeId != "" && !pending[member] {
					members = append(members, member)
				}
				return false, nil
			}); err != nil {
				return nil, "", err
			}
			member, ok := pick(e.r, members)
			if !ok {
				return nil, "no appealed display name moderation", nil
			}
			return &types.MsgResolveDisplayNameAppeal{
				Authority:       policy,
				Member:          member,
				AppealSucceeded: e.r.Intn(2) == 0,
			}, "", nil
		})
}

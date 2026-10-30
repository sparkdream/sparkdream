package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgDeleteAchievement deletes a sim-created achievement that no
// quest requires through an executed x/commons proposal (council.go), one
// step per call.
func SimulateMsgDeleteAchievement(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgDeleteAchievement{}, decodeAs[types.MsgDeleteAchievement](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			ids, err := allAchievements(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			skip := pendingIDs(taken, achievementIDOf)
			// DeleteAchievement does not check quest references; the sim
			// never strands a quest's required achievement.
			quests, err := allQuests(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			for _, q := range quests {
				skip[q.RequiredAchievement] = true
			}
			id, ok := pick(e.r, without(simOwned(ids), skip))
			if !ok {
				return nil, "no sim achievement to delete", nil
			}
			return &types.MsgDeleteAchievement{Authority: policy, AchievementId: id}, "", nil
		})
}

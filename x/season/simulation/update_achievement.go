package simulation

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgUpdateAchievement updates an achievement (genesis or sim) through
// an executed x/commons proposal (council.go), one step per call.
func SimulateMsgUpdateAchievement(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgUpdateAchievement{}, decodeAs[types.MsgUpdateAchievement](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			ids, err := allAchievements(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			id, ok := pick(e.r, without(ids, pendingIDs(taken, achievementIDOf)))
			if !ok {
				return nil, "no achievement to update", nil
			}
			a, err := e.k.Achievement.Get(e.ctx, id)
			if err != nil {
				return nil, "", err
			}
			return &types.MsgUpdateAchievement{
				Authority:            policy,
				AchievementId:        id,
				Name:                 fmt.Sprintf("%s Wayfarer", pickName(e)),
				Description:          "An updated simulation achievement",
				Rarity:               randomRarity(e),
				XpReward:             randomAchievementXP(e),
				RequirementType:      commontypes.Saturate[uint32](a.RequirementType),
				RequirementThreshold: randomThreshold(e),
			}, "", nil
		})
}

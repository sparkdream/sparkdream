package simulation

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgCreateAchievement creates a sim achievement through an
// executed x/commons proposal (council.go), one step per call, up to
// simDefMax of them.
func SimulateMsgCreateAchievement(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgCreateAchievement{}, decodeAs[types.MsgCreateAchievement](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			ids, err := allAchievements(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			if len(simOwned(ids))+len(taken) >= simDefMax {
				return nil, "enough sim achievements", nil
			}
			id, ok := newDefinitionID(e, "achievement", ids, pendingIDs(taken, achievementIDOf))
			if !ok {
				return nil, "achievement id already taken", nil
			}
			return &types.MsgCreateAchievement{
				Authority:            policy,
				AchievementId:        id,
				Name:                 fmt.Sprintf("%s Pathfinder", pickName(e)),
				Description:          "A simulation generated achievement",
				Rarity:               randomRarity(e),
				XpReward:             randomAchievementXP(e),
				RequirementType:      randomRequirementType(e),
				RequirementThreshold: randomThreshold(e),
			}, "", nil
		})
}

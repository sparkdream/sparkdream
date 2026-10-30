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

// SimulateMsgCreateQuest creates a quest definition through an executed
// x/commons proposal (council.go), one step per call. The quest is active,
// has no objectives (claimable right after it starts) and an XP reward
// within max_quest_xp_reward and HardMaxQuestXpReward. The op stops at
// simQuestMax quests.
func SimulateMsgCreateQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgCreateQuest{}, decodeAs[types.MsgCreateQuest](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			quests, err := allQuests(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			if len(quests)+len(taken) >= simQuestMax {
				return nil, "enough quest definitions", nil
			}
			pending := map[string]bool{}
			for _, m := range taken {
				pending[m.(*types.MsgCreateQuest).QuestId] = true
			}
			id := randomQuestId(e.r)
			if _, exists := quests[id]; exists || pending[id] {
				return nil, "quest id already taken", nil
			}
			params, err := e.k.Params.Get(e.ctx)
			if err != nil {
				return nil, "", err
			}
			return &types.MsgCreateQuest{
				Authority:      policy,
				QuestId:        id,
				Name:           fmt.Sprintf("Quest of the %s", pickName(e)),
				Description:    "A simulation generated quest",
				XpReward:       randomQuestXP(e, params),
				Repeatable:     e.r.Intn(2) == 1,
				CooldownEpochs: commontypes.Saturate[uint64](e.r.Intn(3)),
			}, "", nil
		})
}

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

// SimulateMsgUpdateQuest updates a quest definition through an executed
// x/commons proposal (council.go), one step per call: a new name, XP reward
// (within max_quest_xp_reward and HardMaxQuestXpReward), repeatability and
// cooldown. Objectives are omitted (the handler keeps the existing list),
// and the window, level and prerequisite fields carry over unchanged. A
// quest is deactivated by the update only while simQuestTarget other
// active quests remain.
func SimulateMsgUpdateQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgUpdateQuest{}, decodeAs[types.MsgUpdateQuest](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			quests, err := allQuests(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			pending := map[string]bool{}
			for _, m := range taken {
				pending[m.(*types.MsgUpdateQuest).QuestId] = true
			}
			var ids []string
			for _, id := range sortedKeys(quests) {
				if !pending[id] {
					ids = append(ids, id)
				}
			}
			id, ok := pick(e.r, ids)
			if !ok {
				return nil, "no quest to update", nil
			}
			q := quests[id]
			params, err := e.k.Params.Get(e.ctx)
			if err != nil {
				return nil, "", err
			}
			active := q.Active
			if e.r.Intn(4) == 0 {
				active = !active
			}
			if !active && activeQuestCount(quests) <= simQuestTarget {
				active = true
			}
			return &types.MsgUpdateQuest{
				Authority:           policy,
				QuestId:             id,
				Name:                fmt.Sprintf("Quest of the %s", pickName(e)),
				Description:         "An updated simulation quest",
				XpReward:            randomQuestXP(e, params),
				Repeatable:          e.r.Intn(2) == 1,
				CooldownEpochs:      commontypes.Saturate[uint64](e.r.Intn(3)),
				Season:              q.Season,
				StartBlock:          q.StartBlock,
				EndBlock:            q.EndBlock,
				MinLevel:            q.MinLevel,
				RequiredAchievement: q.RequiredAchievement,
				PrerequisiteQuest:   q.PrerequisiteQuest,
				QuestChain:          q.QuestChain,
				Active:              active,
			}, "", nil
		})
}

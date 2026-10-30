package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgDeactivateQuest deactivates an active quest through an
// executed x/commons proposal (council.go), one step per call, while more
// than simQuestTarget active quests remain (counting deactivations already
// proposed), so the quest start and claim ops keep something to work on.
func SimulateMsgDeactivateQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return authorityOp(ak, bk, k, txGen, &types.MsgDeactivateQuest{}, decodeAs[types.MsgDeactivateQuest](),
		func(e *simEnv, policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
			quests, err := allQuests(e.ctx, e.k)
			if err != nil {
				return nil, "", err
			}
			pending := map[string]bool{}
			for _, m := range taken {
				pending[m.(*types.MsgDeactivateQuest).QuestId] = true
			}
			if activeQuestCount(quests)-len(pending) <= simQuestTarget {
				return nil, "too few active quests to deactivate one", nil
			}
			var ids []string
			for _, id := range sortedKeys(quests) {
				if quests[id].Active && !pending[id] {
					ids = append(ids, id)
				}
			}
			id, ok := pick(e.r, ids)
			if !ok {
				return nil, "no active quest", nil
			}
			return &types.MsgDeactivateQuest{Authority: policy, QuestId: id}, "", nil
		})
}

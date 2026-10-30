package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgAwardFromTagBudget has a member of a budget's group award part
// of an active tag budget to the author of a forum post carrying the tag.
func SimulateMsgAwardFromTagBudget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAwardFromTagBudget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bs, err := e.tagBudgets(func(b types.TagBudget) bool { return b.Active && poolBalance(b).GTE(math.NewInt(100)) })
		if err != nil {
			return noOpErr(label, err)
		}
		if len(bs) == 0 {
			return noOp(label, "no active tag budget with funds")
		}
		posts := e.forumPosts()
		var cands []candidate
		for _, b := range bs {
			g, ok := e.groupByPolicy(b.GroupAccount)
			if !ok {
				continue
			}
			awarders := e.liveSeats(g.Index)
			for _, p := range posts {
				tagged := false
				for _, t := range p.Tags {
					tagged = tagged || t == b.Tag
				}
				by, ok := pick(e.r, awarders)
				if !tagged || !ok || e.k.SameMember(e.ctx, p.Author, by.Address.String()) {
					continue
				}
				amt := randBetween(e.r, math.OneInt(), poolBalance(b).QuoRaw(10))
				cands = append(cands, candidate{acc: by, msg: &types.MsgAwardFromTagBudget{
					Creator: by.Address.String(), BudgetId: b.Id, PostId: p.PostId, Amount: amt.String(), Reason: randomReason(e.r),
				}, spent: math.ZeroInt()})
			}
		}
		return e.firstValid(label, cands, 4, "no forum post carries an active budget's tag")
	})
}

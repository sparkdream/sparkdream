package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgMakePostPermanent delivers MsgMakePostPermanent on a random
// active, unexpired ephemeral post from a live member at
// make_permanent_min_trust_level (any such member may promote content).
// With no ephemeral post, a non-member (whose posts are ephemeral) creates
// one first.
func SimulateMsgMakePostPermanent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgMakePostPermanent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgMakePostPermanent{}
		promoter, ok := e.promoter()
		if !ok {
			return noOp(label, "no live member at make_permanent_min_trust_level")
		}
		ephemeral := func(p types.Post) bool { return activePost(p) && p.ExpiresAt > e.now() }
		post, ok := pick(e.r, e.postsWhere(ephemeral))
		if !ok {
			var err error
			post, ok, err = e.ensurePost(func(a simtypes.Account) bool { return !e.member(a) })
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok || !ephemeral(post) {
				return noOp(label, "no ephemeral post and none could be created")
			}
		}
		return e.submit(promoter, &types.MsgMakePostPermanent{Creator: promoter.Address.String(), Id: post.Id}, zero())
	})
}

// promoter returns a random live member at make_permanent_min_trust_level.
func (e *simEnv) promoter() (simtypes.Account, bool) {
	lvl := commontypes.Saturate[int32](e.params.MakePermanentMinTrustLevel)
	return pick(e.r, e.live(func(a simtypes.Account) bool { return e.meetsTrust(a.Address.String(), lvl) }))
}

package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgPinPost delivers MsgPinPost on a random active, permanent,
// unpinned post from a live member at pin_min_trust_level. Pinning is
// display-only and needs permanent content, so with no such post a member
// (whose posts are permanent) creates one first.
func SimulateMsgPinPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgPinPost{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		label := &types.MsgPinPost{}
		pinner, ok := e.pinner()
		if !ok {
			return noOp(label, "no live member at pin_min_trust_level")
		}
		pinnable := func(p types.Post) bool { return activePost(p) && p.ExpiresAt == 0 && p.PinnedBy == "" }
		post, ok := pick(e.r, e.postsWhere(pinnable))
		if !ok {
			var err error
			post, ok, err = e.ensurePost(e.member)
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok || !pinnable(post) {
				return noOp(label, "no permanent unpinned post and none could be created")
			}
		}
		return e.submit(pinner, &types.MsgPinPost{Creator: pinner.Address.String(), Id: post.Id}, zero())
	})
}

// pinner returns a random live member at pin_min_trust_level (pin and
// unpin share the gate).
func (e *simEnv) pinner() (simtypes.Account, bool) {
	lvl := commontypes.Saturate[int32](e.params.PinMinTrustLevel)
	return pick(e.r, e.live(func(a simtypes.Account) bool { return e.meetsTrust(a.Address.String(), lvl) }))
}

// member reports whether a sim account is an active x/rep member.
func (e *simEnv) member(a simtypes.Account) bool {
	return e.isActiveMember(a.Address.String())
}

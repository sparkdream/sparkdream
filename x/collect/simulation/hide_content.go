package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgHideContent delivers MsgHideContent on the sentinel path: a
// CONTENT_SENTINEL bonded-role holder (bonding one first through x/rep's
// MsgBondRole when no sim account holds the role) under its daily hide cap
// and outside the overturn cooldown, whose available bond covers
// sentinel_commit_amount, hides a public, active collection or non-hidden
// item with no unresolved hide record that it does not own. The council
// path (a Commons Operations Committee member) is not simulated here.
func SimulateMsgHideContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgHideContent{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgHideContent{}
		p := e.params
		sentinels, err := e.ensureRoleHolder(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL)
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(sentinels) == 0 {
			return noOp(msg, "no sim account holds or can bond CONTENT_SENTINEL")
		}
		sentinels = accountsWhere(sentinels, func(a simtypes.Account) bool {
			return e.dailyCount(a.Address.String(), "sentinel_hide") < p.MaxHidesPerSentinelPerDay
		})
		sentinel, ok := pick(e.r, sentinels)
		if !ok {
			return noOp(msg, "no sentinel under its daily hide cap")
		}
		targets, err := e.publicTargets(false)
		if err != nil {
			return noOpErr(msg, err)
		}
		// A sentinel cannot hide its own collection or an item it added
		// (ErrSelfModeration).
		addr := sentinel.Address.String()
		var others []target
		for _, t := range targets {
			if t.coll.Owner == addr {
				continue
			}
			if t.typ == types.FlagTargetType_FLAG_TARGET_TYPE_ITEM {
				if it, err := e.k.Item.Get(e.ctx, t.id); err != nil || it.AddedBy == addr {
					continue
				}
			}
			others = append(others, t)
		}
		t, ok := pick(e.r, others)
		if !ok {
			return noOp(msg, "no public active target the sentinel does not own")
		}
		hidden, err := e.hasUnresolvedHide(t.typ, t.id)
		if err != nil {
			return noOpErr(msg, err)
		}
		if hidden {
			return noOp(msg, "target has an unresolved hide record")
		}
		msg.Creator, msg.TargetId, msg.TargetType = sentinel.Address.String(), t.id, t.typ
		msg.ReasonCode = randomModerationReason(e.r)
		msg.Authority = types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL
		if e.r.Intn(2) == 0 {
			msg.Authority = types.ModerationAuthority_MODERATION_AUTHORITY_AUTO
		}
		if e.r.Intn(5) == 0 && p.MaxFlagReasonLength > 0 {
			msg.ReasonCode = commontypes.ModerationReason_MODERATION_REASON_OTHER
			msg.ReasonText = "phoenix policy"
			if commontypes.Saturate[uint32](len(msg.ReasonText)) > p.MaxFlagReasonLength {
				msg.ReasonText = msg.ReasonText[:p.MaxFlagReasonLength]
			}
		}
		return e.submit(sentinel, msg, math.ZeroInt())
	})
}

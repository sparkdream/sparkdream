package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// CollectRepHooks implements reptypes.RepHooks for x/collect. It enqueues
// newly-admitted members into the EndBlocker promotion queue so two kinds of
// state become eventually consistent with the user's new membership:
//
//  1. Any non-member-collaborator records the new member holds on
//     member-owned collections have their inviter's locked DREAM refunded
//     (the inviter no longer needs to be "vouching" for behavior since the
//     member is now subject to full slashing).
//  2. Any ephemeral collections the new member owns (PENDING or ACTIVE+TTL,
//     including endorsed ones) are flipped to permanent — matching the
//     member-creates-permanent path that would have applied if the user had
//     been a member at collection-create time.
//
// Both kinds of work share the params.max_promotions_per_block cap so the
// EndBlocker stays bounded under bursts of admissions.
type CollectRepHooks struct {
	k *Keeper
}

// NewCollectRepHooks builds the hook implementation. Holds a pointer to the
// keeper so app.go's late wiring of repKeeper (via SetRepKeeper) is visible at
// hook-invocation time.
func NewCollectRepHooks(k *Keeper) CollectRepHooks {
	return CollectRepHooks{k: k}
}

// Assert at compile time that CollectRepHooks satisfies reptypes.RepHooks.
var _ reptypes.RepHooks = CollectRepHooks{}

// AfterMemberAdmitted is invoked by x/rep after a Member record is created in
// AcceptInvitation. The EndBlocker drains the queue at the configured per-
// block cap.
//
// Tolerating an unwired keeper is intentional: x/rep treats hook errors as
// non-tx-halting, and the late-wiring gap at genesis bootstrap means the
// keeper pointer can briefly be nil — silently no-op rather than crash.
func (h CollectRepHooks) AfterMemberAdmitted(ctx context.Context, member sdk.AccAddress) error {
	if h.k == nil {
		return nil
	}
	addr, err := h.k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	return h.k.EnqueueMemberForPromotion(ctx, addr)
}

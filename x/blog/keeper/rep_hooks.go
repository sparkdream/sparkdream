package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// BlogRepHooks implements reptypes.RepHooks for x/blog. It enqueues newly-
// admitted members into the EndBlocker promotion queue so their pre-admission
// ephemeral content is eagerly flipped to permanent (capped per block by
// params.max_promotions_per_block).
type BlogRepHooks struct {
	k *Keeper
}

// NewBlogRepHooks builds the hook implementation. Holds a pointer to the
// keeper so the SetClaim-style late wiring of repKeeper/identityKeeper from
// app.go is visible at hook-invocation time.
func NewBlogRepHooks(k *Keeper) BlogRepHooks {
	return BlogRepHooks{k: k}
}

// Assert at compile time that BlogRepHooks satisfies reptypes.RepHooks.
var _ reptypes.RepHooks = BlogRepHooks{}

// AfterMemberAdmitted is invoked by x/rep after a Member record is created in
// AcceptInvitation. We enqueue the new member's address into the blog
// promotion queue; the EndBlocker drains the queue at the configured per-
// block cap, flipping each ephemeral post/reply to expires_at=0 so the
// frontend sees clean permanent content without scanning for is-member.
//
// Returning nil for an unwired keeper is intentional: x/rep treats hook
// errors as non-tx-halting, but we additionally tolerate the late-wiring
// gap that exists at genesis-bootstrap time.
func (h BlogRepHooks) AfterMemberAdmitted(ctx context.Context, member sdk.AccAddress) error {
	if h.k == nil {
		return nil
	}
	creator, err := h.k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	h.k.EnqueueAuthorForPromotion(ctx, creator)
	return nil
}

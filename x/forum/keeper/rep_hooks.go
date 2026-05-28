package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// ForumRepHooks implements reptypes.RepHooks for x/forum. It enqueues newly-
// admitted members into the EndBlocker promotion queue so their pre-admission
// ephemeral posts (capped per block by params.max_promotions_per_block) flip
// to permanent eagerly, rather than waiting for the lazy upgrade path that
// only fires when each post hits its TTL.
type ForumRepHooks struct {
	k *Keeper
}

// NewForumRepHooks builds the hook implementation. Holds a pointer to the
// keeper so the SetClaim-style late wiring of repKeeper/identityKeeper from
// app.go is visible at hook-invocation time.
func NewForumRepHooks(k *Keeper) ForumRepHooks {
	return ForumRepHooks{k: k}
}

// Assert at compile time that ForumRepHooks satisfies reptypes.RepHooks.
var _ reptypes.RepHooks = ForumRepHooks{}

// AfterMemberAdmitted is invoked by x/rep after a Member record is created in
// AcceptInvitation. The EndBlocker drains the queue at the configured per-
// block cap, flipping each ephemeral post to ExpirationTime=0.
//
// Returning nil for an unwired keeper is intentional: x/rep treats hook
// errors as non-tx-halting, but we additionally tolerate the late-wiring
// gap that exists at genesis-bootstrap time.
func (h ForumRepHooks) AfterMemberAdmitted(ctx context.Context, member sdk.AccAddress) error {
	if h.k == nil {
		return nil
	}
	author, err := h.k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	return h.k.EnqueueAuthorForPromotion(ctx, author)
}

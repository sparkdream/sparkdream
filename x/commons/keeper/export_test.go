package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/types"
)

// SetProposalQueued stores a proposal and indexes it in the EndBlock queue,
// the way SubmitProposal does (tests write proposals directly).
func (k Keeper) SetProposalQueued(ctx context.Context, id uint64, p types.Proposal) error {
	if err := k.Proposals.Set(ctx, id, p); err != nil {
		return err
	}
	return k.enqueueProposal(ctx, p)
}

// IsPolicyInSubtree exposes isPolicyInSubtree for the emergency-cancel
// self-protection tests.
func (k Keeper) IsPolicyInSubtree(ctx context.Context, policy, ancestor string) bool {
	return k.isPolicyInSubtree(ctx, policy, ancestor)
}

// IsTermExpired exposes isTermExpired.
func IsTermExpired(g types.Group, now int64) bool { return isTermExpired(g, now) }

// CreateTestAccount stores a base account so the genesis bootstrap finds a
// founder (the fixture's auth keeper is the real one).
func (k Keeper) CreateTestAccount(ctx context.Context, addr sdk.AccAddress) {
	ak := k.authKeeper.(interface {
		NewAccountWithAddress(context.Context, sdk.AccAddress) sdk.AccountI
		SetAccount(context.Context, sdk.AccountI)
	})
	ak.SetAccount(ctx, ak.NewAccountWithAddress(ctx, addr))
}

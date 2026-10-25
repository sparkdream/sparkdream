package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vault/types"
)

// EndBlocker runs two capped passes: PENDING proposals whose expires_at has
// passed become EXPIRED, and resolved proposals older than ProposalRetention
// are deleted (events carry the audit trail). Same bounded-pass pattern as the
// x/session EndBlocker.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	now := ctx.BlockTime().Unix()
	if err := k.expirePass(ctx, now, types.MaxEndBlockerPassItems); err != nil {
		return err
	}
	return k.retentionPass(ctx, now, types.MaxEndBlockerPassItems)
}

func (k Keeper) expirePass(ctx sdk.Context, now int64, cap int) error {
	var due []collections.Pair[int64, uint64]
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](now+1, 0))
	if err := k.ExpiryQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		due = append(due, key)
		return len(due) >= cap, nil
	}); err != nil {
		return err
	}
	for _, key := range due {
		p, err := k.VaultProposal.Get(ctx, key.K2())
		if err != nil || p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING || p.ExpiresAt != key.K1() {
			_ = k.ExpiryQueue.Remove(ctx, key) // stale entry
			continue
		}
		if err := k.resolveProposal(ctx, p, types.ProposalStatus_PROPOSAL_STATUS_EXPIRED); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("vault_proposal_expired",
			sdk.NewAttribute("vault_id", fmt.Sprintf("%d", p.VaultId)),
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", p.ProposalId)),
		))
	}
	return nil
}

func (k Keeper) retentionPass(ctx sdk.Context, now int64, cap int) error {
	var due []collections.Pair[int64, uint64]
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](now-types.ProposalRetention+1, 0))
	if err := k.RetentionQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		due = append(due, key)
		return len(due) >= cap, nil
	}); err != nil {
		return err
	}
	for _, key := range due {
		if err := k.RetentionQueue.Remove(ctx, key); err != nil {
			return err
		}
		p, err := k.VaultProposal.Get(ctx, key.K2())
		if err != nil || p.Status == types.ProposalStatus_PROPOSAL_STATUS_PENDING || p.ResolvedAt != key.K1() {
			continue // stale entry
		}
		if err := k.VaultProposal.Remove(ctx, key.K2()); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("vault_proposal_pruned",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", p.ProposalId)),
			sdk.NewAttribute("status", p.Status.String()),
		))
	}
	return nil
}

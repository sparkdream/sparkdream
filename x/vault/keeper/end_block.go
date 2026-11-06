package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vault/types"
)

// EndBlocker runs three capped passes: PENDING proposals whose expires_at
// has passed become EXPIRED, resolved proposals older than ProposalRetention
// are deleted (events carry the audit trail), and unaccepted signer listings
// past invite_expires_at are removed. Same bounded-pass pattern as the
// x/session EndBlocker.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	now := ctx.BlockTime().Unix()
	if err := k.expirePass(ctx, now, types.MaxEndBlockerPassItems); err != nil {
		return err
	}
	if err := k.retentionPass(ctx, now, types.MaxEndBlockerPassItems); err != nil {
		return err
	}
	return k.inviteExpiryPass(ctx, now, types.MaxEndBlockerPassItems)
}

func (k Keeper) inviteExpiryPass(ctx sdk.Context, now int64, cap int) error {
	var due []collections.Triple[int64, uint64, string]
	rng := new(collections.Range[collections.Triple[int64, uint64, string]]).
		EndExclusive(collections.Join3[int64, uint64, string](now+1, 0, ""))
	if err := k.InviteExpiryQueue.Walk(ctx, rng, func(key collections.Triple[int64, uint64, string]) (bool, error) {
		due = append(due, key)
		return len(due) >= cap, nil
	}); err != nil {
		return err
	}
	for _, key := range due {
		if err := k.expireInvite(ctx, key.K1(), key.K2(), key.K3()); err != nil {
			return err
		}
	}
	return nil
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
			if err := k.ExpiryQueue.Remove(ctx, key); err != nil {
				ctx.Logger().Error("expirePass: ExpiryQueue.Remove failed", "err", err)
			} // stale entry
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
		if err := k.ProposalsByVault.Remove(ctx, collections.Join(p.VaultId, p.ProposalId)); err != nil {
			return err
		}
		if err := k.ProposalsByProposer.Remove(ctx, collections.Join(p.Proposer, p.ProposalId)); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent("vault_proposal_pruned",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", p.ProposalId)),
			sdk.NewAttribute("status", p.Status.String()),
		))
	}
	return nil
}

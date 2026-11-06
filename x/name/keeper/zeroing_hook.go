package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/types"
)

// ZeroingHook adapts the keeper to x/rep's MemberZeroingHook.
type ZeroingHook struct{ k *Keeper }

// NewZeroingHook returns the hook app.go registers with x/rep.
func NewZeroingHook(k *Keeper) ZeroingHook { return ZeroingHook{k: k} }

// AfterMemberZeroed releases a zeroed member's names and settles their
// name-dispute stakes (NAM-101, NAM-202). x/rep burned the DREAM behind the
// stakes with the rest of the member's balance; kept, every resolution of
// the dispute failed on the zeroed side's unlock or burn, the other party's
// stake stayed locked and the name stayed frozen for good. Once an
// overturned zeroing gave the member a balance back, a kept record would
// instead release an unrelated lock (REV-101), so the records go here
// rather than at resolution.
//
//   - Disputes the member filed are closed: the claimant's stake is gone,
//     the name stays with its owner, and a contesting owner's stake is
//     returned (the contest succeeded).
//   - Disputes against the member's names are decided at once: upheld when
//     the claimant can still receive the name (name transferred, claimant
//     stake returned, the zeroed owner's contest stake is gone with the
//     balance burn), voided otherwise (claimant stake returned, name stays).
//   - Every OwnerInfo the member held is marked scavengeable: a zeroing
//     punishes the position, and a name is a position, so the names are
//     released like an owner inactive past expiration_duration
//     (IsOwnerExpired) instead of squatting until the one-year clock.
func (h ZeroingHook) AfterMemberZeroed(ctx context.Context, member string, addresses []string) error {
	k := h.k
	closed, decided, released := 0, 0, 0
	for _, addr := range addresses {
		var names []string
		if err := k.ClaimantDisputes.Walk(ctx, collections.NewPrefixedPairRange[string, string](addr),
			func(key collections.Pair[string, string]) (bool, error) {
				names = append(names, key.K2())
				return false, nil
			}); err != nil {
			return err
		}
		for _, name := range names {
			dispute, found := k.GetDispute(ctx, name)
			if !found || !dispute.Active {
				continue
			}
			if err := k.closeZeroedClaimantDispute(ctx, dispute); err != nil {
				return err
			}
			closed++
		}

		names = names[:0]
		if err := k.OwnerNames.Walk(ctx, collections.NewPrefixedPairRange[string, string](addr),
			func(key collections.Pair[string, string]) (bool, error) {
				names = append(names, key.K2())
				return false, nil
			}); err != nil {
			return err
		}
		for _, name := range names {
			dispute, found := k.GetDispute(ctx, name)
			if !found || !dispute.Active {
				continue
			}
			if err := k.decideZeroedOwnerDispute(ctx, dispute); err != nil {
				return err
			}
			decided++
		}

		info, err := k.Owners.Get(ctx, addr)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return err
		}
		if info.Scavengeable {
			continue
		}
		info.Scavengeable = true
		if err := k.Owners.Set(ctx, addr, info); err != nil {
			return err
		}
		released++
	}
	if closed+decided+released > 0 {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"name_zeroed_stakes_settled",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("disputes_closed", fmt.Sprintf("%d", closed)),
			sdk.NewAttribute("disputes_decided", fmt.Sprintf("%d", decided)),
			sdk.NewAttribute("owners_released", fmt.Sprintf("%d", released)),
		))
	}
	return nil
}

// decideZeroedOwnerDispute settles an active dispute against a name whose
// owner was zeroed (NAM-202). The claimant's stake is returned either way
// (nobody misbehaved on that side): the dispute is upheld and the name
// transferred when the claimant can still receive it, voided when not. The
// zeroed owner's contest stake, if any, has no DREAM behind it any more;
// UnlockOrGone treats it as settled.
func (k Keeper) decideZeroedOwnerDispute(ctx context.Context, dispute types.Dispute) error {
	if !dispute.StakeAmount.IsUint64() {
		return fmt.Errorf("dispute stake amount too large for uint64: %s", dispute.StakeAmount)
	}
	if dispute.ContestChallengeId != "" {
		if stake, err := k.ContestStakes.Get(ctx, dispute.ContestChallengeId); err == nil {
			if !stake.Amount.IsUint64() {
				return fmt.Errorf("contest stake amount too large for uint64: %s", stake.Amount)
			}
			if _, err := k.dreamOps.UnlockOrGone(ctx, stake.Owner, stake.Amount.Uint64()); err != nil {
				return err
			}
			if err := k.ContestStakes.Remove(ctx, dispute.ContestChallengeId); err != nil {
				return err
			}
		}
	}
	outcome := "voided"
	claimant := k.canonicalMember(ctx, dispute.Claimant)
	if k.claimantEligible(ctx, claimant) {
		if err := k.transferName(ctx, dispute.Name, claimant); err != nil {
			return err
		}
		outcome = "upheld"
	}
	if _, err := k.dreamOps.UnlockOrGone(ctx, dispute.Claimant, dispute.StakeAmount.Uint64()); err != nil {
		return err
	}
	if err := k.DisputeStakes.Remove(ctx, fmt.Sprintf("name_dispute:%s:%d", dispute.Name, dispute.FiledAt)); err != nil {
		return err
	}
	dispute.Active = false
	if err := k.SetDispute(ctx, dispute); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"name_dispute_resolved",
		sdk.NewAttribute("name", dispute.Name),
		sdk.NewAttribute("claimant", dispute.Claimant),
		sdk.NewAttribute("outcome", outcome),
		sdk.NewAttribute("contested", fmt.Sprintf("%t", dispute.ContestChallengeId != "")),
		sdk.NewAttribute("reason", "owner_zeroed"),
	))
	return nil
}

// closeZeroedClaimantDispute closes an active dispute whose claimant was
// zeroed: the claimant's stake record goes (its DREAM is already burned),
// a contesting owner's stake is returned, and the name stays put.
func (k Keeper) closeZeroedClaimantDispute(ctx context.Context, dispute types.Dispute) error {
	if dispute.ContestChallengeId != "" {
		if stake, err := k.ContestStakes.Get(ctx, dispute.ContestChallengeId); err == nil {
			if !stake.Amount.IsUint64() {
				return fmt.Errorf("contest stake amount too large for uint64: %s", stake.Amount)
			}
			if _, err := k.dreamOps.UnlockOrGone(ctx, stake.Owner, stake.Amount.Uint64()); err != nil {
				return err
			}
			if err := k.ContestStakes.Remove(ctx, dispute.ContestChallengeId); err != nil {
				return err
			}
		}
		dispute.ContestSucceeded = true
	}
	if err := k.DisputeStakes.Remove(ctx, fmt.Sprintf("name_dispute:%s:%d", dispute.Name, dispute.FiledAt)); err != nil {
		return err
	}
	dispute.Active = false
	if err := k.SetDispute(ctx, dispute); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"name_dispute_resolved",
		sdk.NewAttribute("name", dispute.Name),
		sdk.NewAttribute("claimant", dispute.Claimant),
		sdk.NewAttribute("outcome", "claimant_zeroed"),
		sdk.NewAttribute("contested", fmt.Sprintf("%t", dispute.ContestChallengeId != "")),
	))
	return nil
}

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/commons
// (member-key-migration plan §8, commons row): council and committee seats
// follow the identity automatically, keeping weight and added_at, and votes
// on open proposals are re-keyed so the member can neither vote twice nor
// lose a vote already cast. This is an internal re-key, not
// MsgUpdateGroupMembers: it skips the electoral rate limit and MinMembers
// flapping. The parent or electoral policy can still remove the new address
// with MsgUpdateGroupMembers.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/commons migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration vetoes a migration whose target already sits on a
// body the source sits on: moving the seat would silently merge two seats
// into one (the member count drops, possibly below min_members, and a vote
// of the target would be overwritten).
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr, toStr := from.String(), to.String()
	var conflict string
	if err := k.Groups.Walk(ctx, nil, func(name string, _ types.Group) (bool, error) {
		hasFrom, err := k.Members.Has(ctx, collections.Join(name, fromStr))
		if err != nil || !hasFrom {
			return false, err
		}
		hasTo, err := k.Members.Has(ctx, collections.Join(name, toStr))
		if err != nil {
			return true, err
		}
		if hasTo {
			conflict = name
			return true, nil
		}
		return false, nil
	}); err != nil {
		return err
	}
	if conflict != "" {
		return fmt.Errorf("migration target %s already holds a seat on %s, where %s also sits", toStr, conflict, fromStr)
	}
	return nil
}

// AfterMemberMigration moves every seat and every vote on an open proposal.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr, toStr := from.String(), to.String()
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Open (SUBMITTED) proposals by council, read from the proposal queue
	// so the cost follows open proposals, not proposal history.
	openByCouncil := make(map[string][]uint64)
	if err := k.ProposalQueue.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
		p, err := k.Proposals.Get(ctx, key.K2())
		if err == nil && p.Status == types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED {
			openByCouncil[p.CouncilName] = append(openByCouncil[p.CouncilName], p.Id)
		}
		return false, nil
	}); err != nil {
		return err
	}

	// Seats: one lookup per registered group (councils, committees, boards
	// and sub-groups), a small bounded set.
	var groups []string
	if err := k.Groups.Walk(ctx, nil, func(name string, _ types.Group) (bool, error) {
		groups = append(groups, name)
		return false, nil
	}); err != nil {
		return err
	}
	for _, name := range groups {
		key := collections.Join(name, fromStr)
		member, err := k.Members.Get(ctx, key)
		if err != nil {
			continue
		}
		if err := k.Members.Remove(ctx, key); err != nil {
			return err
		}
		member.Address = toStr
		if err := k.Members.Set(ctx, collections.Join(name, toStr), member); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("council_seat_migrated",
			sdk.NewAttribute("council", name),
			sdk.NewAttribute("from", fromStr),
			sdk.NewAttribute("to", toStr),
		))

		// The seat's weight is unchanged, but a golden-share council
		// re-derives its seat weight from the member set.
		if err := k.syncGoldenShareWeight(ctx, name); err != nil {
			return err
		}

		// Votes on this group's open proposals.
		for _, id := range openByCouncil[name] {
			voteKey := collections.Join(id, fromStr)
			vote, err := k.Votes.Get(ctx, voteKey)
			if err != nil {
				continue
			}
			// Never overwrite a vote the target already cast.
			if has, err := k.Votes.Has(ctx, collections.Join(id, toStr)); err != nil {
				return err
			} else if has {
				if err := k.Votes.Remove(ctx, voteKey); err != nil {
					return err
				}
				continue
			}
			if err := k.Votes.Remove(ctx, voteKey); err != nil {
				return err
			}
			vote.Voter = toStr
			if err := k.Votes.Set(ctx, collections.Join(id, toStr), vote); err != nil {
				return err
			}
		}
	}
	return nil
}

// MaxLiveVotingPeriodSeconds returns the longest voting period any registered
// decision policy uses. x/rep refuses to lower zk_key_activation_delay below
// it (plan decision 17).
func (k Keeper) MaxLiveVotingPeriodSeconds(ctx context.Context) int64 {
	var max int64
	if err := k.DecisionPolicies.Walk(ctx, nil, func(_ string, dp types.DecisionPolicy) (bool, error) {
		if dp.VotingPeriod > max {
			max = dp.VotingPeriod
		}
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("MaxLiveVotingPeriodSeconds: DecisionPolicies.Walk failed", "err", err)
	}
	return max
}

// checkVotingPeriodCap refuses a voting period longer than x/rep's ZK key
// activation delay: a council could otherwise register a window longer than
// the delay and reopen the anonymous double-vote hole the delay closes.
// Without x/rep wired (unit tests) there is no cap.
func (k Keeper) checkVotingPeriodCap(ctx context.Context, votingPeriodSeconds int64) error {
	if k.late.repKeeper == nil {
		return nil
	}
	cap := k.late.repKeeper.ZkKeyActivationDelaySeconds(ctx)
	if cap > 0 && votingPeriodSeconds > cap {
		return fmt.Errorf("voting_period %ds exceeds the zk key activation delay (%ds); see x/rep zk_key_activation_delay", votingPeriodSeconds, cap)
	}
	return nil
}

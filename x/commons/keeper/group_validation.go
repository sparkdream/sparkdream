package keeper

import (
	"context"
	"slices"
	"strings"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Compiled committee size bounds (docs/security-hardening.md, "Committee
// Size Constraints"). They apply to the six standing committees of the
// genesis bootstrap whatever the signer, including x/gov: changing them takes
// a chain upgrade. Every other group's bounds may only be changed by x/gov.
const (
	CommitteeMinMembers uint64 = 2
	CommitteeMaxMembers uint64 = 5
)

// boundedCommittees are the standing committees whose size bounds are
// compiled in.
var boundedCommittees = map[string]bool{
	"Commons Operations Committee":   true,
	"Commons Governance Committee":   true,
	"Technical Operations Committee": true,
	"Technical Governance Committee": true,
	"Ecosystem Operations Committee": true,
	"Ecosystem Governance Committee": true,
}

// reservedGroupNames are names a council may never register: the genesis
// bodies themselves (a deleted body can only come back through x/gov) plus
// the short forms other modules have historically looked up by name. The
// match is case-insensitive.
var reservedGroupNames = []string{
	"operations committee",
	"governance committee",
	"supervisory board",
}

// isReservedGroupName reports whether a non-gov signer is refused the name.
func isReservedGroupName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for canonical := range equalWeightGroups {
		if strings.ToLower(canonical) == lower {
			return true
		}
	}
	return slices.Contains(reservedGroupNames, lower)
}

// checkCommitteeBounds enforces the compiled [2,5] bounds on a standing
// committee. Other groups pass.
func checkCommitteeBounds(groupName string, minMembers, maxMembers uint64) error {
	if !boundedCommittees[groupName] {
		return nil
	}
	if minMembers < CommitteeMinMembers || maxMembers > CommitteeMaxMembers {
		return errorsmod.Wrapf(types.ErrInvalidGroupSize,
			"%s size bounds are fixed at [%d,%d] (got [%d,%d]); changing them takes a chain upgrade",
			groupName, CommitteeMinMembers, CommitteeMaxMembers, minMembers, maxMembers)
	}
	return nil
}

// validateVoteThreshold checks a decision-policy threshold.
//
//   - It must be present.
//   - A percentage threshold lies in (0, 1]: 0 or a negative value would
//     accept a proposal on its first vote, a value above 1 makes the group
//     permanently inert.
//   - An absolute threshold is at least 1 and, when totalWeight is known
//     (positive), at most the group's total weight.
//   - A non-gov signer may not go below a simple majority (0.5) for a
//     percentage policy.
func validateVoteThreshold(policyType string, threshold *math.LegacyDec, totalWeight math.LegacyDec, isGov bool) error {
	if threshold == nil || threshold.IsNil() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "vote_threshold is required")
	}
	switch policyType {
	case PolicyTypePercentage:
		if !threshold.IsPositive() || threshold.GT(math.LegacyOneDec()) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"percentage vote_threshold must be in (0, 1], got %s", threshold)
		}
		if !isGov && threshold.LT(math.LegacyNewDecWithPrec(5, 1)) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"percentage vote_threshold below 0.5 needs x/gov, got %s", threshold)
		}
	case PolicyTypeThreshold:
		abs := threshold.TruncateDec()
		if abs.LT(math.LegacyOneDec()) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"absolute vote_threshold must be at least 1, got %s", threshold)
		}
		if totalWeight.IsPositive() && abs.GT(totalWeight) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
				"absolute vote_threshold %s exceeds the group's total weight %s", abs, totalWeight)
		}
	default:
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid policy_type '%s'", policyType)
	}
	return nil
}

// totalMemberWeight sums the members' weights (unparseable weights count 0).
func totalMemberWeight(members []types.Member) math.LegacyDec {
	total := math.LegacyZeroDec()
	for _, m := range members {
		w, err := math.LegacyNewDecFromStr(m.Weight)
		if err != nil {
			continue
		}
		total = total.Add(w)
	}
	return total
}

// validateElectoralAddress checks an electoral_policy_address supplied by a
// non-gov signer: it must be a registered group policy (an executed proposal
// of a body), never a bare key, and must not be the group's own policy
// address (a body electing itself lets the body's quorum replace the body).
// The empty string means "none" and always passes.
func (k Keeper) validateElectoralAddress(ctx context.Context, electoral string, ownPolicy string) error {
	if electoral == "" {
		return nil
	}
	if _, err := k.addressCodec.StringToBytes(electoral); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid electoral_policy_address %s", electoral)
	}
	if !k.IsGroupPolicyAddress(ctx, electoral) {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"electoral_policy_address %s is not a registered group policy", electoral)
	}
	if ownPolicy != "" && electoral == ownPolicy {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest,
			"a group cannot be its own electoral authority")
	}
	return nil
}

// checkSpendCapWithinParent requires a positive max_spend_per_epoch no larger
// than the parent's own cap. A non-gov signer can never create or configure a
// child whose spending is uncapped (0 or nil means "no cap", spec §3.1) or
// larger than the parent's. A parent with no cap of its own bounds nothing.
func (k Keeper) checkSpendCapWithinParent(ctx context.Context, cap *math.Int, parentPolicy string) error {
	if cap == nil || cap.IsNil() || !cap.IsPositive() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest,
			"max_spend_per_epoch must be positive unless set by x/gov (0 means no cap)")
	}
	_, parent, found := k.GetGroupByPolicy(ctx, parentPolicy)
	if !found || parent.MaxSpendPerEpoch == nil || !parent.MaxSpendPerEpoch.IsPositive() {
		return nil
	}
	if cap.GT(*parent.MaxSpendPerEpoch) {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"max_spend_per_epoch %s exceeds the parent's own cap %s", cap, parent.MaxSpendPerEpoch)
	}
	return nil
}

// isTermExpired reports whether the group's current term has ended.
// current_term_expiration == 0 means "no expiry" (spec §3.1), honoured by
// every term check (submit, execute, anonymous submit, renew, spend gates).
func isTermExpired(g types.Group, now int64) bool {
	return g.CurrentTermExpiration > 0 && now > g.CurrentTermExpiration
}

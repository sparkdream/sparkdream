package keeper

import (
	"context"
	"errors"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
)

// equalWeightGroups are the governance bodies the genesis bootstrap creates.
// In them every member votes with weight 1: only x/gov may give a member any
// other weight. A member who already holds another weight keeps it (the
// founder's weight-3 seat on the Technical and Ecosystem Councils), and the
// Commons Council's golden-share seat is sized by syncGoldenShareWeight.
// Changing the set takes a chain upgrade, like the committee size bounds.
var equalWeightGroups = map[string]bool{
	"Commons Council":                true,
	"Commons Operations Committee":   true,
	"Commons Governance Committee":   true,
	"Commons Supervisory Board":      true,
	"Technical Council":              true,
	"Technical Operations Committee": true,
	"Technical Governance Committee": true,
	"Ecosystem Council":              true,
	"Ecosystem Operations Committee": true,
	"Ecosystem Governance Committee": true,
}

// goldenShareCouncils are the councils in which the Commons Council policy
// holds a golden-share seat. The seat's weight always equals the summed
// weight of every other member, so the Commons Council holds half of the
// council's votes however many members join, and nothing passes a threshold
// above one half without it. Only x/gov may remove the seat.
var goldenShareCouncils = map[string]bool{
	"Technical Council": true,
	"Ecosystem Council": true,
}

// goldenShareSeat returns the address of the golden-share seat in
// councilName (the Commons Council policy), or "" when the council has none.
func (k Keeper) goldenShareSeat(ctx context.Context, councilName string) (string, error) {
	if !goldenShareCouncils[councilName] {
		return "", nil
	}
	commons, err := k.Groups.Get(ctx, "Commons Council")
	if errors.Is(err, collections.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return commons.PolicyAddress, nil
}

// validateMemberWeights checks the weights of members being added to (or
// re-added in) councilName. Every weight must be a positive decimal. Unless
// the signer is x/gov, a genesis governance group accepts only weight 1, the
// member's current weight, or any weight for the golden-share seat (which
// syncGoldenShareWeight then overwrites).
func (k Keeper) validateMemberWeights(ctx context.Context, councilName string, adds []types.Member, isGov bool) error {
	for _, m := range adds {
		w, err := math.LegacyNewDecFromStr(m.Weight)
		if err != nil || !w.IsPositive() {
			return errorsmod.Wrapf(types.ErrInvalidMemberWeight, "member %s: weight %q must be a positive number", m.Address, m.Weight)
		}
	}
	if isGov || !equalWeightGroups[councilName] {
		return nil
	}

	seat, err := k.goldenShareSeat(ctx, councilName)
	if err != nil {
		return err
	}
	for _, m := range adds {
		if m.Address == seat {
			continue
		}
		w := math.LegacyMustNewDecFromStr(m.Weight)
		if w.Equal(math.LegacyOneDec()) {
			continue
		}
		current, err := k.GetMember(ctx, councilName, m.Address)
		if err == nil {
			if cw, err := math.LegacyNewDecFromStr(current.Weight); err == nil && cw.Equal(w) {
				continue
			}
		} else if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return errorsmod.Wrapf(types.ErrInvalidMemberWeight,
			"member %s: %s members vote with weight 1; only x/gov may set weight %s", m.Address, councilName, m.Weight)
	}
	return nil
}

// checkGoldenShareKept rejects a membership change by anyone but x/gov that
// would drop the golden-share seat from councilName. remains reports whether
// the seat is still a member after the change.
func (k Keeper) checkGoldenShareKept(ctx context.Context, councilName string, isGov bool, remains func(addr string) bool) error {
	if isGov {
		return nil
	}
	seat, err := k.goldenShareSeat(ctx, councilName)
	if err != nil || seat == "" {
		return err
	}
	isMember, err := k.HasMember(ctx, councilName, seat)
	if err != nil {
		return err
	}
	if isMember && !remains(seat) {
		return errorsmod.Wrapf(types.ErrGoldenShareSeat, "only x/gov may remove the Commons Council seat from %s", councilName)
	}
	return nil
}

// syncGoldenShareWeight sets the golden-share seat's weight in councilName to
// the summed weight of every other member. Call it after any change to the
// council's membership; it does nothing when the council has no seat.
func (k Keeper) syncGoldenShareWeight(ctx context.Context, councilName string) error {
	seat, err := k.goldenShareSeat(ctx, councilName)
	if err != nil || seat == "" {
		return err
	}
	seatMember, err := k.GetMember(ctx, councilName, seat)
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	members, err := k.GetCouncilMembers(ctx, councilName)
	if err != nil {
		return err
	}
	others := math.LegacyZeroDec()
	for _, m := range members {
		if m.Address == seat {
			continue
		}
		w, err := math.LegacyNewDecFromStr(m.Weight)
		if err != nil {
			continue // unparseable weights are skipped by the tally too
		}
		others = others.Add(w)
	}
	if !others.IsPositive() {
		return nil // the seat is the only member left; keep its weight
	}

	weight := others.String()
	if others.IsInteger() {
		weight = others.TruncateInt().String()
	}
	if seatMember.Weight == weight {
		return nil
	}
	seatMember.Weight = weight
	return k.AddMember(ctx, councilName, seatMember)
}

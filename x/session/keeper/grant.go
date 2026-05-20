package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"sparkdream/x/session/types"
)

// terminalStatus reports whether the given status is absorbing — once
// entered, the grant is never re-activated. Used by the index-removal
// helpers to know whether to decrement counters and clear secondary
// indexes.
func terminalStatus(s types.GrantStatus) bool {
	switch s {
	case types.GrantStatus_GRANT_STATUS_DECLINED,
		types.GrantStatus_GRANT_STATUS_REVOKED,
		types.GrantStatus_GRANT_STATUS_COMPLETED,
		types.GrantStatus_GRANT_STATUS_FIRED:
		return true
	default:
		return false
	}
}

// activeStatus reports whether the grant currently counts against
// per-(granter, type) caps. ACTIVE and PAUSED_INSUFFICIENT_FUNDS both pin
// a slot — paused grants are recoverable via retry/claim, so they should
// continue to count until the granter explicitly revokes or the EndBlocker
// auto-revokes after TTL.
func activeStatus(s types.GrantStatus) bool {
	return s == types.GrantStatus_GRANT_STATUS_ACTIVE ||
		s == types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
}

// nextGrantID allocates the next grant ID from the GrantSeq sequence.
// IDs are 1-indexed (the sequence starts at 0; we add 1 so id 0 is
// reserved for "unset"). This matches x/commons's RecurringSpendSeq
// convention.
func (k Keeper) nextGrantID(ctx context.Context) (uint64, error) {
	id, err := k.GrantSeq.Next(ctx)
	if err != nil {
		return 0, err
	}
	return id + 1, nil
}

// writeGrant persists a freshly created grant to the primary store and
// every secondary index, increments the per-(granter, type) active count,
// and (for SESSION_KEY grants) maintains the (granter, grantee) -> id
// back-compat lookup. Callers are responsible for validating the grant
// before calling.
func (k Keeper) writeGrant(ctx context.Context, g types.Grant) error {
	if g.Id == 0 {
		return fmt.Errorf("grant id must be non-zero")
	}
	if g.Type == types.GrantType_GRANT_TYPE_UNSPECIFIED {
		return fmt.Errorf("grant type must be specified")
	}
	if g.Status == types.GrantStatus_GRANT_STATUS_UNSPECIFIED {
		return fmt.Errorf("grant status must be specified")
	}

	if err := k.Grants.Set(ctx, g.Id, g); err != nil {
		return err
	}
	if err := k.GrantsByGranter.Set(ctx, collections.Join(g.Granter, g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByGrantee.Set(ctx, collections.Join(g.Grantee, g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByExpiration.Set(ctx, collections.Join(g.ExpiresAt.Unix(), g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByTypeAndGranter.Set(ctx, collections.Join3(int32(g.Type), g.Granter, g.Id)); err != nil {
		return err
	}

	if activeStatus(g.Status) {
		if err := k.incActiveGrantCount(ctx, g.Granter, g.Type); err != nil {
			return err
		}
	}

	// SESSION_KEY back-compat: enforce one-active-per-pair.
	if g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY {
		if err := k.SessionKeyByPair.Set(ctx, collections.Join(g.Granter, g.Grantee), g.Id); err != nil {
			return err
		}
	}
	return nil
}

// removeGrantIndexes clears every secondary index entry for a grant and
// (for SESSION_KEY grants) the (granter, grantee) lookup. Used on
// terminal-status transitions; the primary Grants store entry itself is
// removed by the caller. For revoke/decline/expire/complete/fired we
// fully delete the primary record so secondary stores stay in lockstep.
func (k Keeper) removeGrantIndexes(ctx context.Context, g types.Grant) error {
	if err := k.GrantsByGranter.Remove(ctx, collections.Join(g.Granter, g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByGrantee.Remove(ctx, collections.Join(g.Grantee, g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByExpiration.Remove(ctx, collections.Join(g.ExpiresAt.Unix(), g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByTypeAndGranter.Remove(ctx, collections.Join3(int32(g.Type), g.Granter, g.Id)); err != nil {
		return err
	}
	if g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY {
		if err := k.SessionKeyByPair.Remove(ctx, collections.Join(g.Granter, g.Grantee)); err != nil {
			return err
		}
	}
	return nil
}

// deleteGrant removes a grant from the primary store and every secondary
// index, decrementing the per-(granter, type) active count if the grant
// was previously in an active-counting status.
func (k Keeper) deleteGrant(ctx context.Context, g types.Grant) error {
	if activeStatus(g.Status) {
		if err := k.decActiveGrantCount(ctx, g.Granter, g.Type); err != nil {
			return err
		}
	}
	if err := k.removeGrantIndexes(ctx, g); err != nil {
		return err
	}
	return k.Grants.Remove(ctx, g.Id)
}

// GetGrant returns the grant with the given id. Wraps
// `collections.ErrNotFound` as `types.ErrGrantNotFound` so external
// callers (other modules consuming the SessionKeeper interface) get
// a session-typed error without importing the collections package.
func (k Keeper) GetGrant(ctx context.Context, id uint64) (types.Grant, error) {
	g, err := k.Grants.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Grant{}, types.ErrGrantNotFound.Wrapf("id=%d", id)
		}
		return types.Grant{}, err
	}
	return g, nil
}

// ListGrantsByGranter walks every grant owned by `granter`, optionally
// filtered to a single grant type. Pass `GRANT_TYPE_UNSPECIFIED` to
// disable type filtering. Returns grants in insertion order (the
// secondary index is `(granter, id)`).
func (k Keeper) ListGrantsByGranter(
	ctx context.Context,
	granter string,
	filterType types.GrantType,
) ([]types.Grant, error) {
	var out []types.Grant
	rng := collections.NewPrefixedPairRange[string, uint64](granter)
	err := k.GrantsByGranter.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		g, err := k.Grants.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		if filterType != types.GrantType_GRANT_TYPE_UNSPECIFIED && g.Type != filterType {
			return false, nil
		}
		out = append(out, g)
		return false, nil
	})
	return out, err
}

// ListGrantsByGrantee walks every grant payable to `grantee`,
// optionally filtered to a single grant type. Used by the x/commons
// service-hook recovery path (M-svc) to find recurring-pull grants
// targeting a dissolved operator address.
func (k Keeper) ListGrantsByGrantee(
	ctx context.Context,
	grantee string,
	filterType types.GrantType,
) ([]types.Grant, error) {
	var out []types.Grant
	rng := collections.NewPrefixedPairRange[string, uint64](grantee)
	err := k.GrantsByGrantee.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		g, err := k.Grants.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		if filterType != types.GrantType_GRANT_TYPE_UNSPECIFIED && g.Type != filterType {
			return false, nil
		}
		out = append(out, g)
		return false, nil
	})
	return out, err
}

// CountActiveGrants returns the number of currently-active grants for
// (granter, type). O(1) via the ActiveGrantCountByType counter.
func (k Keeper) CountActiveGrants(ctx context.Context, granter string, t types.GrantType) (uint32, error) {
	key := collections.Join(granter, int32(t))
	count, err := k.ActiveGrantCountByType.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func (k Keeper) incActiveGrantCount(ctx context.Context, granter string, t types.GrantType) error {
	key := collections.Join(granter, int32(t))
	count, err := k.ActiveGrantCountByType.Get(ctx, key)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	count++
	return k.ActiveGrantCountByType.Set(ctx, key, count)
}

func (k Keeper) decActiveGrantCount(ctx context.Context, granter string, t types.GrantType) error {
	key := collections.Join(granter, int32(t))
	count, err := k.ActiveGrantCountByType.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Defensive: treat missing counter as zero.
			return nil
		}
		return err
	}
	if count == 0 {
		return nil
	}
	count--
	if count == 0 {
		return k.ActiveGrantCountByType.Remove(ctx, key)
	}
	return k.ActiveGrantCountByType.Set(ctx, key, count)
}

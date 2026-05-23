package simulation

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// ─── find helpers ────────────────────────────────────────────────────────────

// walkSessionGrants yields a projected Session view for every active
// SESSION_KEY grant. Filter is an optional predicate; when nil all are
// included.
func walkSessionGrants(ctx sdk.Context, k keeper.Keeper, filter func(types.Grant, types.SessionKeyPayload) bool) []types.Session {
	var sessions []types.Session
	_ = k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
		if g.Type != types.GrantType_GRANT_TYPE_SESSION_KEY {
			return false, nil
		}
		if g.Status != types.GrantStatus_GRANT_STATUS_ACTIVE {
			return false, nil
		}
		sk := g.GetSessionKey()
		if sk == nil {
			return false, nil
		}
		if filter != nil && !filter(g, *sk) {
			return false, nil
		}
		sessions = append(sessions, types.Session{
			Granter:         g.Granter,
			Grantee:         g.Grantee,
			AllowedMsgTypes: sk.AllowedMsgTypes,
			SpendLimit:      sk.SpendLimit,
			Spent:           sk.Spent,
			Expiration:      g.ExpiresAt,
			CreatedAt:       g.CreatedAt,
			LastUsedAt:      sk.LastUsedAt,
			ExecCount:       sk.ExecCount,
			MaxExecCount:    sk.MaxExecCount,
		})
		return false, nil
	})
	return sessions
}

// findRandomSession walks all sessions and returns one at random.
func findRandomSession(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Session, bool) {
	sessions := walkSessionGrants(ctx, k, nil)
	if len(sessions) == 0 {
		return types.Session{}, false
	}
	return sessions[r.Intn(len(sessions))], true
}

// findNonExpiredSession walks sessions and returns a random non-expired one.
func findNonExpiredSession(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Session, bool) {
	blockTime := ctx.BlockTime()
	sessions := walkSessionGrants(ctx, k, func(g types.Grant, _ types.SessionKeyPayload) bool {
		return g.ExpiresAt.After(blockTime)
	})
	if len(sessions) == 0 {
		return types.Session{}, false
	}
	return sessions[r.Intn(len(sessions))], true
}

// findExecableSession finds a non-expired session with zero spend limit (to avoid
// ante handler spend-budget checks) and remaining exec budget.
func findExecableSession(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Session, bool) {
	blockTime := ctx.BlockTime()
	sessions := walkSessionGrants(ctx, k, func(g types.Grant, sk types.SessionKeyPayload) bool {
		return g.ExpiresAt.After(blockTime) &&
			!sk.SpendLimit.IsPositive() &&
			(sk.MaxExecCount == 0 || sk.ExecCount < sk.MaxExecCount)
	})
	if len(sessions) == 0 {
		return types.Session{}, false
	}
	return sessions[r.Intn(len(sessions))], true
}

// findSessionWithExecBudget returns a random non-expired session with remaining exec budget.
func findSessionWithExecBudget(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Session, bool) {
	blockTime := ctx.BlockTime()
	sessions := walkSessionGrants(ctx, k, func(g types.Grant, sk types.SessionKeyPayload) bool {
		return g.ExpiresAt.After(blockTime) && (sk.MaxExecCount == 0 || sk.ExecCount < sk.MaxExecCount)
	})
	if len(sessions) == 0 {
		return types.Session{}, false
	}
	return sessions[r.Intn(len(sessions))], true
}

// countGranterSessions counts active SESSION_KEY grants for a granter via the
// O(1) ActiveGrantCountByType counter.
func countGranterSessions(ctx sdk.Context, k keeper.Keeper, granter string) uint64 {
	count, _ := k.CountActiveGrants(ctx, granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	return uint64(count)
}

// countGranterGrantsByType is the per-type variant used by the
// RecurringPull / SpendingAllowance / ScheduledOneshot sims to check
// the per-(granter, type) cap before attempting create.
func countGranterGrantsByType(ctx sdk.Context, k keeper.Keeper, granter string, t types.GrantType) uint64 {
	count, _ := k.CountActiveGrants(ctx, granter, t)
	return uint64(count)
}

// walkGrantsByType returns every grant of the given type, optionally
// filtered. Used by the non-SESSION_KEY simulations to pick a random
// existing grant for claim/pull/revoke/decline.
func walkGrantsByType(
	ctx sdk.Context, k keeper.Keeper, t types.GrantType,
	filter func(types.Grant) bool,
) []types.Grant {
	var out []types.Grant
	_ = k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
		if g.Type != t {
			return false, nil
		}
		if filter != nil && !filter(g) {
			return false, nil
		}
		out = append(out, g)
		return false, nil
	})
	return out
}

// findRandomGrantOfType picks a random grant of the given type matching
// the optional filter. Returns ok=false if no grants match.
func findRandomGrantOfType(
	r *rand.Rand, ctx sdk.Context, k keeper.Keeper, t types.GrantType,
	filter func(types.Grant) bool,
) (types.Grant, bool) {
	grants := walkGrantsByType(ctx, k, t, filter)
	if len(grants) == 0 {
		return types.Grant{}, false
	}
	return grants[r.Intn(len(grants))], true
}

// ─── get-or-create helpers ──────────────────────────────────────────────────

// getOrCreateSession finds an existing non-expired session, or creates one
// directly via keeper. Returns the session, granter sim account, grantee sim account.
// When forExec is true, only zero-spend-limit sessions with remaining exec
// budget are considered (avoids ante handler spend-limit failures at delivery).
func getOrCreateSession(
	r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account, forExec bool,
) (types.Session, simtypes.Account, simtypes.Account, error) {
	// Try to find an existing session owned by a sim account
	var session types.Session
	var found bool
	if forExec {
		session, found = findExecableSession(r, ctx, k)
	} else {
		session, found = findNonExpiredSession(r, ctx, k)
	}
	if found {
		granterAcc, gFound := findSimAccount(accs, session.Granter)
		granteeAcc, tFound := findSimAccount(accs, session.Grantee)
		if gFound && tFound {
			return session, granterAcc, granteeAcc, nil
		}
	}

	// No usable session found — create one directly via keeper
	if len(accs) < 2 {
		return types.Session{}, simtypes.Account{}, simtypes.Account{}, fmt.Errorf("need at least 2 accounts")
	}

	// Pick two different accounts
	perm := r.Perm(len(accs))
	granterAcc := accs[perm[0]]
	granteeAcc := accs[perm[1]]

	granter := granterAcc.Address.String()
	grantee := granteeAcc.Address.String()

	// Check no existing session for this pair
	_, err := k.GetSession(ctx, granter, grantee)
	if err == nil {
		// Session already exists for this pair — try one more pair
		if len(accs) >= 3 {
			granteeAcc = accs[perm[2]]
			grantee = granteeAcc.Address.String()
			_, err = k.GetSession(ctx, granter, grantee)
			if err == nil {
				return types.Session{}, simtypes.Account{}, simtypes.Account{}, fmt.Errorf("session already exists for all attempted pairs")
			}
		} else {
			return types.Session{}, simtypes.Account{}, simtypes.Account{}, fmt.Errorf("session already exists")
		}
	}

	// Get allowed msg types from params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Session{}, simtypes.Account{}, simtypes.Account{}, fmt.Errorf("failed to get params: %w", err)
	}

	allowedMsgTypes := randomSubset(r, params.AllowedMsgTypes, 5)
	if len(allowedMsgTypes) == 0 {
		allowedMsgTypes = []string{"/sparkdream.blog.v1.MsgCreatePost"}
	}

	expiration := ctx.BlockTime().Add(2 * time.Hour)
	zeroCoin := sdk.NewInt64Coin(sdk.DefaultBondDenom, 0)
	// Use zero spend limit so the SessionFeeDecorator skips fee delegation.
	// With a positive spend limit, the ante handler would try to deduct fees
	// from the granter which may not have sufficient balance in simulation.
	spendLimit := zeroCoin

	session = types.Session{
		Granter:         granter,
		Grantee:         grantee,
		AllowedMsgTypes: allowedMsgTypes,
		SpendLimit:      spendLimit,
		Spent:           zeroCoin,
		Expiration:      expiration,
		CreatedAt:       ctx.BlockTime(),
		LastUsedAt:      ctx.BlockTime(),
		ExecCount:       0,
		MaxExecCount:    100, // finite cap (the chain rejects 0 at create-time)
	}

	// Write the backing SESSION_KEY grant directly via the keeper helper.
	if err := keeperWriteSessionKeyGrant(ctx, k, session); err != nil {
		return types.Session{}, simtypes.Account{}, simtypes.Account{}, err
	}

	return session, granterAcc, granteeAcc, nil
}

// keeperWriteSessionKeyGrant is a simulation-only direct-write helper that
// allocates a fresh grant id, constructs a SESSION_KEY Grant from the
// legacy Session shape, and persists it via the keeper. Mirrors the
// MsgCreateSession handler without re-running validation.
func keeperWriteSessionKeyGrant(ctx sdk.Context, k keeper.Keeper, s types.Session) error {
	id, err := k.GrantSeq.Next(ctx)
	if err != nil {
		return err
	}
	id++ // 1-indexed; match nextGrantID convention
	g := types.Grant{
		Id:        id,
		Granter:   s.Granter,
		Grantee:   s.Grantee,
		Type:      types.GrantType_GRANT_TYPE_SESSION_KEY,
		Status:    types.GrantStatus_GRANT_STATUS_ACTIVE,
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.Expiration,
		Payload: &types.Grant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: s.AllowedMsgTypes,
				SpendLimit:      s.SpendLimit,
				Spent:           s.Spent,
				MaxExecCount:    s.MaxExecCount,
				ExecCount:       s.ExecCount,
				LastUsedAt:      s.LastUsedAt,
			},
		},
	}
	if err := k.Grants.Set(ctx, id, g); err != nil {
		return err
	}
	if err := k.GrantsByGranter.Set(ctx, collections.Join(s.Granter, id)); err != nil {
		return err
	}
	if err := k.GrantsByGrantee.Set(ctx, collections.Join(s.Grantee, id)); err != nil {
		return err
	}
	if err := k.GrantsByExpiration.Set(ctx, collections.Join(s.Expiration.Unix(), id)); err != nil {
		return err
	}
	if err := k.GrantsByTypeAndGranter.Set(ctx, collections.Join3(int32(types.GrantType_GRANT_TYPE_SESSION_KEY), s.Granter, id)); err != nil {
		return err
	}
	if err := k.SessionKeyByPair.Set(ctx, collections.Join(s.Granter, s.Grantee), id); err != nil {
		return err
	}
	// Bump active-grant count to keep the cap invariant consistent.
	key := collections.Join(s.Granter, int32(types.GrantType_GRANT_TYPE_SESSION_KEY))
	cur, err := k.ActiveGrantCountByType.Get(ctx, key)
	if err != nil && !isNotFound(err) {
		return err
	}
	return k.ActiveGrantCountByType.Set(ctx, key, cur+1)
}

// ─── utility helpers ────────────────────────────────────────────────────────

// randomSubset returns a random non-empty subset of the given slice.
func randomSubset(r *rand.Rand, items []string, maxItems int) []string {
	if len(items) == 0 {
		return nil
	}
	n := r.Intn(len(items)) + 1 // at least 1
	if n > maxItems {
		n = maxItems
	}
	perm := r.Perm(len(items))
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = items[perm[i]]
	}
	return result
}

// findSimAccount finds the simulation account matching the given bech32 address.
func findSimAccount(accs []simtypes.Account, addr string) (simtypes.Account, bool) {
	accAddr, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return simtypes.Account{}, false
	}
	return simtypes.FindAccount(accs, accAddr)
}

func isNotFound(err error) bool {
	return err != nil && errors.Is(err, collections.ErrNotFound)
}

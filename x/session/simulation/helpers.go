package simulation

import (
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

// findRandomSession walks all sessions and returns one at random.
func findRandomSession(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Session, bool) {
	var sessions []types.Session
	_ = k.Sessions.Walk(ctx, nil, func(_ collections.Pair[string, string], s types.Session) (bool, error) {
		sessions = append(sessions, s)
		return false, nil
	})
	if len(sessions) == 0 {
		return types.Session{}, false
	}
	return sessions[r.Intn(len(sessions))], true
}

// findNonExpiredSession walks sessions and returns a random non-expired one.
func findNonExpiredSession(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Session, bool) {
	blockTime := ctx.BlockTime()
	var sessions []types.Session
	_ = k.Sessions.Walk(ctx, nil, func(_ collections.Pair[string, string], s types.Session) (bool, error) {
		if s.Expiration.After(blockTime) {
			sessions = append(sessions, s)
		}
		return false, nil
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
	var sessions []types.Session
	_ = k.Sessions.Walk(ctx, nil, func(_ collections.Pair[string, string], s types.Session) (bool, error) {
		if s.Expiration.After(blockTime) &&
			!s.SpendLimit.IsPositive() &&
			(s.MaxExecCount == 0 || s.ExecCount < s.MaxExecCount) {
			sessions = append(sessions, s)
		}
		return false, nil
	})
	if len(sessions) == 0 {
		return types.Session{}, false
	}
	return sessions[r.Intn(len(sessions))], true
}

// findSessionWithExecBudget returns a random non-expired session with remaining exec budget.
func findSessionWithExecBudget(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Session, bool) {
	blockTime := ctx.BlockTime()
	var sessions []types.Session
	_ = k.Sessions.Walk(ctx, nil, func(_ collections.Pair[string, string], s types.Session) (bool, error) {
		if s.Expiration.After(blockTime) && (s.MaxExecCount == 0 || s.ExecCount < s.MaxExecCount) {
			sessions = append(sessions, s)
		}
		return false, nil
	})
	if len(sessions) == 0 {
		return types.Session{}, false
	}
	return sessions[r.Intn(len(sessions))], true
}

// countGranterSessions counts active sessions for a granter.
func countGranterSessions(ctx sdk.Context, k keeper.Keeper, granter string) uint64 {
	var count uint64
	rng := collections.NewPrefixedPairRange[string, string](granter)
	_ = k.SessionsByGranter.Walk(ctx, rng, func(_ collections.Pair[string, string]) (bool, error) {
		count++
		return false, nil
	})
	return count
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
	zeroCoin := sdk.NewInt64Coin("uspark", 0)
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
		MaxExecCount:    0, // unlimited
	}

	// Store session + all indexes
	key := collections.Join(granter, grantee)
	if err := k.Sessions.Set(ctx, key, session); err != nil {
		return types.Session{}, simtypes.Account{}, simtypes.Account{}, err
	}
	if err := k.SessionsByGranter.Set(ctx, collections.Join(granter, grantee)); err != nil {
		return types.Session{}, simtypes.Account{}, simtypes.Account{}, err
	}
	if err := k.SessionsByGrantee.Set(ctx, collections.Join(grantee, granter)); err != nil {
		return types.Session{}, simtypes.Account{}, simtypes.Account{}, err
	}
	if err := k.SessionsByExpiration.Set(ctx, collections.Join3(expiration.Unix(), granter, grantee)); err != nil {
		return types.Session{}, simtypes.Account{}, simtypes.Account{}, err
	}

	return session, granterAcc, granteeAcc, nil
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

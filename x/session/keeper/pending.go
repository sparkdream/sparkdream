package keeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// Grantee consent for access-class grants (SES-102). See types/pending.go
// for the rationale and docs/x-session-spec.md §31 for the lifecycle.

// pendingDeadline is when a PENDING grant stops waiting: the compiled TTL
// after creation, or the grant's own expiry if that comes first. Derived
// from the grant alone so a re-key or a genesis import rebuilds the index.
func pendingDeadline(g types.Grant) int64 {
	deadline := g.CreatedAt.Unix() + types.PendingGrantTTLSeconds
	if exp := g.ExpiresAt.Unix(); exp < deadline {
		deadline = exp
	}
	return deadline
}

// requiresConsent reports whether a grant of type t from granter to grantee
// must wait for the grantee's acceptance: access-class grants from a granter
// the grantee has not allowed. Self-grants are refused earlier
// (ErrSelfDelegation); the check is restated so the rule holds on its own.
// A module-account grantee is refused outright (ErrModuleAccountGrantee,
// SES-205): it can sign neither an acceptance nor an exec, so the grant
// could only wait out its TTL and forfeit the deposit.
func (k Keeper) requiresConsent(ctx context.Context, granter, grantee string, t types.GrantType) (bool, error) {
	if !types.IsAccessGrantType(t) || granter == grantee {
		return false, nil
	}
	isModule, err := k.isModuleAccount(ctx, grantee)
	if err != nil {
		return false, err
	}
	if isModule {
		return false, errorsmod.Wrapf(types.ErrModuleAccountGrantee, "grantee %s", grantee)
	}
	allowed, err := k.IsGranterAllowed(ctx, grantee, granter)
	if err != nil {
		return false, err
	}
	return !allowed, nil
}

// isModuleAccount reports whether addr is a registered module account.
func (k Keeper) isModuleAccount(ctx context.Context, addr string) (bool, error) {
	acc, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false, err
	}
	if a := k.authKeeper.GetAccount(ctx, acc); a != nil {
		if _, isModule := a.(sdk.ModuleAccountI); isModule {
			return true, nil
		}
	}
	return false, nil
}

// IsGranterAllowed reports whether grantee has allowed granter.
func (k Keeper) IsGranterAllowed(ctx context.Context, grantee, granter string) (bool, error) {
	return k.AllowedGranters.Has(ctx, collections.Join(grantee, granter))
}

// setAllowedGranter writes the (grantee, granter) consent entry and its
// reverse index.
func (k Keeper) setAllowedGranter(ctx context.Context, grantee, granter string) error {
	if err := k.AllowedGranters.Set(ctx, collections.Join(grantee, granter)); err != nil {
		return err
	}
	return k.AllowedGranteesByGranter.Set(ctx, collections.Join(granter, grantee))
}

// removeAllowedGranter clears the (grantee, granter) consent entry and its
// reverse index. Removing a missing key is a no-op.
func (k Keeper) removeAllowedGranter(ctx context.Context, grantee, granter string) error {
	if err := k.AllowedGranters.Remove(ctx, collections.Join(grantee, granter)); err != nil {
		return err
	}
	return k.AllowedGranteesByGranter.Remove(ctx, collections.Join(granter, grantee))
}

// CountPendingGrants returns how many PENDING grants granter holds.
func (k Keeper) CountPendingGrants(ctx context.Context, granter string) (uint32, error) {
	n, err := k.PendingGrantCountByGranter.Get(ctx, granter)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

func (k Keeper) incPendingGrantCount(ctx context.Context, granter string) error {
	n, err := k.CountPendingGrants(ctx, granter)
	if err != nil {
		return err
	}
	return k.PendingGrantCountByGranter.Set(ctx, granter, n+1)
}

func (k Keeper) decPendingGrantCount(ctx context.Context, granter string) error {
	n, err := k.CountPendingGrants(ctx, granter)
	if err != nil {
		return err
	}
	if n <= 1 {
		return k.PendingGrantCountByGranter.Remove(ctx, granter)
	}
	return k.PendingGrantCountByGranter.Set(ctx, granter, n-1)
}

// markPending turns a freshly validated access-class grant into a PENDING
// one before it is written: enforces the per-granter pending cap and
// escrows params.pending_grant_deposit from the granter into the module
// account. The caller writes the grant afterwards; a bank failure here
// rolls the whole creation back.
func (k Keeper) markPending(ctx context.Context, params types.Params, grant *types.Grant) error {
	n, err := k.CountPendingGrants(ctx, grant.Granter)
	if err != nil {
		return err
	}
	if n >= types.MaxPendingGrantsPerGranter {
		return errorsmod.Wrapf(types.ErrMaxPendingGrantsExceeded, "granter %s holds %d pending grants (max %d)",
			grant.Granter, n, types.MaxPendingGrantsPerGranter)
	}
	if params.PendingGrantDeposit > 0 {
		dep := sdk.NewCoin(k.BondDenom(ctx), sdkmath.NewIntFromUint64(params.PendingGrantDeposit))
		granterAddr, err := k.addressCodec.StringToBytes(grant.Granter)
		if err != nil {
			return err
		}
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, granterAddr, types.ModuleName, sdk.NewCoins(dep)); err != nil {
			return errorsmod.Wrap(err, "pending grant deposit")
		}
		if err := k.PendingGrantDeposit.Set(ctx, grant.Id, dep); err != nil {
			return err
		}
	}
	grant.Status = types.GrantStatus_GRANT_STATUS_PENDING
	return nil
}

// refundPendingDeposit returns a PENDING grant's escrowed deposit to
// recipient (the granter, or a migrating granter's new address) and clears
// the escrow row. Returns the zero coin when the grant holds no deposit.
func (k Keeper) refundPendingDeposit(ctx context.Context, grant types.Grant, recipient string) (sdk.Coin, error) {
	zero := sdk.NewCoin(k.BondDenom(ctx), sdkmath.ZeroInt())
	if grant.Status != types.GrantStatus_GRANT_STATUS_PENDING {
		return zero, nil
	}
	dep, err := k.PendingGrantDeposit.Get(ctx, grant.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return zero, nil
		}
		return zero, err
	}
	if !dep.IsZero() {
		to, err := k.addressCodec.StringToBytes(recipient)
		if err != nil {
			return zero, err
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, sdk.NewCoins(dep)); err != nil {
			return zero, errorsmod.Wrap(err, "pending deposit refund failed")
		}
	}
	if err := k.PendingGrantDeposit.Remove(ctx, grant.Id); err != nil {
		return zero, err
	}
	return dep, nil
}

// forfeitPendingDeposit sends an expired PENDING grant's deposit to the
// community pool (or the fee collector when no distribution keeper is
// wired, or if funding the pool fails) and clears the escrow row. Runs in
// the EndBlocker, so failures are logged, never returned: the grant is
// deleted either way and no escrow row is left behind.
func (k Keeper) forfeitPendingDeposit(ctx sdk.Context, grant types.Grant) sdk.Coin {
	zero := sdk.NewCoin(k.BondDenom(ctx), sdkmath.ZeroInt())
	dep, err := k.PendingGrantDeposit.Get(ctx, grant.Id)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			ctx.Logger().Error("forfeitPendingDeposit: PendingGrantDeposit.Get failed", "grant_id", grant.Id, "err", err)
		}
		return zero
	}
	if !dep.IsZero() {
		funded := false
		if k.late.distrKeeper != nil {
			if err := k.late.distrKeeper.FundCommunityPool(ctx, sdk.NewCoins(dep), k.authKeeper.GetModuleAddress(types.ModuleName)); err != nil {
				ctx.Logger().Error("pending deposit forfeit to community pool failed; sending to fee collector",
					"grant_id", grant.Id, "err", err)
			} else {
				funded = true
			}
		}
		if !funded {
			if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, feeCollectorModule, sdk.NewCoins(dep)); err != nil {
				ctx.Logger().Error("pending deposit forfeit failed", "grant_id", grant.Id, "err", err)
			}
		}
	}
	if err := k.PendingGrantDeposit.Remove(ctx, grant.Id); err != nil {
		ctx.Logger().Error("forfeitPendingDeposit: PendingGrantDeposit.Remove failed", "grant_id", grant.Id, "err", err)
	}
	return dep
}

// acceptPendingGrant is the one acceptance path (explicit MsgAcceptGrant
// and the implicit first MsgExecSession): the grant leaves PENDING for
// ACTIVE, which means it now has to pass the checks an active grant passed
// at creation (the grantee's class cap, the granter's per-type cap), joins
// the grantee indexes and the expiration index, the granter becomes an
// allowed granter of the grantee, and the deposit goes back to the granter.
// On a refused acceptance the grant stays PENDING and nothing moves.
func (k Keeper) acceptPendingGrant(ctx context.Context, params types.Params, grant types.Grant) (types.Grant, sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	zero := sdk.NewCoin(k.BondDenom(ctx), sdkmath.ZeroInt())
	if err := k.checkPendingAcceptance(ctx, params, grant); err != nil {
		return grant, zero, err
	}

	refund, err := k.refundPendingDeposit(ctx, grant, grant.Granter)
	if err != nil {
		return grant, zero, err
	}
	// Re-index: drop the pending-side entries, write the active ones.
	if err := k.deleteGrant(ctx, grant); err != nil {
		return grant, zero, err
	}
	grant.Status = types.GrantStatus_GRANT_STATUS_ACTIVE
	if err := k.writeGrant(ctx, grant); err != nil {
		return grant, zero, err
	}
	if err := k.setAllowedGranter(ctx, grant.Grantee, grant.Granter); err != nil {
		return grant, zero, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"grant_accepted",
		sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("type", types.GrantTypeEventValue(grant.Type)),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("refund_amount", refund.String()),
	))
	return grant, refund, nil
}

// checkPendingAcceptance is the read-only half of acceptance: the grant
// must be PENDING and unexpired (the pending pass is capped, so an expired
// grant can linger a few blocks: never activate one), and it must pass the
// checks creation skipped: the grantee's class cap and the granter's
// per-type cap.
func (k Keeper) checkPendingAcceptance(ctx context.Context, params types.Params, grant types.Grant) error {
	if grant.Status != types.GrantStatus_GRANT_STATUS_PENDING {
		return errorsmod.Wrapf(types.ErrGrantNotPending, "grant %d is %s", grant.Id, grant.Status)
	}
	if !grant.ExpiresAt.After(sdk.UnwrapSDKContext(ctx).BlockTime()) {
		return errorsmod.Wrapf(types.ErrGrantExpired, "grant %d", grant.Id)
	}
	if err := k.checkGranteeCapacity(ctx, grant.Grantee, grant.Type); err != nil {
		return err
	}
	switch grant.Type {
	case types.GrantType_GRANT_TYPE_SESSION_KEY:
		n, err := k.CountActiveGrants(ctx, grant.Granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
		if err != nil {
			return err
		}
		if uint64(n) >= params.MaxSessionsPerGranter {
			return types.ErrMaxSessionsExceeded
		}
	case types.GrantType_GRANT_TYPE_SIGN_IN:
		n, err := k.CountActiveGrants(ctx, grant.Granter, types.GrantType_GRANT_TYPE_SIGN_IN)
		if err != nil {
			return err
		}
		if n >= types.MaxSignInGrantsPerGranter {
			return types.ErrMaxSignInGrantsExceeded.Wrapf("max %d", types.MaxSignInGrantsPerGranter)
		}
	}
	return nil
}

// CheckSessionAcceptable is the ante handler's view of a PENDING session
// key (SES-203): for the (granter, grantee) session key it returns nil when
// the key is not pending (an active key needs no acceptance) or when the
// grantee's first MsgExecSession would accept it, and the acceptance error
// otherwise. The fee decorator refuses such an exec before any fee moves,
// so a pending key can spend the granter's spend_limit only on execs that
// do accept it. Costs the same reads acceptance itself does (O(cap) at
// most) and writes nothing.
func (k Keeper) CheckSessionAcceptable(ctx context.Context, granter, grantee string) error {
	id, err := k.SessionKeyByPair.Get(ctx, collections.Join(granter, grantee))
	if err != nil {
		return types.ErrSessionNotFound
	}
	grant, err := k.Grants.Get(ctx, id)
	if err != nil {
		return types.ErrSessionNotFound
	}
	if grant.Status != types.GrantStatus_GRANT_STATUS_PENDING {
		return nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	return k.checkPendingAcceptance(ctx, params, grant)
}

// expirePendingPass drops PENDING grants whose deadline (TTL or own expiry)
// has passed, forfeiting the deposit. Range-walks PendingGrantsByDeadline
// up to block time, capped at `cap` per block like the other passes. An
// entry whose grant is gone or no longer pending is stale and is dropped.
func (k Keeper) expirePendingPass(ctx sdk.Context, blockTime time.Time, cap int) error {
	type entry struct {
		deadline int64
		id       uint64
	}
	var due []entry
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](blockTime.Unix()+1, 0))
	err := k.PendingGrantsByDeadline.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if len(due) >= cap {
			return true, nil
		}
		due = append(due, entry{deadline: key.K1(), id: key.K2()})
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, e := range due {
		indexKey := collections.Join(e.deadline, e.id)
		grant, err := k.Grants.Get(ctx, e.id)
		if err != nil || grant.Status != types.GrantStatus_GRANT_STATUS_PENDING || pendingDeadline(grant) != e.deadline {
			if err := k.PendingGrantsByDeadline.Remove(ctx, indexKey); err != nil {
				ctx.Logger().Error("expirePendingPass: PendingGrantsByDeadline.Remove failed", "err", err)
			}
			continue
		}
		forfeited := k.forfeitPendingDeposit(ctx, grant)
		if err := k.deleteGrant(ctx, grant); err != nil {
			ctx.Logger().Error("pending grant expiry failed", "grant_id", e.id, "err", err)
			continue
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"grant_expired",
			sdk.NewAttribute("granter", grant.Granter),
			sdk.NewAttribute("grantee", grant.Grantee),
			sdk.NewAttribute("type", types.GrantTypeEventValue(grant.Type)),
			sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
			sdk.NewAttribute("status", "pending"),
			sdk.NewAttribute("forfeited_amount", forfeited.String()),
		))
	}
	return nil
}

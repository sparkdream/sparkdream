package keeper

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// CreateGrant is the unified entrypoint for creating any of the four
// typed grants. The handler:
//  1. Runs cross-type validation (self-delegation, expiration, note
//     length, lifetime cap).
//  2. Dispatches on the payload oneof to per-type validation.
//  3. Allocates a grant id and persists the grant via writeGrant, which
//     fans out to every secondary index and bumps the active-grant
//     counter.
//
// SCHEDULED_ONESHOT and SPENDING_ALLOWANCE land in P4/P5; for now they
// return ErrInvalidPayload so the umbrella is wire-stable but P3 only
// activates the two implemented paths.
func (k msgServer) CreateGrant(ctx context.Context, msg *types.MsgCreateGrant) (*types.MsgCreateGrantResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if err := k.validateGrantCommon(ctx, params, blockTime, msg.Granter, msg.Grantee, msg.ExpiresAt, msg.Note); err != nil {
		return nil, err
	}

	// Allocate ID upfront so per-type validators can reference it for
	// logging / events if they need to.
	id, err := k.nextGrantID(ctx)
	if err != nil {
		return nil, err
	}

	grant := types.Grant{
		Id:        id,
		Granter:   msg.Granter,
		Grantee:   msg.Grantee,
		Status:    types.GrantStatus_GRANT_STATUS_ACTIVE,
		CreatedAt: blockTime,
		ExpiresAt: msg.ExpiresAt,
		Note:      msg.Note,
	}

	switch payload := msg.Payload.(type) {
	case *types.MsgCreateGrant_SessionKey:
		sk, err := k.validateSessionKeyPayload(ctx, params, blockTime, msg.Granter, msg.Grantee, msg.ExpiresAt, payload.SessionKey)
		if err != nil {
			return nil, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_SESSION_KEY
		grant.Payload = &types.Grant_SessionKey{SessionKey: sk}

	case *types.MsgCreateGrant_RecurringPull:
		rp, err := k.validateRecurringPullPayload(ctx, params, blockTime, msg.Granter, msg.ExpiresAt, payload.RecurringPull)
		if err != nil {
			return nil, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_RECURRING_PULL
		grant.Payload = &types.Grant_RecurringPull{RecurringPull: rp}

	case *types.MsgCreateGrant_SpendingAllowance:
		sa, err := k.validateSpendingAllowancePayload(ctx, params, blockTime, msg.Granter, payload.SpendingAllowance)
		if err != nil {
			return nil, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE
		grant.Payload = &types.Grant_SpendingAllowance{SpendingAllowance: sa}

	case *types.MsgCreateGrant_ScheduledOneshot:
		so, err := k.validateScheduledOneshotPayload(ctx, params, blockTime, msg.Granter, msg.ExpiresAt, payload.ScheduledOneshot)
		if err != nil {
			return nil, err
		}
		// Compute and escrow the deposit BEFORE persisting the grant so
		// a SendCoinsFromAccountToModule failure rolls back the grant
		// creation cleanly.
		deposit, err := computeOneshotDeposit(params, so.Action)
		if err != nil {
			return nil, err
		}
		depositCoin := sdk.NewCoin(k.BondDenom(ctx), deposit)
		granterAddr, err := k.addressCodec.StringToBytes(msg.Granter)
		if err != nil {
			return nil, err
		}
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, granterAddr, types.ModuleName, sdk.NewCoins(depositCoin)); err != nil {
			return nil, err
		}
		if err := k.OneshotGasDeposit.Set(ctx, id, depositCoin); err != nil {
			return nil, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT
		grant.Payload = &types.Grant_ScheduledOneshot{ScheduledOneshot: so}

	default:
		return nil, types.ErrInvalidPayload.Wrap("payload oneof must be set")
	}

	if err := k.writeGrant(ctx, grant); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"grant_created",
		sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("type", grant.Type.String()),
		sdk.NewAttribute("granter", msg.Granter),
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("expires_at", msg.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000000Z")),
	))

	return &types.MsgCreateGrantResponse{GrantId: id}, nil
}

// validateSessionKeyPayload reuses the SESSION_KEY validation already
// proven in MsgCreateSession. Lifted here so the umbrella handler and
// the legacy MsgCreateSession can share one definition. Returns the
// normalized payload to persist.
func (k Keeper) validateSessionKeyPayload(
	ctx context.Context,
	params types.Params,
	blockTime time.Time,
	granter, grantee string,
	expiresAt time.Time,
	p *types.SessionKeyPayload,
) (*types.SessionKeyPayload, error) {
	if p == nil {
		return nil, types.ErrInvalidPayload
	}

	// One active SESSION_KEY grant per (granter, grantee) pair.
	if has, err := k.SessionKeyByPair.Has(ctx, collections.Join(granter, grantee)); err != nil {
		return nil, err
	} else if has {
		return nil, types.ErrSessionExists
	}

	// Per-granter session cap.
	count, err := k.CountActiveGrants(ctx, granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	if err != nil {
		return nil, err
	}
	if uint64(count) >= params.MaxSessionsPerGranter {
		return nil, types.ErrMaxSessionsExceeded
	}

	// Allowed-msg-types subset checks.
	if uint64(len(p.AllowedMsgTypes)) > params.MaxMsgTypesPerSession {
		return nil, types.ErrMaxMsgTypesExceeded
	}
	activeSet := make(map[string]bool, len(params.AllowedMsgTypes))
	for _, t := range params.AllowedMsgTypes {
		activeSet[t] = true
	}
	for _, t := range p.AllowedMsgTypes {
		// MsgRevokeGrant is delegable iff allow_self_revoke is set
		// (Rev 2 §7.2). The msg server further restricts the revoke
		// target to grants of the same granter.
		if t == types.MsgRevokeGrantTypeURL {
			if !p.AllowSelfRevoke {
				return nil, types.ErrSelfRevokeNotPermitted
			}
			// MsgRevokeGrant doesn't need to appear in
			// params.allowed_msg_types — the allow_self_revoke flag is
			// the gate. Skip the allowlist + denylist checks for it.
			continue
		}
		if !activeSet[t] {
			return nil, types.ErrMsgTypeNotInAllowlist.Wrapf("type: %s", t)
		}
		if types.NonDelegableSessionMsgs[t] {
			return nil, types.ErrMsgTypeForbidden.Wrapf("type: %s", t)
		}
	}

	// Expiration upper bound — SessionKey gets its own narrower cap on
	// top of the universal max_grant_lifetime_seconds.
	maxExp := blockTime.Add(params.MaxExpiration)
	if expiresAt.After(maxExp) {
		return nil, types.ErrExpirationTooLong
	}

	// Spend limit sanity.
	if !p.SpendLimit.IsPositive() {
		return nil, types.ErrSpendLimitRequired
	}
	if p.SpendLimit.Amount.GT(params.MaxSpendLimitAmount) {
		return nil, types.ErrSpendLimitTooHigh
	}
	if p.SpendLimit.Denom != k.BondDenom(ctx) {
		return nil, types.ErrInvalidDenom
	}

	// Exec count cap.
	if p.MaxExecCount == 0 {
		return nil, types.ErrMaxExecCountRequired
	}
	if p.MaxExecCount > params.MaxExecCount {
		return nil, types.ErrMaxExecCountTooHigh
	}

	zero := sdk.NewInt64Coin(k.BondDenom(ctx), 0)
	out := *p
	out.Spent = zero
	out.ExecCount = 0
	out.LastUsedAt = blockTime
	return &out, nil
}

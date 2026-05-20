package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// CreateSession is the legacy session-key creator. Internally writes a
// SESSION_KEY-type Grant; the wire-level message shape and external
// behavior (per-(granter, grantee) uniqueness, max_sessions_per_granter,
// allowlist subset rules, expiration cap, spend-limit denom/positivity
// checks, exec-count cap) are preserved.
func (k msgServer) CreateSession(ctx context.Context, msg *types.MsgCreateSession) (*types.MsgCreateSessionResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 1. Cannot delegate to self
	if msg.Granter == msg.Grantee {
		return nil, types.ErrSelfDelegation
	}

	// 2. No existing active SESSION_KEY grant for this pair.
	has, err := k.SessionKeyByPair.Has(ctx, collections.Join(msg.Granter, msg.Grantee))
	if err != nil {
		return nil, err
	}
	if has {
		return nil, types.ErrSessionExists
	}

	// 3. Per-granter session cap (counts only SESSION_KEY grants).
	count, err := k.CountActiveGrants(ctx, msg.Granter, types.GrantType_GRANT_TYPE_SESSION_KEY)
	if err != nil {
		return nil, err
	}
	if uint64(count) >= params.MaxSessionsPerGranter {
		return nil, types.ErrMaxSessionsExceeded
	}

	// 4. Message types count limit
	if uint64(len(msg.AllowedMsgTypes)) > params.MaxMsgTypesPerSession {
		return nil, types.ErrMaxMsgTypesExceeded
	}

	// 5. Every type must be in active allowlist
	activeSet := make(map[string]bool, len(params.AllowedMsgTypes))
	for _, t := range params.AllowedMsgTypes {
		activeSet[t] = true
	}
	for _, t := range msg.AllowedMsgTypes {
		if !activeSet[t] {
			return nil, types.ErrMsgTypeNotInAllowlist.Wrapf("type: %s", t)
		}
	}

	// 6. No NonDelegableSessionMsgs
	for _, t := range msg.AllowedMsgTypes {
		if types.NonDelegableSessionMsgs[t] {
			return nil, types.ErrMsgTypeForbidden.Wrapf("type: %s", t)
		}
	}

	// 7-8. Expiration checks
	blockTime := sdkCtx.BlockTime()
	if !msg.Expiration.After(blockTime) {
		return nil, types.ErrInvalidExpiration
	}
	maxExp := blockTime.Add(params.MaxExpiration)
	if msg.Expiration.After(maxExp) {
		return nil, types.ErrExpirationTooLong
	}

	// 9. Spend limit must be positive (SESSION-4 fix: zero SpendLimit disables
	// fee delegation in the ante handler, making the session useless)
	if !msg.SpendLimit.IsPositive() {
		return nil, types.ErrSpendLimitRequired
	}

	// 10. Spend limit upper bound check
	if msg.SpendLimit.Amount.GT(params.MaxSpendLimit.Amount) {
		return nil, types.ErrSpendLimitTooHigh
	}

	// 11. Denom check
	if msg.SpendLimit.Denom != "uspark" {
		return nil, types.ErrInvalidDenom
	}

	// 12. Exec count cap must be specified (0 was previously "unlimited" but is
	// now forbidden — every session must declare a finite ceiling).
	if msg.MaxExecCount == 0 {
		return nil, types.ErrMaxExecCountRequired
	}

	// 13. Exec count cap upper bound check
	if msg.MaxExecCount > params.MaxExecCount {
		return nil, types.ErrMaxExecCountTooHigh
	}

	// Allocate the grant ID and write the SESSION_KEY grant.
	id, err := k.nextGrantID(ctx)
	if err != nil {
		return nil, err
	}

	zeroCoin := sdk.NewInt64Coin("uspark", 0)
	grant := types.Grant{
		Id:        id,
		Granter:   msg.Granter,
		Grantee:   msg.Grantee,
		Type:      types.GrantType_GRANT_TYPE_SESSION_KEY,
		Status:    types.GrantStatus_GRANT_STATUS_ACTIVE,
		CreatedAt: blockTime,
		ExpiresAt: msg.Expiration,
		Payload: &types.Grant_SessionKey{
			SessionKey: &types.SessionKeyPayload{
				AllowedMsgTypes: msg.AllowedMsgTypes,
				SpendLimit:      msg.SpendLimit,
				Spent:           zeroCoin,
				MaxExecCount:    msg.MaxExecCount,
				ExecCount:       0,
				LastUsedAt:      blockTime,
			},
		},
	}

	if err := k.writeGrant(ctx, grant); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"session_created",
		sdk.NewAttribute("granter", msg.Granter),
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("expiration", msg.Expiration.String()),
		sdk.NewAttribute("grant_id", fmt.Sprintf("%d", id)),
	))

	return &types.MsgCreateSessionResponse{}, nil
}

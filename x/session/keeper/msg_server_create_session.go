package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

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

	// 2. No existing session for this pair
	key := collections.Join(msg.Granter, msg.Grantee)
	has, err := k.Sessions.Has(ctx, key)
	if err != nil {
		return nil, err
	}
	if has {
		return nil, types.ErrSessionExists
	}

	// 3. Count granter's sessions against limit
	count := uint64(0)
	rng := collections.NewPrefixedPairRange[string, string](msg.Granter)
	err = k.SessionsByGranter.Walk(ctx, rng, func(_ collections.Pair[string, string]) (bool, error) {
		count++
		if count >= params.MaxSessionsPerGranter {
			return true, nil // stop walking
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if count >= params.MaxSessionsPerGranter {
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

	// 9. Spend limit check
	if msg.SpendLimit.IsPositive() {
		if msg.SpendLimit.Amount.GT(params.MaxSpendLimit.Amount) {
			return nil, types.ErrSpendLimitTooHigh
		}
	}

	// 10. Denom check
	if msg.SpendLimit.IsPositive() && msg.SpendLimit.Denom != "uspark" {
		return nil, types.ErrInvalidDenom
	}

	// Create the session
	zeroCoin := sdk.NewInt64Coin("uspark", 0)
	session := types.Session{
		Granter:         msg.Granter,
		Grantee:         msg.Grantee,
		AllowedMsgTypes: msg.AllowedMsgTypes,
		SpendLimit:      msg.SpendLimit,
		Spent:           zeroCoin,
		Expiration:      msg.Expiration,
		CreatedAt:       blockTime,
		LastUsedAt:      blockTime,
		ExecCount:       0,
		MaxExecCount:    msg.MaxExecCount,
	}

	// Store session and indexes
	if err := k.Sessions.Set(ctx, key, session); err != nil {
		return nil, err
	}
	if err := k.SessionsByGranter.Set(ctx, collections.Join(msg.Granter, msg.Grantee)); err != nil {
		return nil, err
	}
	if err := k.SessionsByGrantee.Set(ctx, collections.Join(msg.Grantee, msg.Granter)); err != nil {
		return nil, err
	}
	if err := k.SessionsByExpiration.Set(ctx, collections.Join3(msg.Expiration.Unix(), msg.Granter, msg.Grantee)); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"session_created",
		sdk.NewAttribute("granter", msg.Granter),
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("expiration", msg.Expiration.String()),
	))

	return &types.MsgCreateSessionResponse{}, nil
}

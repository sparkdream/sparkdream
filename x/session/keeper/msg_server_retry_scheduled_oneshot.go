package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// RetryScheduledOneshot re-enqueues a PAUSED_INSUFFICIENT_FUNDS oneshot
// for firing. Per Rev 2 §4.4.2, either the granter or the grantee can
// retry (the granter already authorized the underlying action at
// creation time). Sets `fire_at = block_time` so the next EndBlocker
// fire pass picks it up. Distinct error sentinels per Rev 3 §M1 so
// CLI/indexer surfaces can render the rejection accurately.
func (k msgServer) RetryScheduledOneshot(ctx context.Context, msg *types.MsgRetryScheduledOneshot) (*types.MsgRetryScheduledOneshotResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	grant, err := k.Grants.Get(ctx, msg.GrantId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", msg.GrantId)
		}
		return nil, err
	}

	// Type check first — most informative error for the caller.
	if grant.Type != types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
		return nil, errorsmod.Wrapf(types.ErrGrantTypeMismatch,
			"grant %d is %s, expected SCHEDULED_ONESHOT", grant.Id, grant.Type)
	}

	// Authorization: caller must be granter OR grantee.
	if msg.Caller != grant.Granter && msg.Caller != grant.Grantee {
		return nil, errorsmod.Wrapf(types.ErrUnauthorizedRetry,
			"caller %s is neither granter %s nor grantee %s",
			msg.Caller, grant.Granter, grant.Grantee)
	}

	// Status: must be PAUSED. Distinct sentinels for ACTIVE vs terminal
	// so the surface error is meaningful.
	switch grant.Status {
	case types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS:
		// proceed
	case types.GrantStatus_GRANT_STATUS_ACTIVE:
		return nil, errorsmod.Wrapf(types.ErrGrantNotPaused,
			"grant %d is ACTIVE, not paused", grant.Id)
	case types.GrantStatus_GRANT_STATUS_REVOKED,
		types.GrantStatus_GRANT_STATUS_DECLINED,
		types.GrantStatus_GRANT_STATUS_COMPLETED,
		types.GrantStatus_GRANT_STATUS_FIRED:
		return nil, errorsmod.Wrapf(types.ErrGrantTerminal,
			"grant %d is %s (terminal)", grant.Id, grant.Status)
	default:
		return nil, errorsmod.Wrapf(types.ErrGrantInactive,
			"grant %d is in status %s", grant.Id, grant.Status)
	}

	so := grant.GetScheduledOneshot()
	if so == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidPayload, "scheduled_oneshot payload missing")
	}

	// Update fire_at to now so the next EndBlocker pass picks it up.
	newFireAt := sdkCtx.BlockTime().Unix()
	so.FireAt = newFireAt
	grant.Payload = &types.Grant_ScheduledOneshot{ScheduledOneshot: so}
	grant.Status = types.GrantStatus_GRANT_STATUS_ACTIVE

	if err := k.Grants.Set(ctx, grant.Id, grant); err != nil {
		return nil, err
	}

	// Drop the paused-TTL index entry so the auto-revoke pass doesn't
	// hit this grant once it's back to ACTIVE. The pause_unix used to
	// register the entry isn't stored on-grant; the auto-revoke pass
	// will tolerate the stale entry (skipping non-PAUSED status).

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"oneshot_retry_requested",
		sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("caller", msg.Caller),
	))

	return &types.MsgRetryScheduledOneshotResponse{
		NewFireAt: newFireAt,
	}, nil
}

package keeper

import (
	"context"
	"errors"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// MaxRecurringSpendNoteLen caps the human-readable purpose attached to
// a schedule. Kept tight to bound on-chain string bloat — UIs that
// need more can store off-chain and reference by id. Mirrors session's
// `maxNoteLen` so the wrapper layer's error attribution is clearly
// commons-side.
const MaxRecurringSpendNoteLen = 256

// CancelActiveSchedulesForRecipient revokes every RECURRING_PULL
// grant whose grantee equals `recipient` AND whose granter is a
// registered council policy address. Returns the list of revoked
// grant IDs (for caller-side logging).
//
// Used by the x/service `AfterOperatorDissolved` hook
// ([app/service_adapters.go](../../app/service_adapters.go)): when an
// operator is slashed-dissolved, every council-policy recurring
// payment to that operator must stop immediately so the slashed
// operator doesn't keep getting paid until a human notices.
//
// **M-svc (RecurringSpend migration):** the helper walks the session
// registry instead of the (deleted) parallel commons storage. The
// granter-is-group-policy filter is required because session holds
// *all* RECURRING_PULL grants — including user-to-user pulls, which
// the service hook should not touch.
//
// Continues to emit the legacy `recurring_spend_canceled` event with
// the `reason` attribute (federation-bridge slash recovery flow keys
// off it) alongside the session-emitted `grant_revoked` events.
// Keeping the dual emission scoped to this helper is cheaper than
// threading a `reason` field through the session bypass surface.
func (k Keeper) CancelActiveSchedulesForRecipient(ctx context.Context, recipient string, reason string) ([]uint64, error) {
	if k.late.sessionKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrGroupNotFound,
			"session keeper not wired; CancelActiveSchedulesForRecipient cannot run")
	}

	grants, err := k.late.sessionKeeper.ListGrantsByGrantee(
		ctx, recipient, sessiontypes.GrantType_GRANT_TYPE_RECURRING_PULL,
	)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	commonsModuleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	var canceled []uint64
	for _, g := range grants {
		// Filter: only council-policy granters. Skips user-to-user
		// pulls a future use case might add.
		if !k.IsGroupPolicyAddress(ctx, g.Granter) {
			continue
		}
		if _, err := k.late.sessionKeeper.RevokeGrantInternal(ctx, commonsModuleAddr, g.Id); err != nil {
			// Already terminal (e.g. concurrent cancel from the council)
			// or not found — skip silently and continue. Other errors
			// surface to the caller so dissolution-time KV failures are
			// observable.
			if errors.Is(err, sessiontypes.ErrGrantTerminal) ||
				errors.Is(err, sessiontypes.ErrGrantNotFound) {
				continue
			}
			return canceled, err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"recurring_spend_canceled",
			sdk.NewAttribute("id", fmt.Sprintf("%d", g.Id)),
			sdk.NewAttribute("authority", g.Granter),
			sdk.NewAttribute("recipient", g.Grantee),
			sdk.NewAttribute("reason", reason),
		))
		canceled = append(canceled, g.Id)
	}
	return canceled, nil
}

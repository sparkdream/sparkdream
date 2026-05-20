package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/commons/types"
)

// ClaimRecurringSpend disburses one period of an active schedule.
//
// **M8 (RecurringSpend migration):** the handler is a thin wrapper
// that calls `sessionKeeper.ClaimRecurringPullForGrantee` — the
// privileged keeper entrypoint that runs the same period check,
// expires check, hook PreCheck/PostCommit, bank send, and status
// transition logic that the session msg-server `ClaimRecurringPull`
// runs, but skips the outer signer-bytes derivation (the wrapper
// has already validated the recipient's signature).
//
// The SessionClaimHook (registered by app.go in M4) applies the
// council activation, term-expiry, and per-epoch rate-limit gates as
// PreCheck (no debit) + PostCommit (debit), atomic with the bank
// send. This closes the double-debit window documented in the
// migration plan §3.2.
//
// Response shape preserved: claim_number + next_claim_time map 1:1
// from the session response.
func (k msgServer) ClaimRecurringSpend(goCtx context.Context, msg *types.MsgClaimRecurringSpend) (*types.MsgClaimRecurringSpendResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := k.addressCodec.StringToBytes(msg.Recipient); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	if k.late.sessionKeeper == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic,
			"session keeper not wired (set via app.CommonsKeeper.SetSessionKeeper)")
	}

	commonsModuleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
	resp, err := k.late.sessionKeeper.ClaimRecurringPullForGrantee(ctx, commonsModuleAddr, msg.Id, msg.Recipient)
	if err != nil {
		// Errors from session (ErrGrantNotFound, ErrGrantInactive,
		// ErrRecurringPullUnauthorized, ErrRecurringPullNotDue,
		// ErrRecurringPullWindowClosed, hook veto errors,
		// ErrInsufficientGranterBalance, ErrEpochCeilingExceeded)
		// bubble up unchanged — they carry enough context for the
		// CLI / indexer surface.
		return nil, err
	}

	// No legacy `recurring_spend_claimed` event — session emits
	// `recurring_pull_claimed` (and `grant_resumed` / `grant_completed`
	// as appropriate).
	return &types.MsgClaimRecurringSpendResponse{
		ClaimNumber:   resp.ClaimNumber,
		NextClaimTime: resp.NextClaimTime,
	}, nil
}

package keeper

import (
	"context"
	"fmt"
	"time"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// CreateGrantOnBehalfOf is the privileged, signature-bypass entrypoint
// for module callers. It performs all the validation that the
// MsgCreateGrant handler does — common-path checks, per-payload
// validation, deposit escrow for oneshots, secondary-index fan-out —
// but skips signature and account-sequence verification, because the
// granter is typically a module account that cannot sign a real tx.
//
// Per Rev 2 §3.2 (P8 V2 unification path), this exists so trusted
// modules (x/commons for council recurring spends, future RPGF-style
// flows, etc.) can host their authorizations in the unified registry
// without inventing their own duplicate state machine. Each module
// address listed in `params.authorized_grant_creators` is a strict
// trust grant: callers should review carefully before adding entries.
//
// Authorization gate:
//   - `callerModuleAddr` MUST be present in
//     `params.authorized_grant_creators`. The check is plain string
//     equality on the bech32 form; addresses are validated at param-
//     update time so we don't need to re-parse here.
//   - The allowlist must be non-empty (defense-in-depth: an empty
//     allowlist returns `ErrBypassDisabled` rather than silently
//     accepting an empty-string match).
//   - The caller is expected to pass the *bech32 string* of the
//     module address it derived itself, e.g.
//     `authtypes.NewModuleAddress("commons").String()`. The bypass
//     does not infer the caller from `ctx` — callers must be explicit.
//
// On success returns the allocated grant id. The granter, grantee,
// expiration, note, and typed payload are all controlled by the
// caller; per-payload validation still applies (denom allow-list,
// caps, etc.) so a buggy caller cannot smuggle e.g. a DREAM-denominated
// RecurringPull.
//
// This entrypoint is intentionally NOT exposed via a Msg — it's a
// keeper-to-keeper call only. The MsgCreateGrant handler stays the
// path for user-signed tx flows.
//
// Callers construct a *types.MsgCreateGrant exactly as if they were
// going to send it as a tx — same payload oneof wrappers
// (`&MsgCreateGrant_RecurringPull{RecurringPull: ...}` etc.). The
// bypass uses that struct as the source of truth and runs all the
// same validation; only signature + tx-sequence verification is
// skipped.
func (k Keeper) CreateGrantOnBehalfOf(
	ctx context.Context,
	callerModuleAddr string,
	msg *types.MsgCreateGrant,
) (uint64, error) {
	if msg == nil {
		return 0, types.ErrInvalidPayload.Wrap("msg must not be nil")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Allowlist gate. Empty list = bypass disabled (must be opt-in
	// by gov before any module can use this path).
	if len(params.AuthorizedGrantCreators) == 0 {
		return 0, types.ErrBypassDisabled
	}
	authorized := false
	for _, addr := range params.AuthorizedGrantCreators {
		if addr == callerModuleAddr {
			authorized = true
			break
		}
	}
	if !authorized {
		return 0, errorsmod.Wrapf(types.ErrModuleNotAuthorized,
			"caller %s not in allowlist (size=%d)",
			callerModuleAddr, len(params.AuthorizedGrantCreators))
	}

	// Run the same shared validation the MsgCreateGrant handler does.
	if err := k.validateGrantCommon(ctx, params, blockTime, msg.Granter, msg.Grantee, msg.ExpiresAt, msg.Note); err != nil {
		return 0, err
	}

	id, err := k.nextGrantID(ctx)
	if err != nil {
		return 0, err
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

	switch p := msg.Payload.(type) {
	case *types.MsgCreateGrant_SessionKey:
		sk, err := k.validateSessionKeyPayload(ctx, params, blockTime, msg.Granter, msg.Grantee, msg.ExpiresAt, p.SessionKey)
		if err != nil {
			return 0, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_SESSION_KEY
		grant.Payload = &types.Grant_SessionKey{SessionKey: sk}

	case *types.MsgCreateGrant_RecurringPull:
		rp, err := k.validateRecurringPullPayload(ctx, params, blockTime, msg.Granter, msg.ExpiresAt, p.RecurringPull)
		if err != nil {
			return 0, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_RECURRING_PULL
		grant.Payload = &types.Grant_RecurringPull{RecurringPull: rp}

	case *types.MsgCreateGrant_SpendingAllowance:
		sa, err := k.validateSpendingAllowancePayload(ctx, params, blockTime, msg.Granter, p.SpendingAllowance)
		if err != nil {
			return 0, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE
		grant.Payload = &types.Grant_SpendingAllowance{SpendingAllowance: sa}

	case *types.MsgCreateGrant_ScheduledOneshot:
		so, err := k.validateScheduledOneshotPayload(ctx, params, blockTime, msg.Granter, msg.ExpiresAt, p.ScheduledOneshot)
		if err != nil {
			return 0, err
		}
		// Deposit escrow. The granter here is typically a module
		// account; SendCoinsFromAccountToModule still works because
		// the SDK bank keeper treats module-account-as-account-by-bech32
		// uniformly. Callers are responsible for funding the module
		// account before invoking this path.
		deposit, err := computeOneshotDeposit(params, so.Action)
		if err != nil {
			return 0, err
		}
		depositCoin := sdk.NewCoin(k.BondDenom(ctx), deposit)
		granterAddr, err := k.addressCodec.StringToBytes(msg.Granter)
		if err != nil {
			return 0, err
		}
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, granterAddr, types.ModuleName, sdk.NewCoins(depositCoin)); err != nil {
			return 0, err
		}
		if err := k.OneshotGasDeposit.Set(ctx, id, depositCoin); err != nil {
			return 0, err
		}
		grant.Type = types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT
		grant.Payload = &types.Grant_ScheduledOneshot{ScheduledOneshot: so}

	default:
		return 0, types.ErrInvalidPayload.Wrap("payload oneof must be set")
	}

	if err := k.writeGrant(ctx, grant); err != nil {
		return 0, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"grant_created",
		sdk.NewAttribute("id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("type", grant.Type.String()),
		sdk.NewAttribute("granter", msg.Granter),
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("expires_at", msg.ExpiresAt.UTC().Format(time.RFC3339Nano)),
		sdk.NewAttribute("source", "module_bypass"),
		sdk.NewAttribute("caller_module", callerModuleAddr),
	))

	return id, nil
}

// RevokeGrantInternal is the keeper-to-keeper revocation path. Unlike
// MsgRevokeGrant it skips the granter-signature check, because the
// caller is typically a module's EndBlocker (e.g. x/commons revoking
// grants tied to a council whose term has expired). The gate is the
// same `authorized_grant_creators` allowlist as
// `CreateGrantOnBehalfOf` — a caller who can create on behalf of
// granter X must also be able to revoke for X, to keep the lifecycle
// closed.
//
// Refunds any held OneshotGasDeposit to the grant's granter (typically
// a module account; the refund returns the deposit to the module's
// own account, where the calling module is responsible for handling
// it). Emits `grant_revoked` with `source=module_bypass`.
func (k Keeper) RevokeGrantInternal(
	ctx context.Context,
	callerModuleAddr string,
	grantID uint64,
) (sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return sdk.Coin{}, err
	}
	if len(params.AuthorizedGrantCreators) == 0 {
		return sdk.Coin{}, types.ErrBypassDisabled
	}
	authorized := false
	for _, addr := range params.AuthorizedGrantCreators {
		if addr == callerModuleAddr {
			authorized = true
			break
		}
	}
	if !authorized {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrModuleNotAuthorized,
			"caller %s not in allowlist", callerModuleAddr)
	}

	grant, err := k.Grants.Get(ctx, grantID)
	if err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", grantID)
	}
	if terminalStatus(grant.Status) {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrGrantTerminal,
			"grant %d is %s (terminal)", grant.Id, grant.Status)
	}

	// Refund any held oneshot deposit.
	refund := sdk.NewCoin(k.BondDenom(ctx), sdkmath.ZeroInt())
	if grant.Type == types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
		dep, depErr := k.OneshotGasDeposit.Get(ctx, grant.Id)
		if depErr == nil && !dep.IsZero() {
			granterAddr, addrErr := k.addressCodec.StringToBytes(grant.Granter)
			if addrErr != nil {
				return sdk.Coin{}, addrErr
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, granterAddr, sdk.NewCoins(dep)); err != nil {
				return sdk.Coin{}, errorsmod.Wrap(err, "deposit refund failed")
			}
			_ = k.OneshotGasDeposit.Remove(ctx, grant.Id)
			refund = dep
		}
	}

	if err := k.deleteGrant(ctx, grant); err != nil {
		return sdk.Coin{}, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"grant_revoked",
		sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("type", grant.Type.String()),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("refund_amount", refund.String()),
		sdk.NewAttribute("source", "module_bypass"),
		sdk.NewAttribute("caller_module", callerModuleAddr),
	))

	return refund, nil
}

// DeclineGrantInternal is the privileged decline path, symmetric with
// RevokeGrantInternal. Required by the x/commons RecurringSpend
// wrapper (`MsgDeclineRecurringSpend`): the outer signer is the
// recipient (already validated by the wrapper handler), but routing
// through the session msg server would re-validate the grantee
// signature on a tx that's already been signed by the outer caller.
//
// Authorization is twofold:
//   - `callerModuleAddr` must be in `params.authorized_grant_creators`
//     (same gate as the rest of the bypass surface), and
//   - the supplied `grantee` must equal `grant.Grantee` — defense in
//     depth so a wrapper bug can't decline the wrong recipient's
//     grant.
//
// Refunds any held OneshotGasDeposit to the grant's granter (typically
// a module account; the refund returns to that module's own account
// for the calling module to handle).
//
// Emits `grant_declined` with `source = module_bypass`.
func (k Keeper) DeclineGrantInternal(
	ctx context.Context,
	callerModuleAddr string,
	grantID uint64,
	grantee string,
) (sdk.Coin, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return sdk.Coin{}, err
	}
	if len(params.AuthorizedGrantCreators) == 0 {
		return sdk.Coin{}, types.ErrBypassDisabled
	}
	authorized := false
	for _, addr := range params.AuthorizedGrantCreators {
		if addr == callerModuleAddr {
			authorized = true
			break
		}
	}
	if !authorized {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrModuleNotAuthorized,
			"caller %s not in allowlist", callerModuleAddr)
	}

	grant, err := k.Grants.Get(ctx, grantID)
	if err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", grantID)
	}
	if grant.Grantee != grantee {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrDeclineUnauthorized,
			"supplied grantee %s does not match grant.Grantee %s", grantee, grant.Grantee)
	}
	if terminalStatus(grant.Status) {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrGrantTerminal,
			"grant %d is %s (terminal; decline is one-way)", grant.Id, grant.Status)
	}

	// Refund any held oneshot deposit before deleting the grant.
	refund := sdk.NewCoin(k.BondDenom(ctx), sdkmath.ZeroInt())
	if grant.Type == types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
		dep, depErr := k.OneshotGasDeposit.Get(ctx, grant.Id)
		if depErr == nil && !dep.IsZero() {
			granterAddr, addrErr := k.addressCodec.StringToBytes(grant.Granter)
			if addrErr != nil {
				return sdk.Coin{}, addrErr
			}
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, granterAddr, sdk.NewCoins(dep)); err != nil {
				return sdk.Coin{}, errorsmod.Wrap(err, "deposit refund failed")
			}
			_ = k.OneshotGasDeposit.Remove(ctx, grant.Id)
			refund = dep
		}
	}

	if err := k.deleteGrant(ctx, grant); err != nil {
		return sdk.Coin{}, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"grant_declined",
		sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("type", grant.Type.String()),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("refund_amount", refund.String()),
		sdk.NewAttribute("source", "module_bypass"),
		sdk.NewAttribute("caller_module", callerModuleAddr),
	))

	return refund, nil
}

// ClaimRecurringPullForGrantee is the privileged claim entrypoint for
// the x/commons `MsgClaimRecurringSpend` wrapper. The outer wrapper
// has already verified the recipient's signature on the wrapping
// message; this method skips the msg-server's signature re-check and
// runs the same `claimRecurringPullCommon` body that the msg-server
// `ClaimRecurringPull` runs (period check, expires check, hooks, bank
// send, status transitions, events).
//
// Authorization gate is the same `authorized_grant_creators` allowlist
// as `CreateGrantOnBehalfOf` / `RevokeGrantInternal`. The supplied
// `grantee` must equal `grant.Grantee` (verified inside
// `claimRecurringPullCommon`); a privileged caller cannot claim on
// behalf of an arbitrary address.
func (k Keeper) ClaimRecurringPullForGrantee(
	ctx context.Context,
	callerModuleAddr string,
	grantID uint64,
	grantee string,
) (*types.MsgClaimRecurringPullResponse, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(params.AuthorizedGrantCreators) == 0 {
		return nil, types.ErrBypassDisabled
	}
	authorized := false
	for _, addr := range params.AuthorizedGrantCreators {
		if addr == callerModuleAddr {
			authorized = true
			break
		}
	}
	if !authorized {
		return nil, errorsmod.Wrapf(types.ErrModuleNotAuthorized,
			"caller %s not in allowlist", callerModuleAddr)
	}

	return k.claimRecurringPullCommon(ctx, grantID, grantee)
}

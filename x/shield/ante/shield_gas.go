package ante

import (
	"context"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	shieldtypes "sparkdream/x/shield/types"
)

const (
	// minProofByteLength is the minimum byte length for a valid Groth16 proof.
	// A BN254 Groth16 proof has 3 curve points (2 G1 + 1 G2), each ~32-64 bytes.
	// 128 bytes is a safe lower bound for any non-trivial proof.
	minProofByteLength = 128

	// nullifierByteLength is the expected byte length for nullifiers (32 bytes = 256 bits).
	nullifierByteLength = 32

	// maxSubmitterExecsPerEpoch is the per-submitter address rate limit.
	// This is a hardcoded anti-spam measure at the ante handler level,
	// separate from the per-identity ZK rate limit. Set conservatively high
	// to avoid blocking legitimate relayers, but low enough to bound spam.
	maxSubmitterExecsPerEpoch uint64 = 200
)

// ShieldKeeper defines the interface needed by the ante handler.
type ShieldKeeper interface {
	GetShieldParams(ctx sdk.Context) (shieldtypes.Params, error)
	GetCurrentEpoch(ctx context.Context) uint64
	GetSubmitterExecCount(ctx context.Context, epoch uint64, submitter string) uint64
	IncrementSubmitterExecCount(ctx context.Context, epoch uint64, submitter string)
}

// BankKeeper defines the bank interface needed by the ante handler.
type BankKeeper interface {
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// ShieldGasDecorator intercepts transactions containing MsgShieldedExec
// and deducts fees from the shield module account instead of the submitter.
//
// SHIELD-8: This decorator performs lightweight anti-spam validation BEFORE
// paying gas to prevent draining the shield gas reserve with invalid submissions.
type ShieldGasDecorator struct {
	shieldKeeper ShieldKeeper
	bankKeeper   BankKeeper
}

// NewShieldGasDecorator creates a new ShieldGasDecorator.
func NewShieldGasDecorator(sk ShieldKeeper, bk BankKeeper) ShieldGasDecorator {
	return ShieldGasDecorator{
		shieldKeeper: sk,
		bankKeeper:   bk,
	}
}

func (d ShieldGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msgs := tx.GetMsgs()

	// Check if any message is MsgShieldedExec
	hasShieldedExec := false
	var shieldMsg *shieldtypes.MsgShieldedExec
	for _, msg := range msgs {
		if m, ok := msg.(*shieldtypes.MsgShieldedExec); ok {
			hasShieldedExec = true
			shieldMsg = m
			break
		}
	}

	if !hasShieldedExec {
		return next(ctx, tx, simulate)
	}

	// REJECT multi-message transactions containing MsgShieldedExec
	if len(msgs) != 1 {
		return ctx, shieldtypes.ErrMultiMsgNotAllowed
	}

	// Check shield module is enabled
	params, err := d.shieldKeeper.GetShieldParams(ctx)
	if err != nil {
		return ctx, err
	}
	if !params.Enabled {
		return ctx, shieldtypes.ErrShieldDisabled
	}

	// --- SHIELD-8: Lightweight anti-spam checks BEFORE paying gas ---

	// 1. Validate nullifier format: must be exactly 32 bytes (256-bit hash output).
	if len(shieldMsg.Nullifier) != nullifierByteLength {
		return ctx, shieldtypes.ErrInvalidNullifierLength
	}

	// 2. Validate rate limit nullifier format for immediate mode.
	if shieldMsg.ExecMode == shieldtypes.ShieldExecMode_SHIELD_EXEC_IMMEDIATE {
		if len(shieldMsg.RateLimitNullifier) != nullifierByteLength {
			return ctx, shieldtypes.ErrInvalidNullifierLength
		}
		// 3. Validate proof has minimum byte length (immediate mode requires proof).
		if len(shieldMsg.Proof) < minProofByteLength {
			return ctx, shieldtypes.ErrInvalidProof
		}
	}

	// 4. Per-submitter address rate limit to bound total gas spend per address per epoch.
	epoch := d.shieldKeeper.GetCurrentEpoch(ctx)
	submitterCount := d.shieldKeeper.GetSubmitterExecCount(ctx, epoch, shieldMsg.Submitter)
	if submitterCount >= maxSubmitterExecsPerEpoch {
		return ctx, shieldtypes.ErrRateLimitExceeded
	}
	d.shieldKeeper.IncrementSubmitterExecCount(ctx, epoch, shieldMsg.Submitter)

	// --- End anti-spam checks ---

	// Calculate fee from gas
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, sdkerrors.ErrTxDecode
	}
	fees := feeTx.GetFee()

	if fees.IsZero() {
		// No fees to pay — proceed (gas is still metered)
		ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
		return next(ctx, tx, simulate)
	}

	// Deduct fees from shield module account → fee collector
	err = d.bankKeeper.SendCoinsFromModuleToModule(
		ctx,
		shieldtypes.ModuleName,
		authtypes.FeeCollectorName,
		fees,
	)
	if err != nil {
		return ctx, shieldtypes.ErrShieldGasDepleted
	}

	// Set fee-paid flag so the standard DeductFeeDecorator skips this tx
	ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
	return next(ctx, tx, simulate)
}

// validateNullifierHex checks that a hex-encoded nullifier is well-formed (optional utility).
func validateNullifierHex(nullifierHex string) bool {
	b, err := hex.DecodeString(nullifierHex)
	if err != nil {
		return false
	}
	return len(b) == nullifierByteLength
}

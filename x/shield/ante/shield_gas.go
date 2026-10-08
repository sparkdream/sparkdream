package ante

import (
	"context"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
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
	PrecheckImmediate(ctx sdk.Context, msg *shieldtypes.MsgShieldedExec) error
	GetVerificationKeyVal(ctx context.Context, circuitID string) (shieldtypes.VerificationKey, bool)
	GetShieldedOp(ctx context.Context, typeURL string) (shieldtypes.ShieldedOpRegistration, bool)
	BondDenom(ctx context.Context) string
}

// BankKeeper defines the bank interface needed by the ante handler.
type BankKeeper interface {
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// ShieldGasDecorator intercepts transactions containing MsgShieldedExec
// and deducts fees from the shield module account instead of the submitter.
//
// SHIELD-8: This decorator validates the exec BEFORE paying gas to prevent
// draining the shield gas reserve with invalid submissions. Immediate-mode execs
// get their ZK proof verified here, so only a real member's exec is paid for.
//
// In CheckTx / ReCheckTx / simulate the verification sits behind a node-local
// rejected-proof cache and token bucket (see proof_guard.go); DeliverTx always
// verifies.
//
// It also confines the shared anonymous submitter (whose private key is public)
// to MsgShieldedExec.
type ShieldGasDecorator struct {
	shieldKeeper ShieldKeeper
	bankKeeper   BankKeeper
	guard        *proofGuard
}

// NewShieldGasDecorator creates a new ShieldGasDecorator.
func NewShieldGasDecorator(sk ShieldKeeper, bk BankKeeper) ShieldGasDecorator {
	return ShieldGasDecorator{
		shieldKeeper: sk,
		bankKeeper:   bk,
		guard:        newProofGuard(),
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
		if err := rejectPublicSubmitter(tx); err != nil {
			return ctx, err
		}
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

	immediate := shieldMsg.ExecMode == shieldtypes.ShieldExecMode_SHIELD_EXEC_IMMEDIATE

	// 2. Validate rate limit nullifier format for immediate mode.
	if immediate {
		if len(shieldMsg.RateLimitNullifier) != nullifierByteLength {
			return ctx, shieldtypes.ErrInvalidNullifierLength
		}
		// 3. Validate proof has minimum byte length (immediate mode requires proof).
		if len(shieldMsg.Proof) < minProofByteLength {
			return ctx, shieldtypes.ErrInvalidProof
		}
	} else if !params.EncryptedBatchEnabled {
		// Batch execs can't be verified until decryption; don't pay for one
		// that the msg server is going to reject anyway.
		return ctx, shieldtypes.ErrEncryptedBatchDisabled
	}

	if immediate {
		// 4. Verify the proof, nullifier and identity rate limit before the
		// module pays. The identity rate limit bounds spend per member, so
		// immediate execs need no per-address cap (and anonymous clients
		// share one submitter address).
		if err := d.precheckImmediate(ctx, shieldMsg, simulate); err != nil {
			return ctx, err
		}
		// The msg server runs against the same state, so it needn't verify
		// the proof again.
		ctx = shieldtypes.WithProofVerified(ctx, shieldMsg)
	} else {
		// 4. Batch execs can't be verified until decryption: bound total gas
		// spend per submitter address per epoch instead.
		epoch := d.shieldKeeper.GetCurrentEpoch(ctx)
		if d.shieldKeeper.GetSubmitterExecCount(ctx, epoch, shieldMsg.Submitter) >= maxSubmitterExecsPerEpoch {
			return ctx, shieldtypes.ErrRateLimitExceeded
		}
		d.shieldKeeper.IncrementSubmitterExecCount(ctx, epoch, shieldMsg.Submitter)
	}

	// --- End anti-spam checks ---

	// Calculate fee from gas
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, sdkerrors.ErrTxDecode
	}
	fees := feeTx.GetFee()

	// 5. The submitter picks the fee but the module pays it: cap it, in the
	// bond denom only, so one exec can't hand the reserve to the fee collector.
	maxFee := sdk.NewCoins(sdk.NewCoin(d.shieldKeeper.BondDenom(ctx), params.MaxFeePerExec))
	if !fees.IsZero() && !fees.IsAllLTE(maxFee) {
		return ctx, errorsmod.Wrapf(shieldtypes.ErrFeeTooHigh, "fee %s, max %s", fees, maxFee)
	}

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

// rejectPublicSubmitter refuses a non-shield tx signed by the shared anonymous
// submitter. Its private key is public, so anything else it signed would be
// spendable by anyone.
func rejectPublicSubmitter(tx sdk.Tx) error {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return nil
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return err
	}
	public := shieldtypes.PublicSubmitterAddress()
	for _, s := range signers {
		if public.Equals(sdk.AccAddress(s)) {
			return errorsmod.Wrap(sdkerrors.ErrUnauthorized, "the public anonymous submitter may only sign MsgShieldedExec")
		}
	}
	return nil
}

// validateNullifierHex checks that a hex-encoded nullifier is well-formed (optional utility).
func validateNullifierHex(nullifierHex string) bool {
	b, err := hex.DecodeString(nullifierHex)
	if err != nil {
		return false
	}
	return len(b) == nullifierByteLength
}

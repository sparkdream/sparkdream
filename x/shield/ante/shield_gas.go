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
)

// ShieldKeeper defines the interface needed by the ante handler.
type ShieldKeeper interface {
	GetShieldParams(ctx sdk.Context) (shieldtypes.Params, error)
	GetCurrentEpoch(ctx context.Context) uint64
	PrecheckExec(ctx sdk.Context, msg *shieldtypes.MsgShieldedExec) error
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
// draining the shield gas reserve with invalid submissions. Both modes get a
// ZK proof verified here (the operation proof for an immediate exec, the
// submission proof for an encrypted-batch exec), so only a real member's exec,
// under its identity rate limit, is paid for.
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

	// --- SHIELD-8: validation BEFORE paying gas ---

	// 1. Format: 32-byte nullifiers and a proof of at least a Groth16
	//    proof's size, in both modes (a batch exec carries the submission
	//    proof in cleartext).
	if len(shieldMsg.Nullifier) != nullifierByteLength || len(shieldMsg.RateLimitNullifier) != nullifierByteLength {
		return ctx, shieldtypes.ErrInvalidNullifierLength
	}
	if len(shieldMsg.Proof) < minProofByteLength {
		return ctx, shieldtypes.ErrInvalidProof
	}
	if shieldMsg.ExecMode == shieldtypes.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH && !params.EncryptedBatchEnabled {
		return ctx, shieldtypes.ErrEncryptedBatchDisabled
	}

	// 2. Full validation: every check the msg server would fail on, then the
	//    proof (incl. MessageHash and the identity rate limit, which the
	//    proof makes unforgeable in both modes). Outside DeliverTx this runs
	//    behind the node-local proof guard (rejected-proof cache +
	//    verification throttle; see proof_guard.go).
	if err := d.precheckExec(ctx, shieldMsg, simulate); err != nil {
		return ctx, err
	}
	// The msg server runs against the same state, so it needn't verify the
	// proof again.
	ctx = shieldtypes.WithProofVerified(ctx, shieldMsg)

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

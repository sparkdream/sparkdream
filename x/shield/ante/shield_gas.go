package ante

import (
	"context"
	"encoding/hex"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
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
	PrecheckExec(ctx sdk.Context, msg *shieldtypes.MsgShieldedExec) (bool, error)
	ChargeEpochFee(ctx sdk.Context, fee math.Int, params shieldtypes.Params, msg *shieldtypes.MsgShieldedExec) error
	RefundEpochFee(ctx sdk.Context, epoch uint64, amount math.Int, identity string) error
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
	marks        *FeeMarks
}

// NewShieldGasDecorator creates a new ShieldGasDecorator.
func NewShieldGasDecorator(sk ShieldKeeper, bk BankKeeper) ShieldGasDecorator {
	return ShieldGasDecorator{
		shieldKeeper: sk,
		bankKeeper:   bk,
		guard:        newProofGuard(),
		marks:        NewFeeMarks(),
	}
}

// FeeMarks is where the decorator records each finalize-mode charge for the
// FeeRefundDecorator (post handler) that pays the unused part back.
func (d ShieldGasDecorator) FeeMarks() *FeeMarks { return d.marks }

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

	// The public submitter's key is public: an ordered tx from it bumps a
	// sequence every client shares, so anyone could grief it, and an ordered
	// tx is also a client fingerprint. Its txs must be unordered.
	if err := requireUnorderedPublicSubmitter(tx); err != nil {
		return ctx, err
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
	// Canonical field encodings only: the verifier reduces public inputs mod
	// r, so n and n+r would both verify while being distinct nullifiers.
	if err := shieldtypes.ValidateCanonicalPublicInputs(shieldMsg.Nullifier, shieldMsg.RateLimitNullifier, shieldMsg.MerkleRoot); err != nil {
		return ctx, err
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
	consumed, err := d.precheckExec(ctx, shieldMsg, simulate)
	if err != nil {
		return ctx, err
	}
	// The msg server runs against the same state, so it needn't verify the
	// proof again.
	ctx = shieldtypes.WithProofVerified(ctx, shieldMsg)
	// An immediate exec was spent here (nullifier or owner sequence, and the
	// identity rate limit), in the ante state that commits with the fee even
	// if the inner message fails: the msg server must not spend it again.
	if consumed {
		ctx = shieldtypes.WithExecConsumed(ctx, shieldMsg)
	}

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

	// The standard DeductFeeDecorator (skipped below) is where CheckTx
	// enforces the node's minimum gas prices; apply the same mempool rule
	// here so a shield tx can't claim a large gas limit for a zero or token
	// fee. Like the SDK's check this is node-local and CheckTx-only.
	if ctx.IsCheckTx() && !simulate {
		if err := checkMinGasPrices(ctx, fees, feeTx.GetGas()); err != nil {
			return ctx, err
		}
	}

	if fees.IsZero() {
		// No fees to pay — proceed (gas is still metered)
		ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)
		return next(ctx, tx, simulate)
	}

	// Chain-wide budget: the fees the module pays in one shield epoch are
	// capped (max_fees_per_epoch), so no set of identities, however many
	// in-quota execs they run, can spend the day's funding in a few epochs
	// and leave the reserve at ErrShieldGasDepleted for everyone else. One
	// identity may use only a share of it, and a slice is reserved for
	// group-tree (secret-ballot) execs, so neither one member nor trust-tree
	// traffic can lock everyone else, or secret-ballot governance, out.
	if err := d.shieldKeeper.ChargeEpochFee(ctx, fees.AmountOf(d.shieldKeeper.BondDenom(ctx)), params, shieldMsg); err != nil {
		return ctx, err
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

	// Only a block's own execution is refunded (FeeRefundDecorator): the
	// messages do not run in CheckTx, so a check-state refund would measure
	// nothing, and the check state is discarded at commit anyway.
	if !simulate && ctx.ExecMode() == sdk.ExecModeFinalize {
		d.marks.mark(ctx, FeeCharge{
			Fee:      fees,
			GasLimit: feeTx.GetGas(),
			Epoch:    d.shieldKeeper.GetCurrentEpoch(ctx),
			Identity: hex.EncodeToString(shieldMsg.RateLimitNullifier),
		})
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

// requireUnorderedPublicSubmitter refuses a shield tx signed by the shared
// anonymous submitter unless it is unordered.
func requireUnorderedPublicSubmitter(tx sdk.Tx) error {
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
		if !public.Equals(sdk.AccAddress(s)) {
			continue
		}
		if utx, ok := tx.(sdk.TxWithUnordered); !ok || !utx.GetUnordered() {
			return shieldtypes.ErrOrderedPublicSubmitterTx
		}
	}
	return nil
}

// checkMinGasPrices mirrors the SDK's CheckTx minimum-gas-price rule
// (fee >= ceil(minGasPrice * gasLimit) in at least one denom).
func checkMinGasPrices(ctx sdk.Context, fees sdk.Coins, gas uint64) error {
	minGasPrices := ctx.MinGasPrices()
	if minGasPrices.IsZero() {
		return nil
	}
	required := make(sdk.Coins, len(minGasPrices))
	glDec := math.LegacyNewDecFromInt(math.NewIntFromUint64(gas))
	for i, gp := range minGasPrices {
		required[i] = sdk.NewCoin(gp.Denom, gp.Amount.Mul(glDec).Ceil().RoundInt())
	}
	if !fees.IsAnyGTE(required) {
		return errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "insufficient fees; got: %s required: %s", fees, required)
	}
	return nil
}

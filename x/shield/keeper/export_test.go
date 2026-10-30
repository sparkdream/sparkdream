package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	any "github.com/cosmos/gogoproto/types/any"

	"sparkdream/x/shield/types"
)

// ResolveNullifierScope exposes resolveNullifierScope to the keeper_test package.
func (k Keeper) ResolveNullifierScope(ctx context.Context, reg types.ShieldedOpRegistration, msg *types.MsgShieldedExec) (uint64, error) {
	return k.resolveNullifierScope(ctx, reg, msg)
}

// ExecuteInnerMessage exposes executeInnerMessage to the keeper_test package.
func (k Keeper) ExecuteInnerMessage(ctx sdk.Context, params types.Params, inner *any.Any) (*any.Any, error) {
	return k.executeInnerMessage(ctx, params, inner)
}

// ProcessEncryptedOp exposes processEncryptedOp and its result codes to the
// keeper_test package.
func (k Keeper) ProcessEncryptedOp(ctx context.Context, params types.Params, op *types.PendingShieldedOp, decryptionKey []byte) int {
	return k.processEncryptedOp(ctx, params, op, decryptionKey)
}

const (
	BatchResultOK        = batchResultOK
	BatchResultOtherFail = batchResultOtherFail
)

// VerifyConsumeProof exposes verifyProofAtEpoch with a consume-mode binding.
func (k Keeper) VerifyConsumeProof(ctx context.Context, msg *types.MsgShieldedExec, domain uint32, scope, epoch uint64) error {
	return k.verifyProofAtEpoch(ctx, msg, consumeBinding(domain, scope, msg), epoch, false)
}

// PrecheckImmediateErr is PrecheckImmediate without the consumed flag.
func (k Keeper) PrecheckImmediateErr(ctx sdk.Context, msg *types.MsgShieldedExec) error {
	_, err := k.PrecheckImmediate(ctx, msg)
	return err
}

// PrecheckExecErr is PrecheckExec without the consumed flag.
func (k Keeper) PrecheckExecErr(ctx sdk.Context, msg *types.MsgShieldedExec) error {
	_, err := k.PrecheckExec(ctx, msg)
	return err
}

// ValidateMerkleRoot exposes validateMerkleRoot.
func (k Keeper) ValidateMerkleRoot(ctx context.Context, msg *types.MsgShieldedExec) error {
	return k.validateMerkleRoot(ctx, msg)
}

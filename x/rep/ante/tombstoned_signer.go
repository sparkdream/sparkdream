// Package ante holds x/rep's ante decorators.
package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Messages a tombstoned signer may still send. Everything else, including
// MsgExecSession and any other wrapper, is refused: this is the backstop
// for any authorization site the alias conversion missed, turning it into a
// clean failure.
const (
	msgSendURL             = "/cosmos.bank.v1beta1.MsgSend"
	msgUndelegateURL       = "/cosmos.staking.v1beta1.MsgUndelegate"
	msgBeginRedelegateURL  = "/cosmos.staking.v1beta1.MsgBeginRedelegate"
	msgWithdrawRewardURL   = "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward"
	msgCancelUnbondingURL  = "/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation"
	msgWithdrawCommissionU = "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission"
)

// TombstoneAllowed reports whether a message type may be signed by a
// tombstone left by a migration on the given path. Staking and reward
// withdrawals are always allowed, because delegations stay at the old
// address. MsgSend is allowed only on the KEY path: on the RECOVERY and
// COUNCIL paths the old key is presumed compromised and unbonded SPARK that
// keeps arriving there must wait for the canonical address's
// MsgSweepTombstone instead of being cashed out by a thief.
func TombstoneAllowed(typeURL string, path types.MigrationPath) bool {
	switch typeURL {
	case msgUndelegateURL, msgBeginRedelegateURL, msgWithdrawRewardURL, msgCancelUnbondingURL, msgWithdrawCommissionU:
		return true
	case msgSendURL:
		return path == types.MigrationPath_MIGRATION_PATH_KEY
	default:
		return false
	}
}

// TombstonedSignerDecorator refuses transactions signed by a tombstoned
// address unless every message is on the tombstone allowlist.
type TombstonedSignerDecorator struct {
	k keeper.Keeper
}

// NewTombstonedSignerDecorator constructs the decorator.
func NewTombstonedSignerDecorator(k keeper.Keeper) TombstonedSignerDecorator {
	return TombstonedSignerDecorator{k: k}
}

// AnteHandle implements sdk.AnteDecorator.
func (d TombstonedSignerDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(signing.SigVerifiableTx)
	if !ok {
		return next(ctx, tx, simulate)
	}
	signers, err := sigTx.GetSigners()
	if err != nil {
		return ctx, err
	}
	for _, signer := range signers {
		addr := sdk.AccAddress(signer).String()
		t, found := d.k.GetTombstone(ctx, addr)
		if !found {
			continue
		}
		for _, msg := range tx.GetMsgs() {
			url := sdk.MsgTypeURL(msg)
			if !TombstoneAllowed(url, t.Path) {
				return ctx, errorsmod.Wrapf(types.ErrTombstoneMsgForbidden,
					"%s migrated to %s (%s path); %s is not allowed from a tombstone", addr, t.Canonical, t.Path, url)
			}
		}
	}
	return next(ctx, tx, simulate)
}

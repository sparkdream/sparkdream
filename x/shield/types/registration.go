package types

import (
	errorsmod "cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Validate checks a shielded operation registration.
func (r ShieldedOpRegistration) Validate() error {
	if r.MessageTypeUrl == "" {
		return errorsmod.Wrap(ErrInvalidInnerMessage, "message_type_url cannot be empty")
	}
	// An ownership proof binds the owner's sequence at execution, which an
	// encrypted batch submitter can't predict.
	if r.NullifierMode == NullifierMode_NULLIFIER_MODE_OWNERSHIP &&
		r.BatchMode != ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY {
		return errorsmod.Wrapf(ErrOwnershipBatchMode, "%s: ownership operations need batch_mode IMMEDIATE_ONLY", r.MessageTypeUrl)
	}
	if r.EpochWindow > 1 && (r.NullifierScopeType != NullifierScopeType_NULLIFIER_SCOPE_EPOCH ||
		r.NullifierMode == NullifierMode_NULLIFIER_MODE_OWNERSHIP) {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "%s: epoch_window only applies to EPOCH-scoped one-time nullifiers", r.MessageTypeUrl)
	}
	return nil
}

// EpochScope returns an EPOCH-scoped operation's nullifier scope at epoch:
// the index of its epoch_window-long window, so one action per member per
// window (every epoch when the window is 0 or 1).
func (r ShieldedOpRegistration) EpochScope(epoch uint64) uint64 {
	return epoch / r.Window()
}

// Window returns the epoch_window in effect: at least 1 epoch.
func (r ShieldedOpRegistration) Window() uint64 {
	return max(r.EpochWindow, 1)
}

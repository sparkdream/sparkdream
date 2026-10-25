package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// ClearZkPublicKey zeroes the member's ZK leaf at once. It is the remedy for a
// leaked shield secret (multisig-accounts plan §2.1): a key *change* is capped
// at one per season, so a member who already rotated and then leaks the secret
// would otherwise have no remedy until the season ends. Zeroing can only take
// capability away, so clearing has no cooldown and no per-season limit.
//
// What it must not do is reopen the double-action hole: a member with no key
// and nothing pending registers immediately, so "clear, then register" would
// be an instant key swap. Clearing therefore sets zk_key_cleared and leaves
// zk_key_changed / last_zk_key_change_season untouched; while the flag is set,
// RegisterZkPublicKey always schedules the key through the activation delay
// and still honours the once-per-season cap. The flag is consumed when a key
// activates.
func (k msgServer) ClearZkPublicKey(ctx context.Context, msg *types.MsgClearZkPublicKey) (*types.MsgClearZkPublicKeyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}

	member, err := k.Member.Get(ctx, msg.Member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrMemberNotFound, msg.Member)
	}
	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(types.ErrMemberNotActive, "only active members can clear ZK public keys")
	}
	// A tombstone can never touch a key: the identity lives elsewhere.
	if k.IsTombstoned(ctx, msg.Member) {
		return nil, types.ErrMemberTombstoned
	}
	if len(member.ZkPublicKey) == 0 && len(member.PendingZkPublicKey) == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "member has no ZK public key to clear")
	}

	if len(member.PendingZkPublicKey) > 0 {
		// Drop the scheduled activation; a stale queue entry would be skipped
		// anyway (the EndBlocker re-checks the pending key), but keep state tidy.
		_ = k.ZkKeyActivationQueue.Remove(ctx, collections.Join(member.ZkKeyActivatesAtEpoch, msg.Member))
	}
	member.ZkPublicKey = nil
	member.PendingZkPublicKey = nil
	member.ZkKeyActivatesAtEpoch = 0
	member.ZkKeyCleared = true
	if err := k.Member.Set(ctx, msg.Member, member); err != nil {
		return nil, err
	}
	k.MarkMemberDirty(ctx, msg.Member)

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("rep.zk_public_key_cleared",
		sdk.NewAttribute("member", msg.Member),
	))
	return &types.MsgClearZkPublicKeyResponse{}, nil
}

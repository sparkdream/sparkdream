package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	zkcrypto "sparkdream/tools/crypto"
)

// ZkPublicKeySize is the required size of a ZK public key (32 bytes, BN254 field element).
const ZkPublicKeySize = 32

func (k msgServer) RegisterZkPublicKey(ctx context.Context, msg *types.MsgRegisterZkPublicKey) (*types.MsgRegisterZkPublicKeyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}

	// Validate key size.
	if len(msg.ZkPublicKey) != ZkPublicKeySize {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "zk_public_key must be exactly %d bytes, got %d", ZkPublicKeySize, len(msg.ZkPublicKey))
	}

	// The key is hashed into the trust tree as a BN254 field element; a value at
	// or above the modulus would hash to garbage and lock the member out of
	// every proof.
	if !zkcrypto.IsCanonicalFieldElement(msg.ZkPublicKey) {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "zk_public_key must be below the BN254 scalar field modulus")
	}

	// Member must exist and be active.
	member, err := k.Member.Get(ctx, msg.Member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrMemberNotFound, msg.Member)
	}
	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(types.ErrMemberNotActive, "only active members can register ZK public keys")
	}

	// A tombstone can never re-register a key: the identity lives elsewhere.
	if k.IsTombstoned(ctx, msg.Member) {
		return nil, types.ErrMemberTombstoned
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// First registration (no key, nothing pending) takes effect at once.
	if len(member.ZkPublicKey) == 0 && len(member.PendingZkPublicKey) == 0 {
		member.ZkPublicKey = msg.ZkPublicKey
		if member.ZkKeyCooldownExempt {
			// The first registration after a RECOVERY or COUNCIL migration
			// cleared the key: it still waits out the activation delay so
			// a fresh nullifier cannot double-act inside an open window,
			// but it is exempt from the season cooldown.
			member.ZkPublicKey = nil
			if err := k.scheduleZkKeyActivation(ctx, &member, msg.Member, msg.ZkPublicKey, false); err != nil {
				return nil, err
			}
		}
		if err := k.Member.Set(ctx, msg.Member, member); err != nil {
			return nil, err
		}
		k.MarkMemberDirty(ctx, msg.Member)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("rep.zk_public_key_registered",
			sdk.NewAttribute("member", msg.Member),
			sdk.NewAttribute("activates_at_epoch", fmt.Sprintf("%d", member.ZkKeyActivatesAtEpoch)),
		))
		return &types.MsgRegisterZkPublicKeyResponse{}, nil
	}

	// A change of an existing key: at most one per season (unless a
	// migration cleared the key), the old leaf is zeroed now and the new key
	// activates after zk_key_activation_delay. Both rules close the
	// double-action hole a fresh nullifier would otherwise open
	// (member-key-migration plan, section 9).
	if len(member.PendingZkPublicKey) > 0 {
		return nil, types.ErrZkKeyPending
	}
	if err := k.scheduleZkKeyActivation(ctx, &member, msg.Member, msg.ZkPublicKey, true); err != nil {
		return nil, err
	}
	member.ZkPublicKey = nil
	if err := k.Member.Set(ctx, msg.Member, member); err != nil {
		return nil, err
	}
	k.MarkMemberDirty(ctx, msg.Member)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("rep.zk_public_key_change_scheduled",
		sdk.NewAttribute("member", msg.Member),
		sdk.NewAttribute("activates_at_epoch", fmt.Sprintf("%d", member.ZkKeyActivatesAtEpoch)),
	))
	return &types.MsgRegisterZkPublicKeyResponse{}, nil
}

// scheduleZkKeyActivation parks newKey on the member as pending, enforcing
// the once-per-season cooldown when applyCooldown is set, and enqueues the
// activation. The caller persists the member.
func (k Keeper) scheduleZkKeyActivation(ctx context.Context, member *types.Member, addr string, newKey []byte, applyCooldown bool) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	season, err := k.GetCurrentSeason(ctx)
	if err != nil {
		return err
	}
	if applyCooldown && !member.ZkKeyCooldownExempt && member.ZkKeyChanged && member.LastZkKeyChangeSeason == season {
		return errorsmod.Wrapf(types.ErrZkKeyCooldown, "already changed in season %d", season)
	}
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	member.PendingZkPublicKey = newKey
	member.ZkKeyActivatesAtEpoch = epoch + params.ZkKeyActivationDelay
	member.ZkKeyChanged = true
	member.LastZkKeyChangeSeason = season
	member.ZkKeyCooldownExempt = false
	return k.ZkKeyActivationQueue.Set(ctx, collections.Join(member.ZkKeyActivatesAtEpoch, addr))
}

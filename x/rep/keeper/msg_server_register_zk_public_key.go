package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Member must exist and be active.
	member, err := k.Member.Get(ctx, msg.Member)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrMemberNotFound, msg.Member)
	}
	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(types.ErrMemberNotActive, "only active members can register ZK public keys")
	}

	// Store the ZK public key on the member record.
	member.ZkPublicKey = msg.ZkPublicKey
	if err := k.Member.Set(ctx, msg.Member, member); err != nil {
		return nil, err
	}

	// Mark member dirty so the trust tree is rebuilt in the next EndBlocker.
	k.MarkMemberDirty(ctx, msg.Member)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("rep.zk_public_key_registered",
		sdk.NewAttribute("member", msg.Member),
	))

	return &types.MsgRegisterZkPublicKeyResponse{}, nil
}

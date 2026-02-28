package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RotateVoterKey(ctx context.Context, msg *types.MsgRotateVoterKey) (*types.MsgRotateVoterKeyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Voter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid voter address")
	}

	reg, err := k.VoterRegistration.Get(ctx, msg.Voter)
	if err != nil {
		return nil, types.ErrNotRegistered
	}

	if !reg.Active {
		return nil, types.ErrAlreadyInactive
	}

	// Check new key uniqueness.
	unique, err := k.isZkPubKeyUnique(ctx, msg.NewZkPublicKey, msg.Voter)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check key uniqueness")
	}
	if !unique {
		return nil, types.ErrDuplicatePublicKey
	}

	reg.ZkPublicKey = msg.NewZkPublicKey
	if len(msg.NewEncryptionPublicKey) > 0 {
		reg.EncryptionPublicKey = msg.NewEncryptionPublicKey
	}

	if err := k.VoterRegistration.Set(ctx, msg.Voter, reg); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update voter registration")
	}

	// Notify x/rep to rebuild this member's trust tree leaf (ZK key changed).
	k.repKeeper.MarkMemberDirty(ctx, msg.Voter)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventVoterKeyRotated,
		sdk.NewAttribute(types.AttributeVoter, msg.Voter),
	))

	return &types.MsgRotateVoterKeyResponse{}, nil
}

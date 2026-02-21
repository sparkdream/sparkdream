package keeper

import (
	"context"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RegisterVoter(ctx context.Context, msg *types.MsgRegisterVoter) (*types.MsgRegisterVoterResponse, error) {
	voterAddr, err := k.addressCodec.StringToBytes(msg.Voter)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid voter address")
	}

	// Check membership via x/rep.
	if !k.repKeeper.IsMember(ctx, voterAddr) {
		return nil, types.ErrNotAMember
	}

	// Check params: registration must be open.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}
	if !params.OpenRegistration {
		return nil, types.ErrRegistrationClosed
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check existing registration.
	existing, err := k.VoterRegistration.Get(ctx, msg.Voter)
	if err == nil {
		// Registration exists.
		if existing.Active {
			// Active: check if same key (already registered) or different key (use rotate).
			if bytesEqual(existing.ZkPublicKey, msg.ZkPublicKey) {
				return nil, types.ErrAlreadyRegistered
			}
			return nil, types.ErrUseRotateKey
		}
		// Inactive: reactivate with new keys.
		existing.ZkPublicKey = msg.ZkPublicKey
		existing.EncryptionPublicKey = msg.EncryptionPublicKey
		existing.Active = true
		existing.RegisteredAt = sdkCtx.BlockHeight()

		// Check key uniqueness.
		unique, err := k.isZkPubKeyUnique(ctx, msg.ZkPublicKey, msg.Voter)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to check key uniqueness")
		}
		if !unique {
			return nil, types.ErrDuplicatePublicKey
		}

		if err := k.VoterRegistration.Set(ctx, msg.Voter, existing); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update voter registration")
		}
	} else {
		// New registration: check key uniqueness.
		unique, err := k.isZkPubKeyUnique(ctx, msg.ZkPublicKey, "")
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to check key uniqueness")
		}
		if !unique {
			return nil, types.ErrDuplicatePublicKey
		}

		reg := types.VoterRegistration{
			Address:             msg.Voter,
			ZkPublicKey:         msg.ZkPublicKey,
			EncryptionPublicKey: msg.EncryptionPublicKey,
			RegisteredAt:        sdkCtx.BlockHeight(),
			Active:              true,
		}
		if err := k.VoterRegistration.Set(ctx, msg.Voter, reg); err != nil {
			return nil, errorsmod.Wrap(err, "failed to store voter registration")
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventVoterRegistered,
		sdk.NewAttribute(types.AttributeVoter, msg.Voter),
	))

	return &types.MsgRegisterVoterResponse{}, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

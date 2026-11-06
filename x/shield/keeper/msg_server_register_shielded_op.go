package keeper

import (
	"bytes"
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

func (k msgServer) RegisterShieldedOp(ctx context.Context, msg *types.MsgRegisterShieldedOp) (*types.MsgRegisterShieldedOpResponse, error) {
	authority, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}
	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, msg.Authority)
	}

	reg := msg.Registration
	if err := reg.Validate(); err != nil {
		return nil, err
	}

	// A nullifier domain keeps one scoping rule across every op that uses it,
	// including the op that last used it before being deregistered, whose
	// nullifiers are still stored. (Re-registering an op under a new domain
	// resets its uniqueness: the old nullifiers stay in the old domain.)
	if retired, err := k.RetiredDomains.Get(ctx, reg.NullifierDomain); err == nil {
		if err := types.CheckDomainCompatible(reg, retired); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	var conflict error
	if err := k.IterateShieldedOps(ctx, func(typeURL string, other types.ShieldedOpRegistration) bool {
		if typeURL == reg.MessageTypeUrl {
			return false
		}
		conflict = types.CheckDomainCompatible(reg, other)
		return conflict != nil
	}); err != nil {
		return nil, err
	}
	if conflict != nil {
		return nil, conflict
	}

	// An in-place update that keeps the op's domain keeps its scoping rule
	// too: the domain's stored nullifiers were written under it, and a new
	// scope type, field or window would prune or re-admit them. Changing
	// how an op is scoped needs a new domain.
	prev, hadPrev := k.GetShieldedOp(ctx, reg.MessageTypeUrl)
	if hadPrev {
		if err := types.CheckDomainCompatible(reg, prev); err != nil {
			return nil, err
		}
	}
	if err := k.SetShieldedOp(ctx, reg); err != nil {
		return nil, err
	}
	// The domain is live again; an update that moves the op to another
	// domain may leave its old domain unused.
	if err := k.RetiredDomains.Remove(ctx, reg.NullifierDomain); err != nil {
		return nil, err
	}
	if hadPrev && prev.NullifierDomain != reg.NullifierDomain {
		if err := k.retireDomainIfUnused(ctx, prev); err != nil {
			return nil, err
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldedOpRegistered,
		sdk.NewAttribute(types.AttributeKeyMessageType, reg.MessageTypeUrl),
	))

	return &types.MsgRegisterShieldedOpResponse{}, nil
}

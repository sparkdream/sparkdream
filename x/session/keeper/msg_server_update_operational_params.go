package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/session/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	authority, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Accept governance authority directly
	if !bytes.Equal(k.GetAuthority(), authority) {
		// TODO: Also accept Commons Council Operations Committee via commonsKeeper.IsCouncilAuthorized
		// For now, only governance authority is accepted. When x/commons integration is wired,
		// add: k.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations")
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, msg.Authority)
	}

	currentParams, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Ceiling enforcement: allowed_msg_types must be subset of current ceiling
	ceilingSet := make(map[string]bool, len(currentParams.MaxAllowedMsgTypes))
	for _, t := range currentParams.MaxAllowedMsgTypes {
		ceilingSet[t] = true
	}
	for _, t := range msg.OperationalParams.AllowedMsgTypes {
		if !ceilingSet[t] {
			return nil, types.ErrExceedsCeiling.Wrapf("type %s is not in ceiling", t)
		}
	}

	// Check NonDelegableSessionMsgs
	for _, t := range msg.OperationalParams.AllowedMsgTypes {
		if types.NonDelegableSessionMsgs[t] {
			return nil, types.ErrMsgTypeForbidden.Wrapf("type: %s", t)
		}
	}

	// Apply operational params (preserves ceiling)
	newParams := currentParams.ApplyOperationalParams(msg.OperationalParams)

	if err := newParams.Validate(); err != nil {
		return nil, err
	}

	if err := k.Params.Set(ctx, newParams); err != nil {
		return nil, err
	}

	return &types.MsgUpdateOperationalParamsResponse{}, nil
}

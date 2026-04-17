package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/session/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Accept governance authority or Commons Council Operations Committee.
	if !k.isCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s or Commons Operations Committee, got %s", expectedAuthorityStr, msg.Authority)
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

package keeper

import (
	"context"
	"strings"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AcceptTarget consents to being the resolver target for a name. The signer
// must equal the name's current `target` field. After acceptance, the signer
// is eligible to set this name as their primary for reverse resolution.
func (k msgServer) AcceptTarget(goCtx context.Context, msg *types.MsgAcceptTarget) (*types.MsgAcceptTargetResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	name := strings.ToLower(strings.TrimSpace(msg.Name))
	record, found := k.GetName(ctx, name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNameNotFound, "name '%s' not found", name)
	}
	if record.Target == "" {
		return nil, errorsmod.Wrapf(types.ErrTargetNotSet, "name '%s' has no resolver target", name)
	}
	if record.Target != msg.Authority {
		return nil, errorsmod.Wrapf(types.ErrNotTarget, "you are not the current target of '%s'", name)
	}
	if record.TargetAccepted {
		// Idempotent: already accepted, nothing to do.
		return &types.MsgAcceptTargetResponse{}, nil
	}

	record.TargetAccepted = true
	if err := k.SetName(ctx, record); err != nil {
		return nil, err
	}
	if err := k.AcceptedTargets.Set(ctx, collections.Join(record.Target, name)); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent("name_target_accepted",
			sdk.NewAttribute("name", name),
			sdk.NewAttribute("target", msg.Authority),
		),
	)

	return &types.MsgAcceptTargetResponse{}, nil
}

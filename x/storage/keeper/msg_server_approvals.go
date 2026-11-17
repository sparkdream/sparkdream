package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

// mayApprove reports whether signer may approve a live x/service operator:
// governance, or the controller x/service records for it at this moment.
func (k Keeper) mayApprove(signer, liveController string) bool {
	return k.isGov(signer) || (signer != "" && signer == liveController)
}

func (k msgServer) ApproveOperator(ctx context.Context, msg *types.MsgApproveOperator) (*types.MsgApproveOperatorResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	b, err := k.Backends.Get(ctx, msg.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(msg.Backend)
	}
	op, live := k.liveOperator(ctx, msg.Operator, b.ServiceType)
	if !live {
		return nil, types.ErrOperatorNotActive.Wrapf("no live %s operator at that address", b.ServiceType)
	}
	if !k.mayApprove(msg.Authority, op.Controller) {
		return nil, types.ErrUnauthorized.Wrap("the operator's controller or governance approves")
	}
	key := collections.Join(b.Name, msg.Operator)
	if has, err := k.ApprovedOperators.Has(ctx, key); err != nil {
		return nil, err
	} else if !has {
		n, err := k.countApprovedOperators(ctx, b.Name)
		if err != nil {
			return nil, err
		}
		if n >= p.MaxOperatorsPerBackend {
			return nil, types.ErrTooManyOperators
		}
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	// The approval records the controller: it is that council's consent to
	// be paid this operator's completions and to answer for them.
	if err := k.ApprovedOperators.Set(ctx, key, types.Approval{Controller: op.Controller, Height: now}); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.operator_approved",
		sdk.NewAttribute("backend", b.Name),
		sdk.NewAttribute("operator", msg.Operator),
		sdk.NewAttribute("controller", op.Controller),
	))
	return &types.MsgApproveOperatorResponse{}, nil
}

func (k msgServer) RemoveOperator(ctx context.Context, msg *types.MsgRemoveOperator) (*types.MsgRemoveOperatorResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	key := collections.Join(msg.Backend, msg.Operator)
	a, err := k.ApprovedOperators.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrOperatorNotApproved
		}
		return nil, err
	}
	if !k.isGov(msg.Authority) && msg.Authority != a.Controller {
		return nil, types.ErrUnauthorized.Wrap("the approving controller or governance removes")
	}
	if err := k.ApprovedOperators.Remove(ctx, key); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.operator_removed",
		sdk.NewAttribute("backend", msg.Backend),
		sdk.NewAttribute("operator", msg.Operator),
		sdk.NewAttribute("controller", a.Controller),
	))
	return &types.MsgRemoveOperatorResponse{}, nil
}

func (k msgServer) ApproveScanner(ctx context.Context, msg *types.MsgApproveScanner) (*types.MsgApproveScannerResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	op, live := k.liveOperator(ctx, msg.Scanner, types.ContentScannerServiceType)
	if !live {
		return nil, types.ErrOperatorNotActive.Wrap("no live content-scanner operator at that address")
	}
	if !k.mayApprove(msg.Authority, op.Controller) {
		return nil, types.ErrUnauthorized.Wrap("the scanner's controller or governance approves")
	}
	if has, err := k.ApprovedScanners.Has(ctx, msg.Scanner); err != nil {
		return nil, err
	} else if !has {
		total, _, err := k.countApprovedScanners(ctx)
		if err != nil {
			return nil, err
		}
		if total >= p.MaxApprovedScanners {
			return nil, types.ErrTooManyScanners
		}
	}
	now := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := k.ApprovedScanners.Set(ctx, msg.Scanner, types.Approval{Controller: op.Controller, Height: now}); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.scanner_approved",
		sdk.NewAttribute("scanner", msg.Scanner),
		sdk.NewAttribute("controller", op.Controller),
	))
	return &types.MsgApproveScannerResponse{}, nil
}

func (k msgServer) RemoveScanner(ctx context.Context, msg *types.MsgRemoveScanner) (*types.MsgRemoveScannerResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	a, err := k.ApprovedScanners.Get(ctx, msg.Scanner)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrScannerNotApproved
		}
		return nil, err
	}
	if !k.isGov(msg.Authority) && msg.Authority != a.Controller {
		return nil, types.ErrUnauthorized.Wrap("the approving controller or governance removes")
	}
	// The scanner's flags stay; each is lifted by its controller or governance.
	if err := k.ApprovedScanners.Remove(ctx, msg.Scanner); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.scanner_removed",
		sdk.NewAttribute("scanner", msg.Scanner),
		sdk.NewAttribute("controller", a.Controller),
	))
	return &types.MsgRemoveScannerResponse{}, nil
}

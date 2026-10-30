package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) SetMintPolicy(ctx context.Context, msg *types.MsgSetMintPolicy) (*types.MsgSetMintPolicyResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.MintingClosed {
		return nil, types.ErrMintingClosed
	}
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	if c.IsEditions() && msg.MintPolicy.PublicMintEnabled {
		return nil, errorsmod.Wrap(types.ErrWrongTokenModel, "editions classes have no public mint")
	}
	if err := k.validateMintPolicy(ctx, msg.MintPolicy, c.Flags); err != nil {
		return nil, err
	}
	next := k.normalizePolicy(ctx, msg.MintPolicy)
	// The curve is fixed once public minting has opened (docs/x-artifact-spec.md
	// §7.18): the owner may still disable, close or cap per address, but a
	// collector who read the curve at purchase sees the curve that runs.
	// A FIXED price is a curve too: it cannot be cut (or switched to a
	// curve) under the collectors who already paid it.
	if types.CurveOpened(c, now(ctx)) && !types.SameCurve(c.MintPolicy, next) {
		return nil, types.ErrMintPolicyLocked
	}
	c.MintPolicy = next
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMintPolicySet, classAttr(c.Id),
		sdk.NewAttribute("enabled", strconv.FormatBool(c.MintPolicy.PublicMintEnabled)),
		sdk.NewAttribute("price", c.MintPolicy.Price.String()),
		sdk.NewAttribute("start_time", i64(c.MintPolicy.StartTime)),
		sdk.NewAttribute("end_time", i64(c.MintPolicy.EndTime)),
		sdk.NewAttribute("per_address_limit", u64(uint64(c.MintPolicy.PerAddressLimit))),
		sdk.NewAttribute("pricing", c.MintPolicy.Pricing.String()))
	return &types.MsgSetMintPolicyResponse{}, nil
}

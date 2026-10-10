package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/artifact/types"
)

// validateMintPolicy checks a mint policy against the class flags.
func (k Keeper) validateMintPolicy(ctx context.Context, mp types.MintPolicy, flags types.ClassFlags) error {
	if mp.Price.Denom == "" {
		// An unset price means free; a bare amount without a denom is an error.
		if !mp.Price.Amount.IsNil() && !mp.Price.Amount.IsZero() {
			return errorsmod.Wrap(types.ErrInvalidDenom, "price denom is required")
		}
	} else if err := k.validatePrice(ctx, mp.Price, true); err != nil {
		return err
	}
	if mp.StartTime < 0 || mp.EndTime < 0 {
		return errorsmod.Wrap(types.ErrMintWindow, "times must be non-negative")
	}
	if mp.StartTime > 0 && mp.EndTime > 0 && mp.EndTime <= mp.StartTime {
		return errorsmod.Wrap(types.ErrMintWindow, "end_time must be after start_time")
	}
	if !flags.Transferable && !mp.Price.Amount.IsNil() && mp.Price.Amount.IsPositive() {
		return errorsmod.Wrap(types.ErrInvalidFlags, "soulbound classes cannot sell public mints")
	}
	return nil
}

// normalizePolicy fills an unset price with a zero bond-denom coin.
func (k Keeper) normalizePolicy(ctx context.Context, mp types.MintPolicy) types.MintPolicy {
	if mp.Price.Denom == "" {
		mp.Price = sdk.NewInt64Coin(k.BondDenom(ctx), 0)
	}
	return mp
}

// validateMinterSet deduplicates and validates a minter list.
func (k Keeper) validateMinterSet(minters []string, owner string, p types.Params) ([]string, error) {
	if uint32(len(minters)) > p.MaxMintersPerClass {
		return nil, errorsmod.Wrapf(types.ErrInvalidBatch, "at most %d minters", p.MaxMintersPerClass)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(minters))
	for _, m := range minters {
		if _, err := k.decodeAddr("minter", m); err != nil {
			return nil, err
		}
		if m == owner {
			return nil, errorsmod.Wrap(types.ErrInvalidBatch, "the owner is implicitly a minter")
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, nil
}

// validateClassContent validates the content fields of a class.
func validateClassContent(name, symbol, description, uri, uriHash, tokenURIBase string, p types.Params) error {
	if name == "" {
		return errorsmod.Wrap(types.ErrInvalidMetadata, "name is required")
	}
	if err := types.ValidateText("name", name, p.MaxNameLength, false); err != nil {
		return err
	}
	if err := types.ValidateSymbol(symbol, p.MaxSymbolLength); err != nil {
		return err
	}
	if err := types.ValidateText("description", description, p.MaxDescriptionLength, true); err != nil {
		return err
	}
	if err := types.ValidateURI("uri", uri, p); err != nil {
		return err
	}
	if err := types.ValidateURIHash("uri_hash", uriHash); err != nil {
		return err
	}
	return types.ValidateURI("token_uri_base", tokenURIBase, p)
}

func (k msgServer) CreateClass(ctx context.Context, msg *types.MsgCreateClass) (*types.MsgCreateClassResponse, error) {
	creator, err := k.decodeAddr("creator", msg.Creator)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)

	if !k.canCreateClass(ctx, creator, msg.Creator, p) {
		return nil, types.ErrInsufficientTrust
	}
	count, _ := k.ClassCountByCreator.Get(ctx, msg.Creator)
	if count >= uint64(p.MaxClassesPerCreator) {
		return nil, types.ErrTooManyClasses
	}
	if msg.AcceptedContentLicense != types.ContentLicense {
		return nil, errorsmod.Wrapf(types.ErrContentLicenseNotAccepted, "accepted_content_license must be %q", types.ContentLicense)
	}
	if err := msg.Flags.Validate(); err != nil {
		return nil, err
	}
	if msg.RoyaltyBps > p.MaxRoyaltyBps {
		return nil, errorsmod.Wrapf(types.ErrRoyaltyTooHigh, "max %d bps", p.MaxRoyaltyBps)
	}
	if !msg.Flags.Transferable && msg.RoyaltyBps > 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidFlags, "soulbound classes cannot carry a royalty")
	}
	if err := validateClassContent(msg.Name, msg.Symbol, msg.Description, msg.Uri, msg.UriHash, msg.TokenUriBase, p); err != nil {
		return nil, err
	}
	if err := k.validateMintPolicy(ctx, msg.MintPolicy, msg.Flags); err != nil {
		return nil, err
	}
	minters, err := k.validateMinterSet(msg.Minters, msg.Creator, p)
	if err != nil {
		return nil, err
	}
	payout := msg.PayoutAddress
	if payout == "" {
		payout = msg.Creator
	}
	if _, err := k.validateRecipient("payout_address", payout); err != nil {
		return nil, err
	}
	royaltyRecipients := msg.RoyaltyRecipients
	if len(royaltyRecipients) == 0 {
		royaltyRecipients = types.SoleRoyaltyRecipient(msg.Creator)
	}
	if err := k.validateRoyaltyShares(royaltyRecipients); err != nil {
		return nil, err
	}

	if err := k.burnFrom(ctx, creator, p.ClassCreationFee); err != nil {
		return nil, err
	}

	seq, err := k.ClassSeq.Next(ctx)
	if err != nil {
		return nil, err
	}
	t := now(ctx)
	c := types.Class{
		Id:                seq + 1, // class ids start at 1
		Owner:             msg.Creator,
		Creator:           msg.Creator,
		Name:              msg.Name,
		Symbol:            msg.Symbol,
		Description:       msg.Description,
		Uri:               msg.Uri,
		UriHash:           msg.UriHash,
		TokenUriBase:      msg.TokenUriBase,
		Flags:             msg.Flags,
		MaxSupply:         msg.MaxSupply,
		NextTokenId:       1,
		Minters:           minters,
		MintPolicy:        k.normalizePolicy(ctx, msg.MintPolicy),
		PayoutAddress:     payout,
		RoyaltyBps:        msg.RoyaltyBps,
		RoyaltyRecipients: royaltyRecipients,
		Status:            types.ContentStatus_CONTENT_STATUS_ACTIVE,
		MediaRulesVersion: commontypes.MediaRulesVersion,
		CreatedAt:         t,
		UpdatedAt:         t,
	}
	c.MediaFlags = types.LabelClass(c)
	if err := k.Classes.Set(ctx, c.Id, c); err != nil {
		return nil, err
	}
	if err := k.ClassesByOwner.Set(ctx, collections.Join(c.Owner, c.Id)); err != nil {
		return nil, err
	}
	if err := k.adjustCreatorCount(ctx, msg.Creator, 1); err != nil {
		return nil, err
	}

	emit(ctx, types.EventClassCreated, classAttr(c.Id),
		sdk.NewAttribute("creator", c.Creator),
		sdk.NewAttribute("transferable", strconv.FormatBool(c.Flags.Transferable)),
		sdk.NewAttribute("burn_authorization", c.Flags.BurnAuthorization.String()),
		sdk.NewAttribute("token_metadata_mutable", strconv.FormatBool(c.Flags.TokenMetadataMutable)),
		sdk.NewAttribute("max_supply", u64(c.MaxSupply)))
	return &types.MsgCreateClassResponse{ClassId: c.Id}, nil
}

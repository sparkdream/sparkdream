package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/artifact/types"
)

// CreateEdition adds an edition to an EDITIONS class (docs/x-artifact-spec.md
// §5.8.1). The record itself costs token_deposit, burned: editions are
// never deleted, so there is nothing to refund. With a claim the edition is
// an impact certificate.
func (k msgServer) CreateEdition(ctx context.Context, msg *types.MsgCreateEdition) (*types.MsgCreateEditionResponse, error) {
	minter, err := k.decodeAddr("minter", msg.Minter)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	c, err := k.mintPreconditions(ctx, msg.ClassId, msg.Minter, msg.AcceptedContentLicense, 1)
	if err != nil {
		return nil, err
	}
	if !c.IsEditions() {
		return nil, types.ErrWrongTokenModel
	}
	if err := types.ValidateTokenMetadata(msg.Metadata, p); err != nil {
		return nil, err
	}
	claim, err := k.validateImpactClaim(ctx, msg.Claim, msg.Minter, p)
	if err != nil {
		return nil, err
	}
	if err := k.burnFrom(ctx, minter, p.TokenDeposit); err != nil {
		return nil, err
	}
	e := types.Edition{
		ClassId:           c.Id,
		Id:                c.NextTokenId,
		Creator:           msg.Minter,
		Metadata:          msg.Metadata,
		MetadataFrozen:    tokenFrozen(c),
		MaxSupply:         msg.MaxSupply,
		Status:            types.ContentStatus_CONTENT_STATUS_ACTIVE,
		MediaRulesVersion: commontypes.MediaRulesVersion,
		CreatedAt:         now(ctx),
		Claim:             claim,
		RewardPerUnit:     math.ZeroInt(),
		RewardsFunded:     math.ZeroInt(),
	}
	e.MediaFlags = types.LabelToken(e.Metadata)
	c.NextTokenId++
	c.Supply++
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	if claim.InitiativeId != 0 {
		if err := k.CertificatesByInitiative.Set(ctx, collections.Join3(claim.InitiativeId, c.Id, e.Id)); err != nil {
			return nil, err
		}
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventEditionCreated, classAttr(c.Id), editionAttr(e.Id),
		sdk.NewAttribute("creator", msg.Minter), sdk.NewAttribute("max_supply", u64(e.MaxSupply)),
		sdk.NewAttribute("certificate", boolStr(!claim.IsEmpty())), sdk.NewAttribute("initiative_id", u64(claim.InitiativeId)),
		sdk.NewAttribute("metadata_hash", types.MetadataHash(e.Metadata)))
	return &types.MsgCreateEditionResponse{EditionId: e.Id}, nil
}

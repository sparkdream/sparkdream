package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// openMediaProposals counts a token's open proposals (bounded by the cap).
func (k Keeper) openMediaProposals(ctx context.Context, classID, tokenID uint64) uint32 {
	var n uint32
	_ = k.MediaProposals.Walk(ctx, collections.NewSuperPrefixedTripleRange[uint64, uint64, uint64](classID, tokenID),
		func(_ collections.Triple[uint64, uint64, uint64], _ types.MediaProposal) (bool, error) {
			n++
			return false, nil
		})
	return n
}

// ProposeMedia offers the holder an additional media version
// (docs/x-artifact-spec.md §7.17). The issuer can add options but never
// changes what the holder shows; the proposal waits until the holder
// accepts or rejects it. Capped per token; validated and labelled like
// metadata.
func (k msgServer) ProposeMedia(ctx context.Context, msg *types.MsgProposeMedia) (*types.MsgProposeMediaResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if isTargetHidden(c, t) {
		return nil, types.ErrContentHidden
	}
	if t.Unrevealed {
		return nil, types.ErrNotRevealed
	}
	p := k.GetParams(ctx)
	if err := types.ValidateTokenMetadata(msg.Metadata, p); err != nil {
		return nil, err
	}
	if k.openMediaProposals(ctx, t.ClassId, t.Id) >= p.MaxMediaProposalsPerToken {
		return nil, types.ErrTooManyMediaProposals
	}
	t.NextMediaProposalId++
	mp := types.MediaProposal{
		ClassId:   t.ClassId,
		TokenId:   t.Id,
		Id:        t.NextMediaProposalId,
		Proposer:  msg.Owner,
		Metadata:  msg.Metadata,
		CreatedAt: now(ctx),
	}
	if err := k.MediaProposals.Set(ctx, collections.Join3(t.ClassId, t.Id, mp.Id), mp); err != nil {
		return nil, err
	}
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMediaProposed, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("proposal_id", u64(mp.Id)), sdk.NewAttribute("proposer", msg.Owner),
		sdk.NewAttribute("metadata_hash", types.MetadataHash(msg.Metadata)))
	return &types.MsgProposeMediaResponse{ProposalId: mp.Id}, nil
}

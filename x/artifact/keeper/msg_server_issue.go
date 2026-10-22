package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Issue mints one shared metadata to many recipients (attendance and
// participation badges, docs/x-artifact-spec.md §5.2.3). It follows the
// MsgMint rules token for token but validates the metadata once and allows
// the larger max_issue_batch_size.
func (k msgServer) Issue(ctx context.Context, msg *types.MsgIssue) (*types.MsgIssueResponse, error) {
	minter, err := k.decodeAddr("minter", msg.Minter)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if len(msg.Recipients) == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidBatch, "recipients is empty")
	}
	if uint32(len(msg.Recipients)) > p.MaxIssueBatchSize {
		return nil, errorsmod.Wrapf(types.ErrInvalidBatch, "at most %d recipients", p.MaxIssueBatchSize)
	}
	seen := make(map[string]bool, len(msg.Recipients))
	for _, r := range msg.Recipients {
		if seen[r] {
			return nil, errorsmod.Wrapf(types.ErrInvalidBatch, "duplicate recipient %s", r)
		}
		seen[r] = true
	}
	c, err := k.mintPreconditions(ctx, msg.ClassId, msg.Minter, msg.AcceptedContentLicense, uint64(len(msg.Recipients)))
	if err != nil {
		return nil, err
	}
	if c.IsEditions() {
		return nil, errorsmod.Wrap(types.ErrWrongTokenModel, "editions classes issue units with MsgMintUnits")
	}
	if err := validateMintContent(ctx, c, msg.Metadata, nil, p); err != nil {
		return nil, err
	}

	results := make([]types.MintResult, 0, len(msg.Recipients))
	live := 0
	for _, r := range msg.Recipients {
		res, err := k.mintOne(ctx, &c, minter, msg.Minter, r, msg.Metadata, nil, p)
		if err != nil {
			return nil, err
		}
		if res.Live {
			live++
		}
		results = append(results, res)
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventIssued, classAttr(c.Id), sdk.NewAttribute("minter", msg.Minter),
		sdk.NewAttribute("count", u64(uint64(len(results)))), sdk.NewAttribute("live", u64(uint64(live))),
		sdk.NewAttribute("metadata_hash", types.MetadataHash(msg.Metadata)))
	return &types.MsgIssueResponse{Results: results}, nil
}

package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Reveal sets a blind-minted token's metadata (docs/x-artifact-spec.md
// §7.15). The leaf built from the class, the token id, the salt and the
// metadata must prove into the commitment fixed at class creation, so the
// issuer can neither change what a token was committed to nor swap
// metadata between tokens. A revealed token releases its escrowed mint
// price to the class, or refunds it to the holder when the reveal lands
// after the class deadline. Pending (inbox) mints are revealed in place.
func (k msgServer) Reveal(ctx context.Context, msg *types.MsgReveal) (*types.MsgRevealResponse, error) {
	if _, err := k.decodeAddr("signer", msg.Signer); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if c.Reveal.Kind != types.RevealKind_REVEAL_KIND_BLIND_METADATA {
		return nil, errorsmod.Wrap(types.ErrInvalidReveal, "class is not a blind mint")
	}
	if !k.isMinter(ctx, c, msg.Signer) {
		return nil, types.ErrNotMinter
	}
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	p := k.GetParams(ctx)
	if err := types.ValidateTokenMetadata(msg.Metadata, p); err != nil {
		return nil, err
	}
	salt, err := types.DecodeHexSecret("salt", msg.Salt)
	if err != nil {
		return nil, err
	}
	if len(msg.Proof) > types.MaxRevealProofDepth {
		return nil, errorsmod.Wrapf(types.ErrInvalidReveal, "proof deeper than %d", types.MaxRevealProofDepth)
	}
	proof := make([][]byte, len(msg.Proof))
	for i, h := range msg.Proof {
		if proof[i], err = types.DecodeHexHash("proof", h); err != nil {
			return nil, err
		}
	}
	root, err := types.DecodeHexHash("commitment", c.Reveal.Commitment)
	if err != nil {
		return nil, err
	}
	leaf := types.RevealLeaf(c.Id, msg.TokenId, salt, msg.Metadata)
	if !types.VerifyRevealProof(leaf, proof, root) {
		return nil, errorsmod.Wrap(types.ErrInvalidReveal, "proof does not match the class commitment")
	}
	hash := types.MetadataHash(msg.Metadata)

	key := collections.Join(msg.ClassId, msg.TokenId)
	if pm, err := k.PendingMints.Get(ctx, key); err == nil {
		if !pm.Unrevealed {
			return nil, types.ErrAlreadyRevealed
		}
		pm.Metadata, pm.Unrevealed = msg.Metadata, false
		if err := k.PendingMints.Set(ctx, key, pm); err != nil {
			return nil, err
		}
	} else {
		t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
		if err != nil {
			return nil, err
		}
		if !t.Unrevealed {
			return nil, types.ErrAlreadyRevealed
		}
		if t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			return nil, types.ErrContentHidden
		}
		t.Metadata = msg.Metadata
		t.Unrevealed = false
		t.MetadataFrozen = tokenFrozen(c)
		t.MediaFlags = types.LabelTokenAll(t)
		// Past the deadline the escrow is the holder's refund: a late
		// reveal pays it out instead of taking it (ART-205).
		if err := k.releaseOrRefund(ctx, c, &t); err != nil {
			return nil, err
		}
		if err := k.saveToken(ctx, t); err != nil {
			return nil, err
		}
	}
	emit(ctx, types.EventRevealed, classAttr(msg.ClassId), tokenAttr(msg.TokenId),
		sdk.NewAttribute("by", msg.Signer), sdk.NewAttribute("metadata_hash", hash))
	return &types.MsgRevealResponse{MetadataHash: hash}, nil
}

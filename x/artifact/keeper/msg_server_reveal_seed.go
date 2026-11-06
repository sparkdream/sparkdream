package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// RevealSeed discloses a generative class's seed (docs/x-artifact-spec.md
// §7.15). Its SHA-256 must equal the commitment fixed at creation. Every
// token's seed is then derivable, and the escrowed public-mint payments are
// released to the class by the EndBlocker release queue, capped per block,
// or refunded to the holders when the seed lands after the class deadline
// (their refund right does not lapse on a late reveal, ART-205).
func (k msgServer) RevealSeed(ctx context.Context, msg *types.MsgRevealSeed) (*types.MsgRevealSeedResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.Reveal.Kind != types.RevealKind_REVEAL_KIND_GENERATIVE_SEED {
		return nil, errorsmod.Wrap(types.ErrInvalidReveal, "class is not generative")
	}
	if c.Reveal.RevealedAt != 0 {
		return nil, types.ErrAlreadyRevealed
	}
	seed, err := types.DecodeHexSecret("seed", msg.Seed)
	if err != nil {
		return nil, err
	}
	if types.SeedCommitment(seed) != c.Reveal.Commitment {
		return nil, errorsmod.Wrap(types.ErrInvalidReveal, "seed does not match the class commitment")
	}
	c.Reveal.RevealedAt = now(ctx)
	c.Reveal.Seed = msg.Seed
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	if err := k.ClassReleaseQueue.Set(ctx, c.Id, 0); err != nil {
		return nil, err
	}
	emit(ctx, types.EventSeedRevealed, classAttr(c.Id), sdk.NewAttribute("seed", msg.Seed),
		sdk.NewAttribute("late", boolStr(lateSeedReveal(c))))
	return &types.MsgRevealSeedResponse{}, nil
}

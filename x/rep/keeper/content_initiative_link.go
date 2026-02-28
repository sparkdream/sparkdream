package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
)

// ValidateInitiativeReference checks if an initiative exists and is in a valid status
// for content linking (not COMPLETED, REJECTED, or ABANDONED).
func (k Keeper) ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error {
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return fmt.Errorf("initiative %d not found", initiativeID)
	}

	switch initiative.Status {
	case types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED,
		types.InitiativeStatus_INITIATIVE_STATUS_REJECTED,
		types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED:
		return fmt.Errorf("initiative %d is in terminal status %s", initiativeID, initiative.Status)
	}

	return nil
}

// RegisterContentInitiativeLink registers a content item as linked to an initiative.
// Called by x/blog (and later x/forum) when creating content with an initiative reference.
func (k Keeper) RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error {
	key := collections.Join(initiativeID, collections.Join(targetType, targetID))
	return k.ContentInitiativeLinks.Set(ctx, key)
}

// RemoveContentInitiativeLink removes a content-initiative link.
// Called when content is deleted or hidden.
func (k Keeper) RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error {
	key := collections.Join(initiativeID, collections.Join(targetType, targetID))
	return k.ContentInitiativeLinks.Remove(ctx, key)
}

// GetPropagatedConviction calculates the total conviction propagated from linked content
// to an initiative. Prefix-scans all content linked to the initiative, sums their
// content conviction from external stakers only, and multiplies by the propagation ratio.
//
// Anti-gaming: Content stakes from the initiative's assignee or project creator are
// excluded, preventing sybil networks from bypassing the external conviction requirement
// by routing conviction through the content layer.
func (k Keeper) GetPropagatedConviction(ctx context.Context, initiativeID uint64, assigneeAddr, creatorAddr string) (math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	if params.ConvictionPropagationRatio.IsZero() {
		return math.LegacyZeroDec(), nil
	}

	totalContentConviction := math.LegacyZeroDec()

	// Prefix scan: all content linked to this initiative
	rng := collections.NewPrefixedPairRange[uint64, collections.Pair[int32, uint64]](initiativeID)
	err = k.ContentInitiativeLinks.Walk(ctx, rng, func(key collections.Pair[uint64, collections.Pair[int32, uint64]]) (stop bool, err error) {
		targetType := types.StakeTargetType(key.K2().K1())
		targetID := key.K2().K2()

		// Get external-only content conviction (filters out affiliated stakers)
		conviction, err := k.GetExternalContentConviction(ctx, targetType, targetID, assigneeAddr, creatorAddr)
		if err != nil {
			return false, nil // skip items that error
		}

		totalContentConviction = totalContentConviction.Add(conviction)
		return false, nil
	})
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	// Apply propagation ratio
	propagated := totalContentConviction.Mul(params.ConvictionPropagationRatio)
	return propagated, nil
}

// GetContentInitiativeLinks returns all content items linked to an initiative.
func (k Keeper) GetContentInitiativeLinks(ctx context.Context, initiativeID uint64) ([]ContentInitiativeLinkInfo, error) {
	var links []ContentInitiativeLinkInfo

	rng := collections.NewPrefixedPairRange[uint64, collections.Pair[int32, uint64]](initiativeID)
	err := k.ContentInitiativeLinks.Walk(ctx, rng, func(key collections.Pair[uint64, collections.Pair[int32, uint64]]) (stop bool, err error) {
		links = append(links, ContentInitiativeLinkInfo{
			TargetType: key.K2().K1(),
			TargetID:   key.K2().K2(),
		})
		return false, nil
	})

	return links, err
}

// ContentInitiativeLinkInfo holds info about a content-initiative link.
type ContentInitiativeLinkInfo struct {
	TargetType int32
	TargetID   uint64
}

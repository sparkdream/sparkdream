package app

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	artifacttypes "sparkdream/x/artifact/types"
	seasonmodulekeeper "sparkdream/x/season/keeper"
)

// ArtifactSeasonAdapter bridges x/season to the narrow
// artifacttypes.SeasonKeeper renderer state v2 reads
// (docs/x-artifact-spec.md §7.16). Read-only.
type ArtifactSeasonAdapter struct {
	keeper *seasonmodulekeeper.Keeper
}

// NewArtifactSeasonAdapter wraps the app's season keeper.
func NewArtifactSeasonAdapter(k *seasonmodulekeeper.Keeper) ArtifactSeasonAdapter {
	return ArtifactSeasonAdapter{keeper: k}
}

var _ artifacttypes.SeasonKeeper = ArtifactSeasonAdapter{}

// CurrentSeason returns the current season's number and status.
func (a ArtifactSeasonAdapter) CurrentSeason(ctx context.Context) (artifacttypes.SeasonInfo, error) {
	s, err := a.keeper.Season.Get(ctx)
	if err != nil {
		return artifacttypes.SeasonInfo{}, err
	}
	return artifacttypes.SeasonInfo{Number: s.Number, Status: s.Status.String()}, nil
}

// MemberSeason returns a member's level and XP this season.
func (a ArtifactSeasonAdapter) MemberSeason(ctx context.Context, addr string) (artifacttypes.MemberSeason, bool, error) {
	p, err := a.keeper.MemberProfile.Get(ctx, addr)
	if errors.Is(err, collections.ErrNotFound) {
		return artifacttypes.MemberSeason{}, false, nil
	} else if err != nil {
		return artifacttypes.MemberSeason{}, false, err
	}
	return artifacttypes.MemberSeason{Level: p.SeasonLevel, XP: p.SeasonXp}, true, nil
}

package app

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	artifacttypes "sparkdream/x/artifact/types"
	sessionmodulekeeper "sparkdream/x/session/keeper"
	sessiontypes "sparkdream/x/session/types"
)

// ArtifactSessionAdapter bridges x/session to the narrow
// artifacttypes.SessionKeeper Harberger taxes use (docs/x-artifact-spec.md
// §7.24). Claims go through ClaimRecurringPullForGrantee, which requires
// the artifact module address in session's authorized_grant_creators.
type ArtifactSessionAdapter struct {
	keeper sessionmodulekeeper.Keeper
}

// NewArtifactSessionAdapter wraps the app's session keeper.
func NewArtifactSessionAdapter(k sessionmodulekeeper.Keeper) ArtifactSessionAdapter {
	return ArtifactSessionAdapter{keeper: k}
}

var _ artifacttypes.SessionKeeper = ArtifactSessionAdapter{}

// RecurringPull describes a RecurringPull grant; found is false for a
// missing grant or one of another type.
func (a ArtifactSessionAdapter) RecurringPull(ctx context.Context, grantID uint64) (artifacttypes.RecurringPullInfo, bool, error) {
	g, err := a.keeper.GetGrant(ctx, grantID)
	if errors.Is(err, sessiontypes.ErrGrantNotFound) {
		return artifacttypes.RecurringPullInfo{}, false, nil
	} else if err != nil {
		return artifacttypes.RecurringPullInfo{}, false, err
	}
	rp := g.GetRecurringPull()
	if rp == nil {
		return artifacttypes.RecurringPullInfo{}, false, nil
	}
	return artifacttypes.RecurringPullInfo{
		Granter:       g.Granter,
		Grantee:       g.Grantee,
		Active:        g.Status == sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE,
		Amount:        rp.AmountPerPeriod,
		PeriodSeconds: rp.PeriodSeconds,
		NextClaim:     rp.LastClaimAdvance + rp.PeriodSeconds,
		ExpiresAt:     g.ExpiresAt.Unix(),
	}, true, nil
}

// ClaimRecurringPull claims one due period of a grant made to the artifact
// module and returns the amount pulled. A claim that moved nothing (the
// granter was underfunded and the grant paused, or its window closed)
// returns ErrTaxPullFailed; session keeps that transition.
func (a ArtifactSessionAdapter) ClaimRecurringPull(ctx context.Context, grantID uint64) (sdk.Coin, error) {
	g, err := a.keeper.GetGrant(ctx, grantID)
	if err != nil {
		return sdk.Coin{}, err
	}
	rp := g.GetRecurringPull()
	if rp == nil {
		return sdk.Coin{}, sessiontypes.ErrGrantTypeMismatch
	}
	addr := authtypes.NewModuleAddress(artifacttypes.ModuleName).String()
	resp, err := a.keeper.ClaimRecurringPullForGrantee(ctx, addr, grantID, addr)
	if err != nil {
		return sdk.Coin{}, err
	}
	switch {
	case resp.Paused:
		return sdk.Coin{}, artifacttypes.ErrTaxPullFailed.Wrap("the holder could not cover the tax; the grant is paused")
	case resp.Completed:
		return sdk.Coin{}, artifacttypes.ErrTaxPullFailed.Wrap("the tax grant has ended")
	}
	return rp.AmountPerPeriod, nil
}

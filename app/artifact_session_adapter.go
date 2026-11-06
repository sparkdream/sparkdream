package app

import (
	"context"
	"errors"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	artifacttypes "sparkdream/x/artifact/types"
	sessionmodulekeeper "sparkdream/x/session/keeper"
	sessiontypes "sparkdream/x/session/types"
)

// artifactSessionBank is the slice of x/bank the adapter reads to report
// what a claim actually moved.
type artifactSessionBank interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// ArtifactSessionAdapter bridges x/session to the narrow
// artifacttypes.SessionKeeper Harberger taxes use (docs/x-artifact-spec.md
// §7.24). Claims go through ClaimRecurringPullForGrantee, which requires
// the artifact module address in session's authorized_grant_creators.
type ArtifactSessionAdapter struct {
	keeper sessionmodulekeeper.Keeper
	bank   artifactSessionBank
}

// NewArtifactSessionAdapter wraps the app's session keeper; bank is read
// for the balance delta a claim produced.
func NewArtifactSessionAdapter(k sessionmodulekeeper.Keeper, bank artifactSessionBank) ArtifactSessionAdapter {
	return ArtifactSessionAdapter{keeper: k, bank: bank}
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
	maxPerEpoch, ok := sdkmath.NewIntFromString(rp.MaxPerEpoch)
	if !ok {
		maxPerEpoch = sdkmath.ZeroInt()
	}
	return artifacttypes.RecurringPullInfo{
		Granter:       g.Granter,
		Grantee:       g.Grantee,
		Active:        g.Status == sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE,
		Amount:        rp.AmountPerPeriod,
		PeriodSeconds: rp.PeriodSeconds,
		NextClaim:     rp.LastClaimAdvance + rp.PeriodSeconds,
		ExpiresAt:     g.ExpiresAt.Unix(),
		MaxPerEpoch:   maxPerEpoch,
	}, true, nil
}

// ClaimRecurringPull claims one due period of a grant made to the artifact
// module and returns what the claim moved into the module account (the
// balance delta, not the grant's nominal amount, so a session catch-up
// would never strand a surplus). A claim that moved nothing (the granter
// was underfunded and the grant paused, or its window closed) returns
// ErrTaxPullFailed; session keeps that transition. A claim session's
// per-day ceiling refuses returns ErrTaxPullDeferred: nothing changed and
// the pull is still due.
func (a ArtifactSessionAdapter) ClaimRecurringPull(ctx context.Context, grantID uint64) (sdk.Coin, error) {
	g, err := a.keeper.GetGrant(ctx, grantID)
	if err != nil {
		return sdk.Coin{}, err
	}
	rp := g.GetRecurringPull()
	if rp == nil {
		return sdk.Coin{}, sessiontypes.ErrGrantTypeMismatch
	}
	moduleAddr := authtypes.NewModuleAddress(artifacttypes.ModuleName)
	addr := moduleAddr.String()
	denom := rp.AmountPerPeriod.Denom
	before := a.bank.GetBalance(ctx, moduleAddr, denom)
	resp, err := a.keeper.ClaimRecurringPullForGrantee(ctx, addr, grantID, addr)
	if errors.Is(err, sessiontypes.ErrEpochCeilingExceeded) {
		return sdk.Coin{}, artifacttypes.ErrTaxPullDeferred.Wrap(err.Error())
	} else if err != nil {
		return sdk.Coin{}, err
	}
	switch {
	case resp.Paused:
		return sdk.Coin{}, artifacttypes.ErrTaxPullFailed.Wrap("the holder could not cover the tax; the grant is paused")
	case resp.Completed:
		return sdk.Coin{}, artifacttypes.ErrTaxPullFailed.Wrap("the tax grant has ended")
	}
	moved := a.bank.GetBalance(ctx, moduleAddr, denom).Amount.Sub(before.Amount)
	if !moved.IsPositive() {
		return sdk.Coin{}, artifacttypes.ErrTaxPullFailed.Wrap("the claim moved nothing")
	}
	return sdk.NewCoin(denom, moved), nil
}

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/federation
// (member-key-migration plan §8, federation row). Remote peers attested the
// OLD address, so nothing is carried over: identity links, reputation
// attestations and pending identity challenges of the old address are
// dropped and the new address re-links over IBC. Content-by-creator and
// verifier activity stay lazy; the verifier bond moves with x/rep's
// BondedRoles.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/federation migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration refuses a target that already holds identity links.
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	if n, _ := h.k.IdentityLinkCount.Get(ctx, to.String()); n > 0 {
		return fmt.Errorf("x/federation: %s already has %d identity link(s)", to.String(), n)
	}
	return nil
}

// AfterMemberMigration drops the old address's links, attestations and
// pending challenges.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr := from.String()
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	var peers []string
	rng := collections.NewPrefixedPairRange[string, string](fromStr)
	if err := k.IdentityLinks.Walk(ctx, rng, func(key collections.Pair[string, string], _ types.IdentityLink) (bool, error) {
		peers = append(peers, key.K2())
		return false, nil
	}); err != nil {
		return err
	}
	for _, peerID := range peers {
		k.removeIdentityLink(ctx, sdkCtx, fromStr, peerID, params)
	}
	if err := k.IdentityLinkCount.Remove(ctx, fromStr); err != nil {
		return err
	}

	var attPeers []string
	if err := k.RepAttestations.Walk(ctx, rng, func(key collections.Pair[string, string], _ types.ReputationAttestation) (bool, error) {
		attPeers = append(attPeers, key.K2())
		return false, nil
	}); err != nil {
		return err
	}
	for _, peerID := range attPeers {
		k.removeRepAttestation(ctx, fromStr, peerID)
	}

	var challengePeers []string
	if err := k.PendingIdChallenges.Walk(ctx, rng, func(key collections.Pair[string, string], _ types.PendingIdentityChallenge) (bool, error) {
		challengePeers = append(challengePeers, key.K2())
		return false, nil
	}); err != nil {
		return err
	}
	for _, peerID := range challengePeers {
		k.removePendingChallenge(ctx, fromStr, peerID)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("federation_identity_dropped_on_migration",
		sdk.NewAttribute("from", fromStr),
		sdk.NewAttribute("to", to.String()),
		sdk.NewAttribute("links", fmt.Sprintf("%d", len(peers))),
		sdk.NewAttribute("attestations", fmt.Sprintf("%d", len(attPeers))),
		sdk.NewAttribute("pending_challenges", fmt.Sprintf("%d", len(challengePeers))),
	))
	return nil
}

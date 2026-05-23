package app

import (
	"context"

	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypesv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// guardian_adapters.go bridges concrete SDK keepers to the GetParams-shaped
// interfaces guardian expects (see x/guardian/types/expected_keepers.go).
// Several SDK keepers expose Params via a collections.Item rather than a
// method, or via a method without an error return; these wrappers normalize
// to (Params, error). Mint and slashing already match the interface
// directly and are wired without adapters.

// DistrKeeperAdapterForGuardian wraps the upstream distribution keeper to
// satisfy guardiantypes.DistrKeeper. Distinct from the existing
// DistrKeeperAdapter in service_adapters.go (which serves x/service +
// x/split with a different surface area) so each guardian filter has a
// minimal-surface adapter rather than a shared maximal one.
type DistrKeeperAdapterForGuardian struct {
	k distrkeeper.Keeper
}

// NewDistrKeeperAdapterForGuardian wraps a distrkeeper.Keeper for guardian.
func NewDistrKeeperAdapterForGuardian(k distrkeeper.Keeper) DistrKeeperAdapterForGuardian {
	return DistrKeeperAdapterForGuardian{k: k}
}

// GetParams implements guardiantypes.DistrKeeper.
func (a DistrKeeperAdapterForGuardian) GetParams(ctx context.Context) (distrtypes.Params, error) {
	return a.k.Params.Get(ctx)
}

// GovKeeperAdapterForGuardian wraps the upstream gov keeper to satisfy
// guardiantypes.GovKeeper.
type GovKeeperAdapterForGuardian struct {
	k *govkeeper.Keeper
}

// NewGovKeeperAdapterForGuardian wraps a govkeeper.Keeper for guardian.
func NewGovKeeperAdapterForGuardian(k *govkeeper.Keeper) GovKeeperAdapterForGuardian {
	return GovKeeperAdapterForGuardian{k: k}
}

// GetParams implements guardiantypes.GovKeeper.
func (a GovKeeperAdapterForGuardian) GetParams(ctx context.Context) (govtypesv1.Params, error) {
	return a.k.Params.Get(ctx)
}

// AuthKeeperAdapterForGuardian wraps the upstream auth (account) keeper to
// satisfy guardiantypes.AuthKeeper. The upstream GetParams returns no error;
// the adapter adopts the (Params, error) signature for interface uniformity.
type AuthKeeperAdapterForGuardian struct {
	k authkeeper.AccountKeeper
}

// NewAuthKeeperAdapterForGuardian wraps an authkeeper.AccountKeeper for guardian.
func NewAuthKeeperAdapterForGuardian(k authkeeper.AccountKeeper) AuthKeeperAdapterForGuardian {
	return AuthKeeperAdapterForGuardian{k: k}
}

// GetParams implements guardiantypes.AuthKeeper.
func (a AuthKeeperAdapterForGuardian) GetParams(ctx context.Context) (authtypes.Params, error) {
	return a.k.GetParams(ctx), nil
}

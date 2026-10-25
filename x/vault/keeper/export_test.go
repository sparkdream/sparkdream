package keeper_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

func keeperQuery(f *fixture) types.QueryServer { return keeper.NewQueryServerImpl(f.keeper) }

func keeperVaultInvariant(f *fixture) sdk.Invariant { return keeper.VaultIndexesInvariant(f.keeper) }
func keeperProposalInvariant(f *fixture) sdk.Invariant {
	return keeper.ProposalIndexesInvariant(f.keeper)
}

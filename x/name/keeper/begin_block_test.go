package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/types"
)

func TestBeginBlocker_AutoResolvesExpiredDispute(t *testing.T) {
	f := initFixture(t)

	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()
	ownerAddr := sdk.AccAddress([]byte("owner_address_______"))
	owner := ownerAddr.String()

	// Create name owned by owner
	f.keeper.SetName(f.ctx, types.NameRecord{Name: "alice", Owner: owner})
	f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "alice"))

	// Create dispute filed at block 100 (timeout = 100800, so deadline = 100900)
	f.keeper.SetDispute(f.ctx, types.Dispute{
		Name:        "alice",
		Claimant:    claimant,
		FiledAt:     100,
		StakeAmount: math.NewInt(50),
		Active:      true,
	})
	challengeID := fmt.Sprintf("name_dispute:%s:%d", "alice", 100)
	f.keeper.DisputeStakes.Set(f.ctx, challengeID, types.DisputeStake{
		ChallengeId: challengeID,
		Staker:      claimant,
		Amount:      math.NewInt(50),
	})

	// Set block height past deadline
	ctx := f.ctx.WithBlockHeader(cmtproto.Header{Height: 101000})

	err := f.keeper.BeginBlocker(ctx)
	require.NoError(t, err)

	// Dispute should be inactive
	dispute, found := f.keeper.GetDispute(ctx, "alice")
	require.True(t, found)
	require.False(t, dispute.Active, "Dispute should be auto-resolved")

	// Name should be transferred to claimant
	record, found := f.keeper.GetName(ctx, "alice")
	require.True(t, found)
	require.Equal(t, claimant, record.Owner, "Name should transfer to claimant on timeout")

	// Claimant's stake should be unlocked (returned)
	unlocked := f.mockRep.UnlockedDREAM[claimant]
	require.True(t, unlocked.Equal(math.NewInt(50)), "Claimant stake should be unlocked")
}

func TestBeginBlocker_DoesNotResolveWithinTimeout(t *testing.T) {
	f := initFixture(t)

	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()
	ownerAddr := sdk.AccAddress([]byte("owner_address_______"))
	owner := ownerAddr.String()

	f.keeper.SetName(f.ctx, types.NameRecord{Name: "bob", Owner: owner})
	f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "bob"))
	f.keeper.SetDispute(f.ctx, types.Dispute{
		Name:        "bob",
		Claimant:    claimant,
		FiledAt:     100,
		StakeAmount: math.NewInt(50),
		Active:      true,
	})

	// Set block height within deadline (100 + 100800 = 100900)
	ctx := f.ctx.WithBlockHeader(cmtproto.Header{Height: 50000})

	err := f.keeper.BeginBlocker(ctx)
	require.NoError(t, err)

	// Dispute should still be active
	dispute, found := f.keeper.GetDispute(ctx, "bob")
	require.True(t, found)
	require.True(t, dispute.Active, "Dispute should still be active")

	// Name should still be with owner
	record, found := f.keeper.GetName(ctx, "bob")
	require.True(t, found)
	require.Equal(t, owner, record.Owner)

	// No DREAM operations should have occurred
	require.Empty(t, f.mockRep.UnlockedDREAM, "No stakes should be unlocked")
}

func TestBeginBlocker_SkipsContestedDisputes(t *testing.T) {
	f := initFixture(t)

	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()
	ownerAddr := sdk.AccAddress([]byte("owner_address_______"))
	owner := ownerAddr.String()

	f.keeper.SetName(f.ctx, types.NameRecord{Name: "carol", Owner: owner})
	f.keeper.OwnerNames.Set(f.ctx, collections.Join(owner, "carol"))
	f.keeper.SetDispute(f.ctx, types.Dispute{
		Name:               "carol",
		Claimant:           claimant,
		FiledAt:            100,
		StakeAmount:        math.NewInt(50),
		Active:             true,
		ContestChallengeId: "name_contest:carol:200", // Contested!
		ContestedAt:        200,
	})

	// Set block height past deadline
	ctx := f.ctx.WithBlockHeader(cmtproto.Header{Height: 200000})

	err := f.keeper.BeginBlocker(ctx)
	require.NoError(t, err)

	// Dispute should still be active (jury handles contested disputes)
	dispute, found := f.keeper.GetDispute(ctx, "carol")
	require.True(t, found)
	require.True(t, dispute.Active, "Contested dispute should NOT be auto-resolved")
}

func TestBeginBlocker_SkipsAlreadyResolved(t *testing.T) {
	f := initFixture(t)

	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()

	f.keeper.SetDispute(f.ctx, types.Dispute{
		Name:        "dave",
		Claimant:    claimant,
		FiledAt:     100,
		StakeAmount: math.NewInt(50),
		Active:      false, // Already resolved
	})

	ctx := f.ctx.WithBlockHeader(cmtproto.Header{Height: 200000})

	err := f.keeper.BeginBlocker(ctx)
	require.NoError(t, err)

	// Should remain inactive (not touched again)
	dispute, found := f.keeper.GetDispute(ctx, "dave")
	require.True(t, found)
	require.False(t, dispute.Active)

	// No DREAM operations
	require.Empty(t, f.mockRep.UnlockedDREAM)
}

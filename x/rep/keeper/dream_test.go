package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestGetCurrentEpoch tests epoch calculation
func TestGetCurrentEpoch(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Get params
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	// Default EpochBlocks should be set
	require.Greater(t, params.EpochBlocks, int64(0))

	// At block 0, epoch should be 0
	epoch, err := k.GetCurrentEpoch(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), epoch)

	// Advance to next epoch boundary
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks)
	ctx = sdkCtx

	epoch, err = k.GetCurrentEpoch(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), epoch)

	// Test at multiple epochs
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 5)
	ctx = sdkCtx

	epoch, err = k.GetCurrentEpoch(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(5), epoch)
}

// TestGetCurrentEpoch_ZeroEpochBlocks tests division by zero protection
func TestGetCurrentEpoch_ZeroEpochBlocks(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Set EpochBlocks to 0
	params, _ := k.Params.Get(ctx)
	params.EpochBlocks = 0
	k.Params.Set(ctx, params)

	// Should return 0 without error
	epoch, err := k.GetCurrentEpoch(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), epoch)
}

// TestApplyPendingDecay tests decay calculation
func TestApplyPendingDecay(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member with balance, past grace period (LastDecayEpoch=30, advance to epoch 31)
	member := types.Member{
		Address:        sdk.AccAddress([]byte("test")).String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		LastDecayEpoch: 30,
	}

	// Move to epoch 31 (1 epoch elapsed, past 30-epoch grace period)
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 31)
	ctx = sdkCtx

	// Apply decay
	err := k.ApplyPendingDecay(ctx, &member)
	require.NoError(t, err)

	// Verify decay applied (with 0.2% unstaked decay rate)
	// balance * (1 - 0.002)^1 = 1000 * 0.998 = 998
	expectedBalance := math.NewInt(998)
	require.Equal(t, expectedBalance.String(), member.DreamBalance.String())
	require.Equal(t, int64(31), member.LastDecayEpoch)

	// Verify lifetime burned updated
	expectedBurned := math.NewInt(2)
	require.Equal(t, expectedBurned.String(), member.LifetimeBurned.String())
}

// TestApplyPendingDecay_MultipleEpochs tests compound decay
func TestApplyPendingDecay_MultipleEpochs(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member past grace period (LastDecayEpoch=30, advance to epoch 33)
	member := types.Member{
		Address:        sdk.AccAddress([]byte("test")).String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		LastDecayEpoch: 30,
	}

	// Move to epoch 33 (3 epochs elapsed, past grace period)
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 33)
	ctx = sdkCtx

	// Apply decay
	err := k.ApplyPendingDecay(ctx, &member)
	require.NoError(t, err)

	// Verify compound decay: 1000 * (1 - 0.002)^3 = 1000 * 0.994012 ≈ 994.011
	expectedBalance := math.NewInt(994) // Truncated
	require.Equal(t, expectedBalance.String(), member.DreamBalance.String())
	require.Equal(t, int64(33), member.LastDecayEpoch)
}

// TestApplyPendingDecay_WithStakedBalance pins that the lazy member pass only
// decays the UNSTAKED portion: staked decay is owned by decayStakes, which
// shrinks the stake records and the aggregate together once per epoch. Here the
// member holds a synthetic aggregate with no stake records behind it, so
// nothing decays the staked part at all.
func TestApplyPendingDecay_WithStakedBalance(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member with 1000 total, 600 staked, 400 unstaked (past grace period)
	member := types.Member{
		Address:        sdk.AccAddress([]byte("test")).String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(600)),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		LastDecayEpoch: 30,
	}

	// Move to epoch 31 (1 epoch elapsed, past grace period)
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 31)
	ctx = sdkCtx

	// Apply decay
	err := k.ApplyPendingDecay(ctx, &member)
	require.NoError(t, err)

	// Unstaked 400 decays at 0.2%: 400 * 0.998 = 399.2 → 399 (truncated)
	// Unstaked decay = 400 - 399 = 1
	// Staked 600 is untouched by this pass (decayStakes owns staked decay and
	// only runs where actual stake records exist to shrink).
	expectedBalance := math.NewInt(999)
	require.Equal(t, expectedBalance.String(), member.DreamBalance.String())

	require.Equal(t, math.NewInt(600).String(), member.StakedDream.String(),
		"the lazy member pass must not decay the staked aggregate")

	// Verify only the unstaked DREAM was burned
	require.Equal(t, math.NewInt(1).String(), member.LifetimeBurned.String())
}

// TestApplyPendingDecay_NoDecayWhenUpToDate tests no decay when already current
func TestApplyPendingDecay_NoDecayWhenUpToDate(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	initialBalance := math.NewInt(1000)
	member := types.Member{
		Address:        sdk.AccAddress([]byte("test")).String(),
		DreamBalance:   PtrInt(initialBalance),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		LastDecayEpoch: 5,
	}

	// Set current epoch to 5 (same as LastDecayEpoch)
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 5)
	ctx = sdkCtx

	// Apply decay
	err := k.ApplyPendingDecay(ctx, &member)
	require.NoError(t, err)

	// Balance should be unchanged
	require.Equal(t, initialBalance.String(), member.DreamBalance.String())
	require.Equal(t, int64(5), member.LastDecayEpoch)
}

// TestGetBalance tests balance retrieval with decay
func TestGetBalance(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member past grace period
	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		LastDecayEpoch: 30,
	})

	// Move to epoch 31 (1 epoch elapsed, past grace period)
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 31)
	ctx = sdkCtx

	// Get balance (should apply decay)
	balance, err := k.GetBalance(ctx, addr)
	require.NoError(t, err)

	// Should return decayed balance: 1000 * (1 - 0.002) = 1000 * 0.998 = 998
	expectedBalance := math.NewInt(998)
	require.Equal(t, expectedBalance.String(), balance.String())

	// Verify member was updated in store
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, expectedBalance.String(), member.DreamBalance.String())
	require.Equal(t, int64(31), member.LastDecayEpoch)
}

// TestGetBalance_NonExistentMember tests getting balance of non-member
func TestGetBalance_NonExistentMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("nonexistent"))

	// Should return 0 without error
	balance, err := k.GetBalance(ctx, addr)
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), balance.String())
}

// TestMintDREAM tests minting DREAM tokens
func TestMintDREAM(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.NewInt(50)),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		LastDecayEpoch: 0,
	})

	// Mint 500 DREAM
	mintAmount := math.NewInt(500)
	err := k.MintDREAM(ctx, addr, mintAmount)
	require.NoError(t, err)

	// Verify balance updated
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(600).String(), member.DreamBalance.String())

	// Verify lifetime earned updated
	require.Equal(t, math.NewInt(550).String(), member.LifetimeEarned.String())

	// Verify event emitted
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	events := sdkCtx.EventManager().Events()
	require.Greater(t, len(events), 0)

	// Find mint_dream event
	var found bool
	for _, event := range events {
		if event.Type == "mint_dream" {
			found = true
			require.Equal(t, addr.String(), event.Attributes[0].Value)
			require.Equal(t, mintAmount.String(), event.Attributes[1].Value)
		}
	}
	require.True(t, found, "mint_dream event should be emitted")
}

// TestMintDREAM_InvalidAmount tests minting with invalid amounts
func TestMintDREAM_InvalidAmount(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Test zero amount
	err := k.MintDREAM(ctx, addr, math.ZeroInt())
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	// Test negative amount
	err = k.MintDREAM(ctx, addr, math.NewInt(-100))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

// TestMintDREAM_NonExistentMember tests minting to non-member
func TestMintDREAM_NonExistentMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("nonexistent"))

	err := k.MintDREAM(ctx, addr, math.NewInt(100))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

// TestMintDREAM_AppliesDecayFirst tests decay applied before mint
func TestMintDREAM_AppliesDecayFirst(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		LastDecayEpoch: 30,
	})

	// Move to epoch 31 (1 epoch elapsed, past grace period)
	params, _ := k.Params.Get(ctx)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithBlockHeight(params.EpochBlocks * 31)
	ctx = sdkCtx

	// Mint 100 DREAM
	err := k.MintDREAM(ctx, addr, math.NewInt(100))
	require.NoError(t, err)

	// Balance should be: (1000 * 0.998) + 100 = 998 + 100 = 1098
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1098).String(), member.DreamBalance.String())

	// Verify decay was applied
	require.Equal(t, int64(31), member.LastDecayEpoch)
}

// TestBurnDREAM tests burning DREAM tokens
func TestBurnDREAM(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create member
	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.NewInt(50)),
		LastDecayEpoch: 0,
	})

	// Burn 300 DREAM
	burnAmount := math.NewInt(300)
	err := k.BurnDREAM(ctx, addr, burnAmount)
	require.NoError(t, err)

	// Verify balance reduced
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(700).String(), member.DreamBalance.String())

	// Verify lifetime burned updated
	require.Equal(t, math.NewInt(350).String(), member.LifetimeBurned.String())

	// Verify event emitted
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	events := sdkCtx.EventManager().Events()

	var found bool
	for _, event := range events {
		if event.Type == "burn_dream" {
			found = true
			require.Equal(t, addr.String(), event.Attributes[0].Value)
			require.Equal(t, burnAmount.String(), event.Attributes[1].Value)
		}
	}
	require.True(t, found, "burn_dream event should be emitted")
}

// TestBurnDREAM_InsufficientBalance tests burning more than balance
func TestBurnDREAM_InsufficientBalance(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Try to burn more than balance
	err := k.BurnDREAM(ctx, addr, math.NewInt(200))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInsufficientBalance)

	// Verify balance unchanged
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100).String(), member.DreamBalance.String())
}

// TestBurnDREAM_InvalidAmount tests burning with invalid amounts
func TestBurnDREAM_InvalidAmount(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Test zero
	err := k.BurnDREAM(ctx, addr, math.ZeroInt())
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	// Test negative
	err = k.BurnDREAM(ctx, addr, math.NewInt(-50))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

// TestLockDREAM tests locking (staking) DREAM
func TestLockDREAM(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(200)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Lock 300 DREAM
	lockAmount := math.NewInt(300)
	err := k.LockDREAM(ctx, addr, lockAmount)
	require.NoError(t, err)

	// Verify staked increased
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500).String(), member.StakedDream.String())

	// Total balance unchanged
	require.Equal(t, math.NewInt(1000).String(), member.DreamBalance.String())

	// Verify event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	events := sdkCtx.EventManager().Events()

	var found bool
	for _, event := range events {
		if event.Type == "lock_dream" {
			found = true
			require.Equal(t, addr.String(), event.Attributes[0].Value)
			require.Equal(t, lockAmount.String(), event.Attributes[1].Value)
		}
	}
	require.True(t, found)
}

// TestLockDREAM_InsufficientUnlocked tests locking more than available
func TestLockDREAM_InsufficientUnlocked(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(700)), // 300 unlocked
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Try to lock 500 (only 300 available)
	err := k.LockDREAM(ctx, addr, math.NewInt(500))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
}

// TestUnlockDREAM tests unlocking (unstaking) DREAM
func TestUnlockDREAM(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(600)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Unlock 200 DREAM
	unlockAmount := math.NewInt(200)
	err := k.UnlockDREAM(ctx, addr, unlockAmount)
	require.NoError(t, err)

	// Verify staked decreased
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, math.NewInt(400).String(), member.StakedDream.String())

	// Total balance unchanged
	require.Equal(t, math.NewInt(1000).String(), member.DreamBalance.String())

	// Verify event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	events := sdkCtx.EventManager().Events()

	var found bool
	for _, event := range events {
		if event.Type == "unlock_dream" {
			found = true
		}
	}
	require.True(t, found)
}

// TestUnlockDREAM_InsufficientStaked tests unlocking more than staked caps to staked amount
func TestUnlockDREAM_InsufficientStaked(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(300)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Unlock more than staked — should cap to staked amount (300), not error
	err := k.UnlockDREAM(ctx, addr, math.NewInt(500))
	require.NoError(t, err)

	// Verify staked is now zero (all 300 unlocked)
	member, err := k.Member.Get(ctx, addr.String())
	require.NoError(t, err)
	require.True(t, member.StakedDream.IsZero())
}

// TestUnlockDREAM_ZeroStaked tests unlocking when nothing is staked
func TestUnlockDREAM_ZeroStaked(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	addr := sdk.AccAddress([]byte("test"))
	k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Unlock when staked is zero — should error
	err := k.UnlockDREAM(ctx, addr, math.NewInt(100))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInsufficientStake)
}

// TestTransferDREAM_Tip tests tip transfers
func TestTransferDREAM_Tip(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	sender := sdk.AccAddress([]byte("sender"))
	recipient := sdk.AccAddress([]byte("recipient"))

	// Create members
	k.Member.Set(ctx, sender.String(), types.Member{
		Address:            sender.String(),
		DreamBalance:       PtrInt(math.NewInt(1000)),
		StakedDream:        PtrInt(math.NewInt(0)),
		LifetimeEarned:     PtrInt(math.ZeroInt()),
		LifetimeBurned:     PtrInt(math.ZeroInt()),
		TipsGivenThisEpoch: 0,
		LastTipEpoch:       0,
	})

	k.Member.Set(ctx, recipient.String(), types.Member{
		Address:        recipient.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Transfer 50 DREAM as tip
	amount := math.NewInt(50)
	err := k.TransferDREAM(ctx, sender, recipient, amount, types.TransferPurpose_TRANSFER_PURPOSE_TIP)
	require.NoError(t, err)

	// Get params for tax calculation
	params, _ := k.Params.Get(ctx)
	tax := math.LegacyNewDecFromInt(amount).Mul(params.TransferTaxRate).TruncateInt()
	netAmount := amount.Sub(tax)

	// Verify sender balance reduced
	senderMember, _ := k.Member.Get(ctx, sender.String())
	require.Equal(t, math.NewInt(950).String(), senderMember.DreamBalance.String())

	// Verify recipient received net amount
	recipientMember, _ := k.Member.Get(ctx, recipient.String())
	expectedRecipient := math.NewInt(100).Add(netAmount)
	require.Equal(t, expectedRecipient.String(), recipientMember.DreamBalance.String())

	// Verify tip counter incremented
	require.Equal(t, uint32(1), senderMember.TipsGivenThisEpoch)
}

// TestTransferDREAM_ExceedsMaxTip tests tip limit enforcement
func TestTransferDREAM_ExceedsMaxTip(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	sender := sdk.AccAddress([]byte("sender"))
	recipient := sdk.AccAddress([]byte("recipient"))

	k.Member.Set(ctx, sender.String(), types.Member{
		Address:        sender.String(),
		DreamBalance:   PtrInt(math.NewInt(10000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	k.Member.Set(ctx, recipient.String(), types.Member{
		Address:        recipient.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Get max tip amount
	params, _ := k.Params.Get(ctx)

	// Try to tip more than max
	err := k.TransferDREAM(ctx, sender, recipient, params.MaxTipAmount.Add(math.NewInt(1)), types.TransferPurpose_TRANSFER_PURPOSE_TIP)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrExceedsMaxTipAmount)
}

// TestTransferDREAM_ExceedsTipsPerEpoch tests epoch tip limit
func TestTransferDREAM_ExceedsTipsPerEpoch(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	sender := sdk.AccAddress([]byte("sender"))
	recipient := sdk.AccAddress([]byte("recipient"))

	params, _ := k.Params.Get(ctx)
	currentEpoch, _ := k.GetCurrentEpoch(ctx)

	k.Member.Set(ctx, sender.String(), types.Member{
		Address:            sender.String(),
		DreamBalance:       PtrInt(math.NewInt(10000)),
		StakedDream:        PtrInt(math.NewInt(0)),
		LifetimeEarned:     PtrInt(math.ZeroInt()),
		LifetimeBurned:     PtrInt(math.ZeroInt()),
		TipsGivenThisEpoch: params.MaxTipsPerEpoch, // Already at max
		LastTipEpoch:       currentEpoch,
	})

	k.Member.Set(ctx, recipient.String(), types.Member{
		Address:        recipient.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Try one more tip
	err := k.TransferDREAM(ctx, sender, recipient, math.NewInt(10), types.TransferPurpose_TRANSFER_PURPOSE_TIP)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrExceedsMaxTipsPerEpoch)
}

// TestTransferDREAM_Gift tests gift transfers
func TestTransferDREAM_Gift(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	sender := sdk.AccAddress([]byte("sender"))
	recipient := sdk.AccAddress([]byte("recipient"))

	k.Member.Set(ctx, sender.String(), types.Member{
		Address:        sender.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	k.Member.Set(ctx, recipient.String(), types.Member{
		Address:        recipient.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		InvitedBy:      sender.String(), // Invited by sender
	})

	// Transfer as gift
	amount := math.NewInt(200)
	err := k.TransferDREAM(ctx, sender, recipient, amount, types.TransferPurpose_TRANSFER_PURPOSE_GIFT)
	require.NoError(t, err)

	// Verify transfer succeeded
	senderMember, _ := k.Member.Get(ctx, sender.String())
	require.Equal(t, math.NewInt(800).String(), senderMember.DreamBalance.String())
}

// TestTransferDREAM_CannotTransferToSelf tests self-transfer rejection
func TestTransferDREAM_CannotTransferToSelf(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	sender := sdk.AccAddress([]byte("sender"))

	k.Member.Set(ctx, sender.String(), types.Member{
		Address:        sender.String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Try to transfer to self
	err := k.TransferDREAM(ctx, sender, sender, math.NewInt(100), types.TransferPurpose_TRANSFER_PURPOSE_TIP)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCannotTransferToSelf)
}

// TestTransferDREAM_InsufficientBalance tests transfer with insufficient balance
func TestTransferDREAM_InsufficientBalance(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	sender := sdk.AccAddress([]byte("sender"))
	recipient := sdk.AccAddress([]byte("recipient"))

	k.Member.Set(ctx, sender.String(), types.Member{
		Address:        sender.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	k.Member.Set(ctx, recipient.String(), types.Member{
		Address:        recipient.String(),
		DreamBalance:   PtrInt(math.NewInt(100)),
		StakedDream:    PtrInt(math.NewInt(0)),
		LifetimeEarned: PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
	})

	// Try to transfer more than balance (use BOUNTY to avoid tip limit check)
	err := k.TransferDREAM(ctx, sender, recipient, math.NewInt(200), types.TransferPurpose_TRANSFER_PURPOSE_BOUNTY)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
}

// decayEpochContext returns a context at the first block past the 30-epoch
// new-member grace window, where one epoch of decay is due.
func decayEpochContext(t *testing.T, f *fixture) sdk.Context {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	return sdkCtx.WithBlockHeight(params.EpochBlocks * 31)
}

// TestDecayStakes_ShrinksRecordsPoolsAndAggregates is the core
// ledger-consistency regression: staked decay must shrink the stake record,
// every pool denominator, and the staker's member aggregates by the same
// amount, so the aggregate always equals the sum of the obligations backing
// it. The old design decayed only the aggregate, silently stranding the
// difference on whoever unlocked last.
func TestDecayStakes_ShrinksRecordsPoolsAndAggregates(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "decay_ledger_creator", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "decay_ledger_staker", math.NewInt(1_000_000_000))
	// Past the grace window with exactly one epoch of decay due, so the
	// expected figures below are one epoch's worth.
	stakerMember := mustMember(t, f, staker)
	stakerMember.LastDecayEpoch = 30
	require.NoError(t, k.Member.Set(f.ctx, staker.String(), stakerMember))
	initID := newActiveInitiative(t, f, creator, "dlgr")

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	require.NoError(t, k.MaybeApplyBulkDecay(decayEpochContext(t, f)))

	stake := mustStake(t, f, stakeID)
	// 1_000_000 * 0.99975 = 999_750
	require.Equal(t, "999750", stake.Amount.String(), "the stake record must decay")

	totalStaked, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "999750", totalStaked.String(), "the pool denominator must decay with the records")

	member := mustMember(t, f, staker)
	require.Equal(t, "999750", member.StakedDream.String(), "the member aggregate must decay with the records")

	// Unstaked 999_000_000 decays 1_998_000; staked decay burns another 250.
	require.Equal(t, "998001750", member.DreamBalance.String())
	require.Equal(t, "1998250", member.LifetimeBurned.String())
}

// TestDecayStakes_PreservesPendingProportionally pins the reward-debt
// scaling: decay must shrink the pending claim by the same factor as the
// principal, not clamp it to zero (which is what an unscaled debt would do,
// since pending = amount*acc - debt would go negative and truncate).
func TestDecayStakes_PreservesPendingProportionally(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "decay_pend_creator_", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "decay_pend_staker_", math.NewInt(1_000_000_000))
	initID := newActiveInitiative(t, f, creator, "dpend")

	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, k.DistributeEpochStakingRewardsFromPool(f.ctx))

	pendingBefore, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.Equal(t, "500", pendingBefore.String(), "precondition: 0.0005 yield on 1M staked")

	require.NoError(t, k.MaybeApplyBulkDecay(decayEpochContext(t, f)))

	pendingAfter, err := k.GetPendingStakingRewards(f.ctx, mustStake(t, f, stakeID))
	require.NoError(t, err)
	require.Equal(t, "499", pendingAfter.String(),
		"a decayed stake keeps a proportionally decayed claim: 999_750 * 0.0005 = 499.8 -> 499")
}

// TestDecayStakes_GraceMemberExempt pins that stakes held by a member still
// inside the new-member grace window do not decay.
func TestDecayStakes_GraceMemberExempt(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "decay_grac_creator_", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "decay_grac_staker_", math.NewInt(1_000_000_000))
	initID := newActiveInitiative(t, f, creator, "dgrac")

	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", amount)
	require.NoError(t, err)

	// The grace window is measured from the join HEIGHT (the height-domain
	// twin of the JoinedAt timestamp); setting it to the current epoch's
	// first block puts the member at age 0 — in grace.
	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	graceMember := mustMember(t, f, staker)
	graceMember.JoinedAtHeight = params.EpochBlocks * 31
	require.NoError(t, k.Member.Set(f.ctx, staker.String(), graceMember))

	require.NoError(t, k.MaybeApplyBulkDecay(decayEpochContext(t, f)))

	require.Equal(t, amount.String(), mustStake(t, f, stakeID).Amount.String(),
		"a staker inside the grace window must not decay")
	require.Equal(t, amount.String(), mustMember(t, f, staker).StakedDream.String())
}

// TestDecayStakes_DecaysContentConviction pins the scope on the other side:
// content conviction stakes decay like every other reward-bearing position,
// but never touch the seasonal pool denominator.
//
// They were exempt on the stated grounds that content conviction "already
// time-decays through the conviction half-life". It does not — both
// CalculateContentConviction and CalculateRawStakeConviction ramp time_factor
// linearly to 1.0 and hold it there, so neither is a half-life despite the
// parameter names. Content stakes are locked (exempt from unstaked decay),
// earn no DREAM, and propagate conviction into initiative conviction, which
// made them a costless shelter strictly better than holding DREAM: 0%/epoch
// against 0.2%, with a governance benefit attached. Decaying the principal is
// also what makes content conviction genuinely erode, since conviction is
// amount * time_factor and only the amount can carry it.
func TestDecayStakes_DecaysContentConviction(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	staker := newStakerMember(t, f, "decay_cont_staker_", math.NewInt(1_000_000_000))
	amount := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, 1, "", amount)
	require.NoError(t, err)

	require.NoError(t, k.MaybeApplyBulkDecay(decayEpochContext(t, f)))

	// 1_000_000 * 0.99975 = 999_750
	require.Equal(t, "999750", mustStake(t, f, stakeID).Amount.String(),
		"content conviction stakes decay like every other staked position")

	// The per-member content aggregate moves with it (adjustMemberContentStaked).
	require.Equal(t, "999750", mustMember(t, f, staker).ContentStakedDream.String(),
		"the content aggregate must decay with the records")

	totalStaked, err := k.GetSeasonalPoolTotalStaked(f.ctx)
	require.NoError(t, err)
	require.True(t, totalStaked.IsZero(), "content stakes never enter the seasonal denominator")
}

// TestApplyPendingDecay_GraceMeasuredByJoinHeightNotTimestamp is the
// regression for the perpetual-grace bug: the grace check used to divide the
// JoinedAt unix timestamp by EpochBlocks as though it were a block height, so
// every invited member computed a hugely negative age and was exempt from
// decay forever. Grace must be measured against JoinedAtHeight, with the
// timestamp irrelevant.
func TestApplyPendingDecay_GraceMeasuredByJoinHeightNotTimestamp(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	epochBlocks := params.EpochBlocks

	// An invited member: JoinedAt is a real unix timestamp, joined at block
	// height 10 * epoch_blocks (join epoch 10), LastDecayEpoch stamped at
	// join so only the grace gate decides.
	member := types.Member{
		Address:        sdk.AccAddress([]byte("invited")).String(),
		DreamBalance:   PtrInt(math.NewInt(1000)),
		StakedDream:    PtrInt(math.ZeroInt()),
		LifetimeBurned: PtrInt(math.ZeroInt()),
		JoinedAt:       1_750_000_000, // unix seconds — must be ignored by the grace gate
		JoinedAtHeight: epochBlocks * 10,
		LastDecayEpoch: 10,
	}

	// At epoch 31 the member is 21 epochs old — inside the 30-epoch window.
	sdkCtx := sdk.UnwrapSDKContext(ctx).WithBlockHeight(epochBlocks * 31)
	inGrace := member
	require.NoError(t, k.ApplyPendingDecay(sdkCtx, &inGrace))
	require.Equal(t, "1000", inGrace.DreamBalance.String(),
		"a member inside the grace window must not decay")
	require.Equal(t, int64(31), inGrace.LastDecayEpoch)

	// At epoch 41 the member is 31 epochs old — the window has closed and
	// the (previously perpetual) decay finally applies.
	sdkCtx = sdk.UnwrapSDKContext(ctx).WithBlockHeight(epochBlocks * 41)
	outGrace := member
	outGrace.LastDecayEpoch = 40
	require.NoError(t, k.ApplyPendingDecay(sdkCtx, &outGrace))
	require.Equal(t, "998", outGrace.DreamBalance.String(),
		"an invited member past the grace window must decay — the unix JoinedAt must not extend the window forever")
}

// TestDecayStakes_ExemptsProposedProjectStakes covers the asymmetry between
// decay and accrual on projects.
//
// stakeAccruing pays only on ACTIVE projects, so a stake placed while a project
// is still PROPOSED earns nothing — and used to be charged staked decay for the
// privilege. That is a pure levy on backing work at the earliest and least
// certain moment, which is exactly the conviction the system is trying to buy.
// The window is bounded (approval starts accrual, rejection ends the stake), so
// exempting it creates no lasting shelter. Terminal projects keep decaying:
// their principal is freely withdrawable and decay is the nudge to withdraw.
func TestDecayStakes_ExemptsProposedProjectStakes(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	creator := newStakerMember(t, f, "decay_prop_creator__", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "decay_prop_staker___", math.NewInt(1_000_000_000))

	proposed, err := k.CreateProject(f.ctx, creator, "Proposed", "D", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false)
	require.NoError(t, err)

	active, err := k.CreateProject(f.ctx, creator, "Active", "D", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(f.ctx, active, sdk.AccAddress([]byte("approver")),
		math.NewInt(100000), math.NewInt(1000)))

	amount := math.NewInt(1_000_000)
	proposedStake, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, proposed, "", amount)
	require.NoError(t, err)
	activeStake, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_PROJECT, active, "", amount)
	require.NoError(t, err)

	require.NoError(t, k.MaybeApplyBulkDecay(decayEpochContext(t, f)))

	require.Equal(t, amount.String(), mustStake(t, f, proposedStake).Amount.String(),
		"a stake on a PROPOSED project is frozen out of the pool and must not be charged decay")
	require.Equal(t, "999750", mustStake(t, f, activeStake).Amount.String(),
		"a stake on an ACTIVE project decays normally")
}

// sumLifetimeBurned totals LifetimeBurned across every member — the ledger-side
// record of DREAM destroyed from member balances.
func sumLifetimeBurned(t *testing.T, f *fixture) math.Int {
	t.Helper()
	total := math.ZeroInt()
	require.NoError(t, f.keeper.Member.Walk(f.ctx, nil, func(_ string, m types.Member) (bool, error) {
		if m.LifetimeBurned != nil {
			total = total.Add(*m.LifetimeBurned)
		}
		return false, nil
	}))
	return total
}

// TestTrackBurn_CoversEveryDreamDestructionPath is the regression for the
// SeasonBurned gap.
//
// TrackBurn had exactly one call site — the treasury-overflow burn — so
// SeasonBurned reported one minor line as the chain's entire destruction: no
// slashing, no failed challenges or invitations, no creation fees, no bonds, no
// decay, no transfer tax, no zeroing. This is the same shape as the TrackMint
// defect fixed in the seasonal-pool work, where SeasonMinted counted only the
// protocol's 10% treasury share. MintBurnRatio and DreamSupplyStats read this
// counter, so both were reporting on a rounding error.
//
// The invariant asserted is the one that actually matters and does not depend
// on which other members the fixture seeds: every micro-DREAM that leaves a
// member's balance as a burn must land in SeasonBurned. Each subtest drives one
// of the five member-balance destruction paths and requires the counter to move
// by exactly the total LifetimeBurned movement. (The sixth path, treasury
// overflow, burns a module ledger no member's LifetimeBurned records, and has
// always tracked its own burn.)
func TestTrackBurn_CoversEveryDreamDestructionPath(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, f *fixture)
	}{
		{
			// The choke point every module burn routes through — creation
			// fees, slashing, failed challenges and invitations, bonds, and
			// the x/forum, x/collect, x/reveal, x/name and x/season burns.
			name: "BurnDREAM",
			run: func(t *testing.T, f *fixture) {
				addr := newStakerMember(t, f, "burn_central_member_", math.NewInt(1_000_000))
				require.NoError(t, f.keeper.BurnDREAM(f.ctx, addr, math.NewInt(250_000)))
			},
		},
		{
			name: "unstaked decay",
			run: func(t *testing.T, f *fixture) {
				newStakerMember(t, f, "burn_unstaked_decay_", math.NewInt(1_000_000))
				require.NoError(t, f.keeper.MaybeApplyBulkDecay(decayEpochContext(t, f)))
			},
		},
		{
			name: "staked decay",
			run: func(t *testing.T, f *fixture) {
				k := f.keeper
				creator := newStakerMember(t, f, "burn_staked_creator_", math.NewInt(5_000_000_000))
				staker := newStakerMember(t, f, "burn_staked_decay___", math.NewInt(1_000_000))
				initID := newActiveInitiative(t, f, creator, "burnstaked")
				_, err := k.CreateStake(f.ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE,
					initID, "", math.NewInt(1_000_000))
				require.NoError(t, err)
				require.NoError(t, k.MaybeApplyBulkDecay(decayEpochContext(t, f)))
			},
		},
		{
			name: "transfer tax",
			run: func(t *testing.T, f *fixture) {
				k := f.keeper
				params, err := k.Params.Get(f.ctx)
				require.NoError(t, err)
				sender := newStakerMember(t, f, "burn_tax_sender_____", math.NewInt(5_000_000_000))
				recipient := newStakerMember(t, f, "burn_tax_recipient__", math.NewInt(5_000_000_000))
				require.True(t, params.TransferTaxRate.MulInt(params.MaxTipAmount).TruncateInt().IsPositive(),
					"precondition: the transfer must be taxed")
				require.NoError(t, k.TransferDREAM(f.ctx, sender, recipient, params.MaxTipAmount,
					types.TransferPurpose_TRANSFER_PURPOSE_TIP))
			},
		},
		{
			name: "zeroing",
			run: func(t *testing.T, f *fixture) {
				addr := newStakerMember(t, f, "burn_zeroed_member__", math.NewInt(750_000))
				require.NoError(t, f.keeper.ZeroMember(f.ctx, addr, "test"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)

			counterBefore, err := f.keeper.GetSeasonBurned(f.ctx)
			require.NoError(t, err)
			ledgerBefore := sumLifetimeBurned(t, f)

			tc.run(t, f)

			counterAfter, err := f.keeper.GetSeasonBurned(f.ctx)
			require.NoError(t, err)
			ledgerAfter := sumLifetimeBurned(t, f)

			burned := ledgerAfter.Sub(ledgerBefore)
			require.True(t, burned.IsPositive(),
				"precondition: this path must actually destroy DREAM, got %s", burned)
			require.Equal(t, burned.String(), counterAfter.Sub(counterBefore).String(),
				"SeasonBurned must move by exactly the DREAM destroyed (%s)", burned)
		})
	}
}

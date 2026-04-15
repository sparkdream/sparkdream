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

// TestApplyPendingDecay_WithStakedBalance tests that both staked and unstaked balances decay
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

	// Unstaked 400 decays at 0.2%: 400 * (1 - 0.002) = 400 * 0.998 = 399.2 → 399 (truncated)
	// Unstaked decay = 400 - 399 = 1
	// Staked 600 decays at 0.05%: 600 * (1 - 0.0005) = 600 * 0.9995 = 599.7 → 599 (truncated)
	// Staked decay = 600 - 599 = 1
	// Total balance: 1000 - 1 (unstaked decay) - 1 (staked decay) = 998
	expectedBalance := math.NewInt(998)
	require.Equal(t, expectedBalance.String(), member.DreamBalance.String())

	// Staked balance reduced by staked decay
	require.Equal(t, math.NewInt(599).String(), member.StakedDream.String())

	// Verify 2 DREAM burned total (1 unstaked + 1 staked)
	require.Equal(t, math.NewInt(2).String(), member.LifetimeBurned.String())
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

package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// DREAM is not meant to be monetized. These tests pin the limits that keep
// member-to-member transfers too slow to be worth buying through: tips capped
// by amount per sender, gifts capped per invitee for life, and — the binding
// constraint — a cap on what any one account can take in, however many senders
// cooperate.

func mkTransferMember(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, balance int64, invitedBy string) {
	t.Helper()
	require.NoError(t, k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		DreamBalance:   keeper.PtrInt(math.NewInt(balance)),
		StakedDream:    keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned: keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		InvitedBy:      invitedBy,
	}))
}

func setTransferParams(t *testing.T, k keeper.Keeper, ctx sdk.Context, mut func(*types.Params)) types.Params {
	t.Helper()
	p, err := k.Params.Get(ctx)
	require.NoError(t, err)
	mut(&p)
	require.NoError(t, p.Validate())
	require.NoError(t, k.Params.Set(ctx, p))
	return p
}

func TestTransferDREAM_RejectsEveryPurposeButTipAndGift(t *testing.T) {
	// The old switch had cases for TIP and GIFT and no default, so BOUNTY — and
	// any undefined number on a hand-built tx — was an uncapped send.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	sender := sdk.AccAddress([]byte("purpose-sender--"))
	recipient := sdk.AccAddress([]byte("purpose-recipnt-"))
	mkTransferMember(t, k, ctx, sender, 1_000_000_000, "")
	mkTransferMember(t, k, ctx, recipient, 0, "")

	for _, purpose := range []types.TransferPurpose{2, 7, -1} {
		err := k.TransferDREAM(ctx, sender, recipient, math.NewInt(900_000_000), purpose)
		require.ErrorIs(t, err, types.ErrInvalidTransferPurpose, "purpose %d", purpose)
	}
	m, err := k.Member.Get(ctx, sender.String())
	require.NoError(t, err)
	require.Equal(t, "1000000000", m.DreamBalance.String(), "nothing moved")
}

func TestTransferDREAM_TipAllowanceIsCountedByAmount(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	p := setTransferParams(t, k, ctx, func(p *types.Params) {
		p.MaxTipAmount = math.NewInt(100)
		p.MaxTipsSentPerEpoch = math.NewInt(150)
	})
	sender := sdk.AccAddress([]byte("tip-sender------"))
	a := sdk.AccAddress([]byte("tip-recipient-a-"))
	b := sdk.AccAddress([]byte("tip-recipient-b-"))
	mkTransferMember(t, k, ctx, sender, 10_000, "")
	mkTransferMember(t, k, ctx, a, 0, "")
	mkTransferMember(t, k, ctx, b, 0, "")

	require.NoError(t, k.TransferDREAM(ctx, sender, a, p.MaxTipAmount, types.TransferPurpose_TRANSFER_PURPOSE_TIP))
	// A second full tip would take the epoch total to 200 > 150, whichever
	// recipient it goes to.
	err := k.TransferDREAM(ctx, sender, b, p.MaxTipAmount, types.TransferPurpose_TRANSFER_PURPOSE_TIP)
	require.ErrorIs(t, err, types.ErrExceedsMaxTipsPerEpoch)
	require.NoError(t, k.TransferDREAM(ctx, sender, b, math.NewInt(50), types.TransferPurpose_TRANSFER_PURPOSE_TIP))

	// A new epoch restores the allowance.
	next := ctx.WithBlockHeight(ctx.BlockHeight() + p.EpochBlocks)
	require.NoError(t, k.TransferDREAM(next, sender, b, p.MaxTipAmount, types.TransferPurpose_TRANSFER_PURPOSE_TIP))
}

func TestTransferDREAM_RecipientLimitBindsAcrossManySenders(t *testing.T) {
	// The reason the limit sits on the recipient: a buyer can collect from any
	// number of sellers, each well inside their own sender allowance.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	p := setTransferParams(t, k, ctx, func(p *types.Params) {
		p.TransferTaxRate = math.LegacyZeroDec()
		p.MaxTipAmount = math.NewInt(100)
		p.MaxTipsSentPerEpoch = math.NewInt(100)
		p.MaxTransferReceivedPerEpoch = math.NewInt(250)
		p.MaxTransferReceivedPerSeason = math.NewInt(400)
	})
	buyer := sdk.AccAddress([]byte("limit-buyer-----"))
	mkTransferMember(t, k, ctx, buyer, 0, "")
	sellers := make([]sdk.AccAddress, 6)
	for i := range sellers {
		sellers[i] = sdk.AccAddress([]byte{'s', 'e', 'l', 'l', 'e', 'r', '-', '-', '-', '-', '-', '-', '-', '-', '-', byte('0' + i)})
		mkTransferMember(t, k, ctx, sellers[i], 1_000, "")
	}

	require.NoError(t, k.TransferDREAM(ctx, sellers[0], buyer, math.NewInt(100), types.TransferPurpose_TRANSFER_PURPOSE_TIP))
	require.NoError(t, k.TransferDREAM(ctx, sellers[1], buyer, math.NewInt(100), types.TransferPurpose_TRANSFER_PURPOSE_TIP))
	err := k.TransferDREAM(ctx, sellers[2], buyer, math.NewInt(100), types.TransferPurpose_TRANSFER_PURPOSE_TIP)
	require.ErrorIs(t, err, types.ErrExceedsTransferReceiveLimit, "a third seller cannot push the buyer past the epoch limit")
	seller2, err := k.Member.Get(ctx, sellers[2].String())
	require.NoError(t, err)
	require.Equal(t, "1000", seller2.DreamBalance.String(), "a refused tip moves nothing")
	require.Nil(t, seller2.TipsSentThisEpoch, "and charges nothing against the sender")
	require.NoError(t, k.TransferDREAM(ctx, sellers[2], buyer, math.NewInt(50), types.TransferPurpose_TRANSFER_PURPOSE_TIP))

	// The epoch window resets; the season window does not.
	next := ctx.WithBlockHeight(ctx.BlockHeight() + p.EpochBlocks)
	require.NoError(t, k.TransferDREAM(next, sellers[3], buyer, math.NewInt(100), types.TransferPurpose_TRANSFER_PURPOSE_TIP))
	err = k.TransferDREAM(next, sellers[4], buyer, math.NewInt(100), types.TransferPurpose_TRANSFER_PURPOSE_TIP)
	require.ErrorIs(t, err, types.ErrExceedsTransferReceiveLimit, "350 received this season; 100 more would exceed 400")
	require.NoError(t, k.TransferDREAM(next, sellers[4], buyer, math.NewInt(50), types.TransferPurpose_TRANSFER_PURPOSE_TIP))

	m, err := k.Member.Get(next, buyer.String())
	require.NoError(t, err)
	require.Equal(t, "400", m.DreamBalance.String())
	require.Equal(t, "400", m.TransferReceivedThisSeason.String())
}

func TestTransferDREAM_GiftIsCappedPerInviteeForLife(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	p := setTransferParams(t, k, ctx, func(p *types.Params) {
		p.MaxGiftPerInvitee = math.NewInt(500)
		p.MaxTransferReceivedPerEpoch = math.NewInt(10)
		p.MaxTransferReceivedPerSeason = math.NewInt(10)
		p.MaxTipsSentPerEpoch = math.NewInt(10)
		p.MaxTipAmount = math.NewInt(10)
	})
	inviter := sdk.AccAddress([]byte("gift-inviter----"))
	invitee := sdk.AccAddress([]byte("gift-invitee----"))
	stranger := sdk.AccAddress([]byte("gift-stranger---"))
	mkTransferMember(t, k, ctx, inviter, 10_000, "")
	mkTransferMember(t, k, ctx, invitee, 0, inviter.String())
	mkTransferMember(t, k, ctx, stranger, 0, "")

	err := k.TransferDREAM(ctx, inviter, stranger, math.NewInt(100), types.TransferPurpose_TRANSFER_PURPOSE_GIFT)
	require.ErrorIs(t, err, types.ErrGiftOnlyToInvitees)

	// Gifts are not held to the recipient limits (10 here): the lifetime
	// per-invitee cap bounds them, and each member has one inviter.
	require.NoError(t, k.TransferDREAM(ctx, inviter, invitee, math.NewInt(300), types.TransferPurpose_TRANSFER_PURPOSE_GIFT))
	later := ctx.WithBlockHeight(ctx.BlockHeight() + 10*p.EpochBlocks)
	err = k.TransferDREAM(later, inviter, invitee, math.NewInt(201), types.TransferPurpose_TRANSFER_PURPOSE_GIFT)
	require.ErrorIs(t, err, types.ErrExceedsGiftAllowance, "the allowance is for life, not per epoch")
	require.NoError(t, k.TransferDREAM(later, inviter, invitee, math.NewInt(200), types.TransferPurpose_TRANSFER_PURPOSE_GIFT))

	rec, err := k.GiftRecord.Get(later, collections.Join(inviter.String(), invitee.String()))
	require.NoError(t, err)
	require.Equal(t, "500", rec.TotalGifted.String())
}

func TestTransferDREAM_RefusedGiftWritesNoLedger(t *testing.T) {
	// Limit checks run before any write, so a gift refused for balance does not
	// consume the invitee's lifetime allowance.
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	inviter := sdk.AccAddress([]byte("gift-poor-invitr"))
	invitee := sdk.AccAddress([]byte("gift-poor-invite"))
	mkTransferMember(t, k, ctx, inviter, 100, "")
	mkTransferMember(t, k, ctx, invitee, 0, inviter.String())

	err := k.TransferDREAM(ctx, inviter, invitee, math.NewInt(200), types.TransferPurpose_TRANSFER_PURPOSE_GIFT)
	require.ErrorIs(t, err, types.ErrInsufficientBalance)
	_, gErr := k.GiftRecord.Get(ctx, collections.Join(inviter.String(), invitee.String()))
	require.Error(t, gErr)
}

func TestTransferLimitParamsValidate(t *testing.T) {
	for name, mut := range map[string]func(*types.Params){
		"zero tip":                  func(p *types.Params) { p.MaxTipAmount = math.ZeroInt() },
		"tip above epoch allowance": func(p *types.Params) { p.MaxTipAmount = p.MaxTipsSentPerEpoch.AddRaw(1) },
		"zero gift allowance":       func(p *types.Params) { p.MaxGiftPerInvitee = math.ZeroInt() },
		"nil receive epoch":         func(p *types.Params) { p.MaxTransferReceivedPerEpoch = math.Int{} },
		"epoch above season":        func(p *types.Params) { p.MaxTransferReceivedPerEpoch = p.MaxTransferReceivedPerSeason.AddRaw(1) },
		"negative bounty ratio":     func(p *types.Params) { p.InitiativeBountyMaxBudgetRatio = math.LegacyNewDec(-1) },
		"zero bounty contributions": func(p *types.Params) { p.MaxInitiativeBountyContributions = 0 },
		"zero bounty minimum":       func(p *types.Params) { p.MinInitiativeBountyContribution = math.ZeroInt() },
	} {
		t.Run(name, func(t *testing.T) {
			p := types.DefaultParams()
			mut(&p)
			require.Error(t, p.Validate())
		})
	}
	p := types.DefaultParams()
	p.InitiativeBountyMaxBudgetRatio = math.LegacyZeroDec()
	require.NoError(t, p.Validate(), "a zero ratio disables bounties and is valid")
	require.NoError(t, types.DefaultRepOperationalParams().Validate())
	require.Equal(t, types.DefaultParams().ExtractOperationalParams(), types.DefaultRepOperationalParams(),
		"operational defaults must mirror the full defaults")
}

package keeper_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

// Wave 1 (attendance issuance, mutual attestations, expiring and redeemable
// tokens, locked royalty splits) and wave 2 (price curves, blind and
// generative mints, schedules, renderers, multi-asset) of
// docs/x-artifact-spec.md §16.2.

func (f *fixture) issue(minter sdk.AccAddress, classID uint64, recipients ...sdk.AccAddress) (*types.MsgIssueResponse, error) {
	rs := make([]string, len(recipients))
	for i, r := range recipients {
		rs[i] = r.String()
	}
	return f.msg.Issue(f.ctx, &types.MsgIssue{
		Minter: minter.String(), ClassId: classID, Recipients: rs,
		Metadata: types.TokenMetadata{Name: "aurora summit 2026"}, AcceptedContentLicense: types.ContentLicense,
	})
}

func (f *fixture) tokenQuery(classID, tokenID uint64) types.TokenView {
	f.t.Helper()
	resp, err := f.query.Token(f.ctx, &types.QueryTokenRequest{ClassId: classID, TokenId: tokenID})
	require.NoError(f.t, err)
	return resp.Token
}

// --- Wave 1 -----------------------------------------------------------------

func TestIssueBatch(t *testing.T) {
	f := initFixture(t)
	badge := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
	})
	// dave opts into the inbox; a member sender would otherwise deliver directly.
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	before := f.bank.bal(f.alice)
	resp, err := f.issue(f.alice, badge, f.bob, f.carol, f.dave)
	require.NoError(t, err)
	require.Len(t, resp.Results, 3)
	require.True(t, resp.Results[0].Live, "member delivered directly")
	require.True(t, resp.Results[1].Live)
	require.False(t, resp.Results[2].Live, "non-member waits in the inbox")
	require.Equal(t, "aurora summit 2026", f.token(badge, 1).Metadata.Name)
	require.Equal(t, f.carol.String(), f.token(badge, 2).Owner)
	p := f.params()
	require.Equal(t, before.Sub(p.TokenDeposit.MulRaw(3)).Sub(p.InboxFee), f.bank.bal(f.alice))
	c := f.class(badge)
	require.Equal(t, uint64(2), c.Supply)
	require.Equal(t, uint64(1), c.ReservedSupply)

	_, err = f.issue(f.alice, badge, f.bob, f.bob)
	require.ErrorIs(t, err, types.ErrInvalidBatch, "duplicate recipient")
	_, err = f.issue(f.alice, badge)
	require.ErrorIs(t, err, types.ErrInvalidBatch, "empty")
	_, err = f.issue(f.dave, badge, f.bob)
	require.ErrorIs(t, err, types.ErrNotMinter)
	f.setParams(func(p *types.Params) { p.MaxIssueBatchSize = 2 })
	_, err = f.issue(f.alice, badge, f.bob, f.carol, f.dave)
	require.ErrorIs(t, err, types.ErrInvalidBatch, "over max_issue_batch_size")
	f.checkInvariants()
}

func TestMutualAttestation(t *testing.T) {
	f := initFixture(t)
	_, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{
		Creator: f.alice.String(), Name: "Vouch", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Mutual: true},
	})
	require.ErrorIs(t, err, types.ErrInvalidFlags, "mutual needs HOLDER_OR_ISSUER")
	_, err = f.msg.CreateClass(f.ctx, &types.MsgCreateClass{
		Creator: f.alice.String(), Name: "Vouch", AcceptedContentLicense: types.ContentLicense,
		Flags:      types.ClassFlags{Mutual: true, BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER},
		MintPolicy: types.MintPolicy{PublicMintEnabled: true},
	})
	require.ErrorIs(t, err, types.ErrInvalidFlags, "no public mint on mutual classes")

	vouch := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{Mutual: true, BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
		m.Minters = []string{f.carol.String()}
	})
	// Even a trusted member recipient must sign: the mint waits in the inbox.
	res := f.mint(f.alice, vouch, f.bob, 1)
	require.False(t, res[0].Live)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: vouch, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.alice.String(), Metadata: meta("self")}}})
	require.ErrorIs(t, err, types.ErrInvalidRecipient, "no self-attestation")
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(), Entries: []types.AcceptEntry{{ClassId: vouch, TokenId: 1}}})
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), f.token(vouch, 1).Owner)

	// carol (a listed minter) attests to bob; either party may burn.
	res = f.mint(f.carol, vouch, f.bob, 1)
	require.False(t, res[0].Live)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(), Entries: []types.AcceptEntry{{ClassId: vouch, TokenId: 2}}})
	require.NoError(t, err)
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.dave.String(), ClassId: vouch, TokenIds: []uint64{2}, Reason: "x"})
	require.ErrorIs(t, err, types.ErrNotAuthorized, "a stranger cannot revoke")
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.carol.String(), ClassId: vouch, TokenIds: []uint64{1}, Reason: "x"})
	require.ErrorIs(t, err, types.ErrNotAuthorized, "carol did not sign token 1")
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.carol.String(), ClassId: vouch, TokenIds: []uint64{2}, Reason: "withdrawn"})
	require.NoError(t, err, "the attesting minter revokes its own attestation")
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: vouch, TokenId: 1}}})
	require.NoError(t, err, "the recipient burns")
	require.Equal(t, uint64(0), f.class(vouch).Supply)
	f.checkInvariants()
}

func TestExpiringTokensMarkAndRenew(t *testing.T) {
	f := initFixture(t)
	pass := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_MARK, Term: 100, RenewalPrice: spark(1)}
		m.PayoutAddress = f.carol.String()
	})
	_, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Bad", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Transferable: true}, Expiry: types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_MARK, Term: 1}})
	require.ErrorIs(t, err, types.ErrInvalidExpiry, "term below the floor")
	_, err = f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Bad", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Transferable: true}, Expiry: types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_MARK, Term: 100, RenewalPrice: sdkCoin(dream, 1)}})
	require.ErrorIs(t, err, types.ErrDreamNotAccepted)

	f.mint(f.alice, pass, f.bob, 1)
	tok := f.token(pass, 1)
	start := f.ctx.BlockTime().Unix()
	require.Equal(t, start+100, tok.ExpiresAt)
	require.False(t, f.tokenQuery(pass, 1).Expired)

	f.advance(100, 1)
	require.True(t, f.tokenQuery(pass, 1).Expired)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: pass, TokenId: 1, Price: spark(1), Duration: 60})
	require.ErrorIs(t, err, types.ErrTokenExpired)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(), Entries: []types.TransferEntry{{ClassId: pass, TokenId: 1, Recipient: f.alice.String()}}})
	require.NoError(t, err, "an expired MARK token can still be given away")
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.alice.String(), Entries: []types.TransferEntry{{ClassId: pass, TokenId: 1, Recipient: f.bob.String()}}})
	require.NoError(t, err)

	// Holder renewal pays the class; price protection applies.
	_, err = f.msg.Renew(f.ctx, &types.MsgRenew{Signer: f.bob.String(), ClassId: pass, TokenId: 1, MaxPrice: sdkCoin(bond, 1)})
	require.ErrorIs(t, err, types.ErrPriceExceeded)
	bobB, carolB := f.bank.bal(f.bob), f.bank.bal(f.carol)
	resp, err := f.msg.Renew(f.ctx, &types.MsgRenew{Signer: f.bob.String(), ClassId: pass, TokenId: 1, MaxPrice: spark(1)})
	require.NoError(t, err)
	now := f.ctx.BlockTime().Unix()
	require.Equal(t, now+100, resp.ExpiresAt, "renewal from now when already expired")
	fee := spark(1).Amount.MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	require.Equal(t, bobB.Sub(spark(1).Amount), f.bank.bal(f.bob))
	require.Equal(t, carolB.Add(spark(1).Amount.Sub(fee)), f.bank.bal(f.carol))
	require.Equal(t, fee, f.distr.pool.AmountOf(bond))
	require.False(t, f.tokenQuery(pass, 1).Expired)

	// Issuer renewal is free and stacks on the current expiry.
	aliceB := f.bank.bal(f.alice)
	resp, err = f.msg.Renew(f.ctx, &types.MsgRenew{Signer: f.alice.String(), ClassId: pass, TokenId: 1})
	require.NoError(t, err)
	require.Equal(t, now+200, resp.ExpiresAt)
	require.Equal(t, aliceB, f.bank.bal(f.alice))
	_, err = f.msg.Renew(f.ctx, &types.MsgRenew{Signer: f.dave.String(), ClassId: pass, TokenId: 1, MaxPrice: spark(1)})
	require.ErrorIs(t, err, types.ErrNotAuthorized)

	plain := f.createClass(f.alice)
	f.mint(f.alice, plain, f.bob, 1)
	_, err = f.msg.Renew(f.ctx, &types.MsgRenew{Signer: f.bob.String(), ClassId: plain, TokenId: 1, MaxPrice: spark(1)})
	require.ErrorIs(t, err, types.ErrNotExpiring)
	f.checkInvariants()
}

func TestExpiringTokensBurnInEndBlocker(t *testing.T) {
	f := initFixture(t)
	pass := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: 100}
	})
	f.mint(f.alice, pass, f.bob, 2)
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: pass, TokenId: 1, Price: spark(1), Duration: 600})
	require.NoError(t, err)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(), Entries: []types.TransferEntry{{ClassId: pass, TokenId: 2, Recipient: f.dave.String()}}})
	require.NoError(t, err)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER, f.token(pass, 2).Lock)
	// Renewing re-indexes under the new expiry.
	_, err = f.msg.Renew(f.ctx, &types.MsgRenew{Signer: f.alice.String(), ClassId: pass, TokenId: 1})
	require.NoError(t, err)
	f.checkInvariants()

	f.advance(99, 1)
	f.endBlock()
	require.Equal(t, uint64(2), f.class(pass).Supply, "nothing due yet")

	bobB := f.bank.bal(f.bob)
	f.advance(1, 1)
	f.endBlock()
	c := f.class(pass)
	require.Equal(t, uint64(1), c.Supply, "token 2 expired; token 1 was renewed")
	require.Equal(t, uint64(1), c.Burned)
	_, found, _ := f.keeper.GetToken(f.ctx, pass, 2)
	require.False(t, found)
	require.Equal(t, bobB.Add(f.params().TokenDeposit), f.bank.bal(f.bob), "deposit refunded to the holder")
	_, err = f.keeper.PendingTransfers.Get(f.ctx, keeperPair(pass, 2))
	require.Error(t, err, "pending transfer cancelled")

	f.advance(100, 1)
	f.endBlock()
	require.Equal(t, uint64(0), f.class(pass).Supply)
	_, err = f.keeper.Listings.Get(f.ctx, keeperPair(pass, 1))
	require.Error(t, err, "listing removed with the token")
	f.checkInvariants()
}

func TestRedeemableTokens(t *testing.T) {
	f := initFixture(t)
	ticket := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{Transferable: true, Redeemable: true}
		m.Minters = []string{f.carol.String()}
	})
	f.mint(f.alice, ticket, f.bob, 2)
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: ticket, TokenId: 1, Price: spark(1), Duration: 600})
	require.NoError(t, err)

	_, err = f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.bob.String(), ClassId: ticket, TokenIds: []uint64{1}})
	require.ErrorIs(t, err, types.ErrNotMinter, "the holder does not redeem")
	_, err = f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.carol.String(), ClassId: ticket, TokenIds: []uint64{1}})
	require.NoError(t, err, "the venue (a listed minter) scans the ticket")
	tok := f.token(ticket, 1)
	require.Equal(t, f.ctx.BlockTime().Unix(), tok.RedeemedAt)
	require.Equal(t, f.bob.String(), tok.Owner, "redemption never moves ownership")
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, tok.Lock, "the listing was cancelled")
	_, err = f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.alice.String(), ClassId: ticket, TokenIds: []uint64{1}})
	require.ErrorIs(t, err, types.ErrAlreadyRedeemed)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(), Entries: []types.TransferEntry{{ClassId: ticket, TokenId: 1, Recipient: f.alice.String()}}})
	require.ErrorIs(t, err, types.ErrRedeemedNotTransferable)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: ticket, TokenId: 1, Price: spark(1), Duration: 600})
	require.ErrorIs(t, err, types.ErrRedeemedNotTransferable)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: ticket, TokenId: 1}}})
	require.NoError(t, err, "a spent ticket can still be burned for its deposit")

	plain := f.createClass(f.alice)
	f.mint(f.alice, plain, f.bob, 1)
	_, err = f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.alice.String(), ClassId: plain, TokenIds: []uint64{1}})
	require.ErrorIs(t, err, types.ErrNotRedeemable)

	souvenir := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{Transferable: true, Redeemable: true, RedeemedTransferable: true}
	})
	f.mint(f.alice, souvenir, f.bob, 1)
	_, err = f.msg.Redeem(f.ctx, &types.MsgRedeem{Signer: f.alice.String(), ClassId: souvenir, TokenIds: []uint64{1}})
	require.NoError(t, err)
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(), Entries: []types.TransferEntry{{ClassId: souvenir, TokenId: 1, Recipient: f.alice.String()}}})
	require.NoError(t, err, "souvenirs keep trading")
	f.checkInvariants()
}

func TestLockRoyalty(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.RoyaltyRecipients = []types.RoyaltyShare{{Address: f.alice.String(), WeightBps: 7000}, {Address: f.carol.String(), WeightBps: 3000}}
	})
	_, err := f.msg.LockRoyalty(f.ctx, &types.MsgLockRoyalty{Owner: f.bob.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrNotClassOwner)
	_, err = f.msg.LockRoyalty(f.ctx, &types.MsgLockRoyalty{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	require.True(t, f.class(id).RoyaltyLocked)
	_, err = f.msg.LockRoyalty(f.ctx, &types.MsgLockRoyalty{Owner: f.alice.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrRoyaltyLocked)

	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, UpdateMask: []string{"royalty_recipients"},
		RoyaltyRecipients: types.SoleRoyaltyRecipient(f.alice.String())})
	require.ErrorIs(t, err, types.ErrRoyaltyLocked)
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, UpdateMask: []string{"royalty_bps"}, RoyaltyBps: 100})
	require.ErrorIs(t, err, types.ErrRoyaltyLocked)
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, UpdateMask: []string{"description"}, Description: "still editable"})
	require.NoError(t, err)
	// Handover does not unlock.
	_, err = f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(t, err)
	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.bob.String(), ClassId: id})
	require.NoError(t, err)
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.bob.String(), ClassId: id, UpdateMask: []string{"royalty_bps"}, RoyaltyBps: 0})
	require.ErrorIs(t, err, types.ErrRoyaltyLocked)
	f.checkInvariants()
}

// --- Wave 2: pricing ----------------------------------------------------------

func TestLinearCurvePublicMint(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1), Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, PriceStep: spark(1).Amount.QuoRaw(2)}
		m.PayoutAddress = f.carol.String()
	})
	q, err := f.query.PublicMintQuote(f.ctx, &types.QueryPublicMintQuoteRequest{ClassId: id, Buyer: f.bob.String(), Quantity: 2})
	require.NoError(t, err)
	require.Equal(t, int64(2_500_000), q.TotalPrice.Amount.Int64())
	require.Equal(t, int64(1_000_000), q.UnitPrice.Amount.Int64())
	require.Equal(t, int64(1_500_000), q.MaxUnitPrice.Amount.Int64())
	require.True(t, q.Available)

	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 2, MaxPrice: spark(1)})
	require.ErrorIs(t, err, types.ErrPriceExceeded, "max_price must cover the last token")
	carolB := f.bank.bal(f.carol)
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 2, MaxPrice: sdkCoin(bond, 1_500_000)})
	require.NoError(t, err)
	fee := math.NewInt(2_500_000).MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	require.Equal(t, carolB.Add(math.NewInt(2_500_000).Sub(fee)), f.bank.bal(f.carol))
	require.Equal(t, uint64(2), f.class(id).PublicMinted)
	q, _ = f.query.PublicMintQuote(f.ctx, &types.QueryPublicMintQuoteRequest{ClassId: id, Buyer: f.bob.String(), Quantity: 1})
	require.Equal(t, int64(2_000_000), q.UnitPrice.Amount.Int64(), "third token")

	// The curve is fixed once minting opened; other knobs still move.
	mp := f.class(id).MintPolicy
	mp.PriceStep = spark(1).Amount
	_, err = f.msg.SetMintPolicy(f.ctx, &types.MsgSetMintPolicy{Owner: f.alice.String(), ClassId: id, MintPolicy: mp})
	require.ErrorIs(t, err, types.ErrMintPolicyLocked)
	mp = f.class(id).MintPolicy
	mp.PerAddressLimit, mp.PublicMintEnabled = 3, false
	_, err = f.msg.SetMintPolicy(f.ctx, &types.MsgSetMintPolicy{Owner: f.alice.String(), ClassId: id, MintPolicy: mp})
	require.NoError(t, err)

	_, err = f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Bad", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Transferable: true}, MintPolicy: types.MintPolicy{Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, Price: spark(1)}})
	require.ErrorIs(t, err, types.ErrInvalidMintPricing, "curve without a step")
	_, err = f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Bad", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{}, MintPolicy: types.MintPolicy{Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, PriceStep: math.NewInt(1)}})
	require.ErrorIs(t, err, types.ErrInvalidFlags, "soulbound classes run no curve")
	f.checkInvariants()
}

func TestDutchPublicMint(t *testing.T) {
	f := initFixture(t)
	start := f.ctx.BlockTime().Unix()
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(10), Pricing: types.MintPricing_MINT_PRICING_DUTCH,
			FloorPrice: spark(2).Amount, StartTime: start, EndTime: start + 1000}
	})
	q, _ := f.query.PublicMintQuote(f.ctx, &types.QueryPublicMintQuoteRequest{ClassId: id, Buyer: f.bob.String(), Quantity: 1})
	require.Equal(t, spark(10).Amount, q.UnitPrice.Amount)
	f.advance(500, 1)
	q, _ = f.query.PublicMintQuote(f.ctx, &types.QueryPublicMintQuoteRequest{ClassId: id, Buyer: f.bob.String(), Quantity: 1})
	require.Equal(t, spark(6).Amount, q.UnitPrice.Amount, "halfway down")
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 1, MaxPrice: spark(5)})
	require.ErrorIs(t, err, types.ErrPriceExceeded)
	bobB := f.bank.bal(f.bob)
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 1, MaxPrice: spark(6)})
	require.NoError(t, err)
	require.Equal(t, bobB.Sub(spark(6).Amount).Sub(f.params().TokenDeposit), f.bank.bal(f.bob))
	mp := f.class(id).MintPolicy
	mp.FloorPrice = spark(1).Amount
	_, err = f.msg.SetMintPolicy(f.ctx, &types.MsgSetMintPolicy{Owner: f.alice.String(), ClassId: id, MintPolicy: mp})
	require.ErrorIs(t, err, types.ErrMintPolicyLocked)
	f.advance(600, 1)
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 1, MaxPrice: spark(2)})
	require.ErrorIs(t, err, types.ErrMintWindow, "the auction ended")
	f.checkInvariants()
}

// --- Wave 2: commit-reveal ----------------------------------------------------

// blindTree commits n tokens of the class the fixture will create next.
func (f *fixture) blindTree(classID uint64, n int) (root []byte, salts [][]byte, metas []types.TokenMetadata, proofs [][][]byte) {
	leaves := make([][]byte, n)
	for i := range leaves {
		salts = append(salts, []byte(fmt.Sprintf("salt-%d", i)))
		metas = append(metas, types.TokenMetadata{Name: fmt.Sprintf("blind #%d", i+1), Attributes: []types.Attribute{{Key: "rarity", Value: fmt.Sprint(i)}}})
		leaves[i] = types.RevealLeaf(classID, uint64(i+1), salts[i], metas[i])
	}
	root, proofs = types.BuildRevealTree(leaves)
	return
}

func TestBlindMintReveal(t *testing.T) {
	f := initFixture(t)
	root, salts, metas, proofs := f.blindTree(1, 3)
	deadline := f.ctx.BlockTime().Unix() + types.MinRevealWindow + 100
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(2)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_BLIND_METADATA, Commitment: hex.EncodeToString(root), Deadline: deadline}
		m.MaxSupply = 3
		m.PayoutAddress = f.carol.String()
	})
	require.Equal(t, uint64(1), id)
	_, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Bad", AcceptedContentLicense: types.ContentLicense,
		Flags: types.ClassFlags{Transferable: true}, Reveal: types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_BLIND_METADATA, Commitment: hex.EncodeToString(root), Deadline: deadline, Seed: "aa"}})
	require.ErrorIs(t, err, types.ErrInvalidReveal, "chain-set fields refused")

	carolB, aliceB := f.bank.bal(f.carol), f.bank.bal(f.alice)
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 2, MaxPrice: spark(2)})
	require.NoError(t, err)
	tok := f.token(id, 1)
	require.True(t, tok.Unrevealed)
	require.Equal(t, spark(2).Amount, tok.Paid, "price escrowed until reveal")
	require.False(t, tok.MetadataFrozen)
	require.Equal(t, carolB, f.bank.bal(f.carol), "payout waits for the reveal")
	require.Equal(t, f.params().TokenDeposit.MulRaw(2).Add(spark(4).Amount), f.moduleBalance())
	require.Equal(t, types.DisplayUnrevealed, f.tokenQuery(id, 1).DisplaySource)
	f.checkInvariants()

	// Minters cannot pre-fill metadata on a blind class, nor edit before reveal.
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.bob.String(), Metadata: meta("x")}}})
	require.ErrorIs(t, err, types.ErrInvalidReveal)
	_, err = f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: meta("x")})
	require.ErrorIs(t, err, types.ErrNotRevealed)
	_, err = f.msg.ProposeMedia(f.ctx, &types.MsgProposeMedia{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: meta("x")})
	require.ErrorIs(t, err, types.ErrNotRevealed)

	reveal := func(signer sdk.AccAddress, tokenID uint64, m types.TokenMetadata, salt []byte, proof [][]byte) error {
		_, err := f.msg.Reveal(f.ctx, &types.MsgReveal{Signer: signer.String(), ClassId: id, TokenId: tokenID, Metadata: m,
			Salt: hex.EncodeToString(salt), Proof: types.HexHashes(proof)})
		return err
	}
	require.ErrorIs(t, reveal(f.bob, 1, metas[0], salts[0], proofs[0]), types.ErrNotMinter)
	require.ErrorIs(t, reveal(f.alice, 1, metas[1], salts[0], proofs[0]), types.ErrInvalidReveal, "wrong metadata")
	require.ErrorIs(t, reveal(f.alice, 1, metas[0], salts[1], proofs[0]), types.ErrInvalidReveal, "wrong salt")
	require.ErrorIs(t, reveal(f.alice, 1, metas[0], salts[0], proofs[1]), types.ErrInvalidReveal, "wrong proof")
	require.ErrorIs(t, reveal(f.alice, 2, metas[0], salts[0], proofs[0]), types.ErrInvalidReveal, "proof for another token")
	require.NoError(t, reveal(f.alice, 1, metas[0], salts[0], proofs[0]))
	tok = f.token(id, 1)
	require.False(t, tok.Unrevealed)
	require.Equal(t, metas[0], tok.Metadata)
	require.True(t, tok.Paid.IsZero())
	fee := spark(2).Amount.MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	require.Equal(t, carolB.Add(spark(2).Amount.Sub(fee)), f.bank.bal(f.carol), "payout released")
	require.Equal(t, fee, f.distr.pool.AmountOf(bond))
	require.Equal(t, types.DisplayStored, f.tokenQuery(id, 1).DisplaySource)
	require.ErrorIs(t, reveal(f.alice, 1, metas[0], salts[0], proofs[0]), types.ErrAlreadyRevealed)
	require.Equal(t, aliceB, f.bank.bal(f.alice))

	// Burning an unrevealed token before the deadline forfeits the price to the class.
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.dave.String(), ClassId: id, Quantity: 1, MaxPrice: spark(2)})
	require.NoError(t, err)
	daveB := f.bank.bal(f.dave)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.dave.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 3}}})
	require.NoError(t, err)
	require.Equal(t, daveB.Add(f.params().TokenDeposit), f.bank.bal(f.dave), "deposit only")
	require.Equal(t, carolB.Add(spark(4).Amount.Sub(fee).Sub(fee)), f.bank.bal(f.carol))

	// Past the deadline an unrevealed token refunds everything the holder paid.
	f.advance(types.MinRevealWindow+101, 1)
	bobB := f.bank.bal(f.bob)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 2}}})
	require.NoError(t, err)
	require.Equal(t, bobB.Add(f.params().TokenDeposit).Add(spark(2).Amount), f.bank.bal(f.bob))
	require.True(t, f.moduleBalance().Equal(f.params().TokenDeposit), "only token 1's deposit remains")
	f.checkInvariants()
}

func TestBlindMintInboxRevealAndRevoke(t *testing.T) {
	f := initFixture(t)
	root, salts, metas, proofs := f.blindTree(1, 2)
	deadline := f.ctx.BlockTime().Unix() + types.MinRevealWindow + 100
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_BLIND_METADATA, Commitment: hex.EncodeToString(root), Deadline: deadline}
	})
	// A minter mint of a blind class carries empty metadata and lands in the
	// inbox of a recipient who asked for one; it can be revealed while pending.
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.dave.String()}}})
	require.NoError(t, err)
	pm, err := f.keeper.PendingMints.Get(f.ctx, keeperPair(id, 1))
	require.NoError(t, err)
	require.True(t, pm.Unrevealed)
	_, err = f.msg.Reveal(f.ctx, &types.MsgReveal{Signer: f.alice.String(), ClassId: id, TokenId: 1, Metadata: metas[0],
		Salt: hex.EncodeToString(salts[0]), Proof: types.HexHashes(proofs[0])})
	require.NoError(t, err)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.dave.String(), Entries: []types.AcceptEntry{{ClassId: id, TokenId: 1, ExpectedMetadataHash: types.MetadataHash(metas[0])}}})
	require.NoError(t, err)
	require.False(t, f.token(id, 1).Unrevealed, "accepted after reveal")
	require.True(t, f.token(id, 1).MetadataFrozen)
	f.checkInvariants()
}

func TestGenerativeSeedReveal(t *testing.T) {
	f := initFixture(t)
	seed := []byte("phoenix-rising")
	deadline := f.ctx.BlockTime().Unix() + types.MinRevealWindow + 100
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: types.SeedCommitment(seed), Deadline: deadline}
		m.PayoutAddress = f.carol.String()
	})
	carolB := f.bank.bal(f.carol)
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 3, MaxPrice: spark(1)})
	require.NoError(t, err)
	require.Equal(t, spark(1).Amount, f.token(id, 2).Paid)
	require.False(t, f.token(id, 2).Unrevealed, "generative tokens carry their metadata; only the seed is pending")
	require.Empty(t, f.tokenQuery(id, 2).Seed)

	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.alice.String(), ClassId: id, Seed: hex.EncodeToString([]byte("wrong"))})
	require.ErrorIs(t, err, types.ErrInvalidReveal)
	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.bob.String(), ClassId: id, Seed: hex.EncodeToString(seed)})
	require.ErrorIs(t, err, types.ErrNotClassOwner)
	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.alice.String(), ClassId: id, Seed: hex.EncodeToString(seed)})
	require.NoError(t, err)
	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.alice.String(), ClassId: id, Seed: hex.EncodeToString(seed)})
	require.ErrorIs(t, err, types.ErrAlreadyRevealed)
	require.Equal(t, types.TokenSeed(seed, id, 2), f.tokenQuery(id, 2).Seed)
	require.Equal(t, carolB, f.bank.bal(f.carol), "payments wait for the release queue")

	f.setParams(func(p *types.Params) { p.MaxExpirationsPerBlock = 10 })
	f.advance(1, 1)
	f.endBlock()
	fee := spark(1).Amount.MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	require.Equal(t, carolB.Add(spark(3).Amount.Sub(fee.MulRaw(3))), f.bank.bal(f.carol))
	require.True(t, f.token(id, 3).Paid.IsZero())
	_, err = f.keeper.ClassReleaseQueue.Get(f.ctx, id)
	require.Error(t, err, "queue drained")
	require.Equal(t, f.params().TokenDeposit.MulRaw(3), f.moduleBalance())
	f.checkInvariants()
}

func TestGenerativeDeadlineRefund(t *testing.T) {
	f := initFixture(t)
	seed := []byte("never-revealed")
	deadline := f.ctx.BlockTime().Unix() + types.MinRevealWindow
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(3)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: types.SeedCommitment(seed), Deadline: deadline}
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: id, Quantity: 1, MaxPrice: spark(3)})
	require.NoError(t, err)
	// Issuer revocation is impossible on a transferable class; the holder
	// waits for the deadline and reclaims the price.
	f.advance(types.MinRevealWindow, 1)
	bobB := f.bank.bal(f.bob)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	require.Equal(t, bobB.Add(spark(3).Amount).Add(f.params().TokenDeposit), f.bank.bal(f.bob))
	require.True(t, f.moduleBalance().IsZero())
	f.checkInvariants()
}

// --- Wave 2: presentation -------------------------------------------------------

func TestMetadataSchedule(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	now := f.ctx.BlockTime().Unix()
	schedule := []types.MetadataStage{
		{FromTime: now + 100, Metadata: types.TokenMetadata{Name: "chick"}},
		{FromTime: now + 200, Metadata: types.TokenMetadata{Name: "phoenix", Uri: "ipfs://bafyphoenix"}},
	}
	_, err := f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.bob.String(), Metadata: types.TokenMetadata{Name: "egg"}, Schedule: []types.MetadataStage{schedule[1], schedule[0]}}}})
	require.ErrorIs(t, err, types.ErrInvalidSchedule)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.bob.String(), Metadata: types.TokenMetadata{Name: "egg"}, Schedule: schedule}}})
	require.NoError(t, err)
	tok := f.token(id, 1)
	require.Len(t, tok.Schedule, 2)
	require.Equal(t, uint32(8), tok.MediaFlags, "a future stage with a URI labels the token now")

	v := f.tokenQuery(id, 1)
	require.Equal(t, "egg", v.DisplayedMetadata.Name)
	require.Equal(t, types.DisplayStored, v.DisplaySource)
	require.Equal(t, types.MetadataHash(types.TokenMetadata{Name: "egg"}), v.MetadataHash, "the hash pins stored metadata")
	f.advance(100, 1)
	v = f.tokenQuery(id, 1)
	require.Equal(t, "chick", v.DisplayedMetadata.Name)
	require.Equal(t, types.DisplaySchedule, v.DisplaySource)
	f.advance(100, 1)
	require.Equal(t, "phoenix", f.tokenQuery(id, 1).DisplayedMetadata.Name)
	list, err := f.query.Tokens(f.ctx, &types.QueryTokensRequest{ClassId: id})
	require.NoError(t, err)
	require.Equal(t, "phoenix", list.Tokens[0].DisplayedMetadata.Name, "list queries follow the schedule too")

	f.setParams(func(p *types.Params) { p.MaxScheduleStages = 0 })
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.bob.String(), Metadata: types.TokenMetadata{Name: "egg"}, Schedule: []types.MetadataStage{{FromTime: f.ctx.BlockTime().Unix() + 5}}}}})
	require.ErrorIs(t, err, types.ErrInvalidSchedule, "schedules disabled")
	f.checkInvariants()
}

func TestMediaVersions(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 1)
	propose := func(name string) (uint64, error) {
		resp, err := f.msg.ProposeMedia(f.ctx, &types.MsgProposeMedia{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: types.TokenMetadata{Name: name, Uri: "ipfs://bafy" + name}})
		if err != nil {
			return 0, err
		}
		return resp.ProposalId, nil
	}
	_, err := f.msg.ProposeMedia(f.ctx, &types.MsgProposeMedia{Owner: f.bob.String(), ClassId: id, TokenId: 1, Metadata: meta("x")})
	require.ErrorIs(t, err, types.ErrNotClassOwner, "only the issuer proposes")
	p1, err := propose("remaster")
	require.NoError(t, err)
	p2, err := propose("animated")
	require.NoError(t, err)
	require.Equal(t, uint64(1), p1)
	require.Equal(t, uint64(2), p2)
	props, err := f.query.MediaProposals(f.ctx, &types.QueryMediaProposalsRequest{ClassId: id, TokenId: 1})
	require.NoError(t, err)
	require.Len(t, props.Proposals, 2)
	f.setParams(func(p *types.Params) { p.MaxMediaProposalsPerToken = 2 })
	_, err = propose("third")
	require.ErrorIs(t, err, types.ErrTooManyMediaProposals)

	_, err = f.msg.AcceptMedia(f.ctx, &types.MsgAcceptMedia{Owner: f.alice.String(), ClassId: id, TokenId: 1, ProposalId: p1})
	require.ErrorIs(t, err, types.ErrNotTokenOwner, "only the holder accepts")
	acc, err := f.msg.AcceptMedia(f.ctx, &types.MsgAcceptMedia{Owner: f.bob.String(), ClassId: id, TokenId: 1, ProposalId: p1, Display: true})
	require.NoError(t, err)
	require.Equal(t, uint32(1), acc.Version)
	tok := f.token(id, 1)
	require.Len(t, tok.MediaVersions, 1)
	require.Equal(t, uint32(1), tok.DisplayedVersion)
	v := f.tokenQuery(id, 1)
	require.Equal(t, "remaster", v.DisplayedMetadata.Name)
	require.Equal(t, types.DisplayMediaVersion, v.DisplaySource)
	require.Equal(t, "t0", v.Token.Metadata.Name, "stored metadata untouched")

	_, err = f.msg.SetDisplayedMedia(f.ctx, &types.MsgSetDisplayedMedia{Owner: f.bob.String(), ClassId: id, TokenId: 1, Version: 2})
	require.ErrorIs(t, err, types.ErrInvalidMediaVersion)
	_, err = f.msg.SetDisplayedMedia(f.ctx, &types.MsgSetDisplayedMedia{Owner: f.bob.String(), ClassId: id, TokenId: 1, Version: 0})
	require.NoError(t, err)
	require.Equal(t, types.DisplayStored, f.tokenQuery(id, 1).DisplaySource)

	_, err = f.msg.RejectMedia(f.ctx, &types.MsgRejectMedia{Signer: f.dave.String(), ClassId: id, TokenId: 1, ProposalId: p2})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.msg.RejectMedia(f.ctx, &types.MsgRejectMedia{Signer: f.alice.String(), ClassId: id, TokenId: 1, ProposalId: p2})
	require.NoError(t, err, "the issuer withdraws")
	_, err = f.msg.RejectMedia(f.ctx, &types.MsgRejectMedia{Signer: f.bob.String(), ClassId: id, TokenId: 1, ProposalId: p2})
	require.ErrorIs(t, err, types.ErrMediaProposalNotFound)

	// Burning clears open proposals.
	_, err = propose("late")
	require.NoError(t, err)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	n := 0
	require.NoError(t, f.keeper.MediaProposals.Walk(f.ctx, nil, func(_ collections.Triple[uint64, uint64, uint64], _ types.MediaProposal) (bool, error) {
		n++
		return false, nil
	}))
	require.Zero(t, n)
	f.checkInvariants()
}

func TestRenderer(t *testing.T) {
	f := initFixture(t)
	pkgPath := "gno.land/p/demo/aurora"
	pkg := types.GnoPackage{
		Files: []types.GnoSourceFile{{Name: "aurora.gno", Body: "package aurora\n\nfunc Render(a uint64, b uint64, c string, d int64, e int64) string { return \"\" }\n"}},
		Pure:  true,
		Funcs: []types.GnoFuncSig{{Name: "Render", Params: []string{"uint64", "uint64", "string", "int64", "int64"}, Results: []string{"string"}}},
	}
	f.gnovm.pkgs[pkgPath] = pkg
	hash := types.RendererSourceHash(pkg.Files)
	create := func(r types.Renderer) error {
		_, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{Creator: f.alice.String(), Name: "Aurora", AcceptedContentLicense: types.ContentLicense,
			Flags: types.ClassFlags{Transferable: true}, Renderer: r})
		return err
	}
	require.ErrorIs(t, create(types.Renderer{PkgPath: "gno.land/p/demo/missing", SourceHash: hash}), types.ErrInvalidRenderer)
	require.ErrorIs(t, create(types.Renderer{PkgPath: pkgPath, SourceHash: types.SeedCommitment([]byte("x"))}), types.ErrInvalidRenderer)
	require.ErrorIs(t, create(types.Renderer{SourceHash: hash}), types.ErrInvalidRenderer)
	require.NoError(t, create(types.Renderer{PkgPath: pkgPath, SourceHash: hash}))
	id := uint64(1)
	f.mint(f.alice, id, f.bob, 1)

	f.gnovm.eval = func(_, expr string) (string, error) {
		return fmt.Sprintf(`{"name":"rendered at %d","attributes":[{"key":"expr","value":%q}]}`, f.ctx.BlockHeight(), expr), nil
	}
	v := f.tokenQuery(id, 1)
	require.Equal(t, types.DisplayRenderer, v.DisplaySource)
	require.Equal(t, "rendered at 100", v.DisplayedMetadata.Name)
	require.Equal(t, types.RenderExpr(id, 1, "", f.ctx.BlockTime().Unix(), 100), v.DisplayedMetadata.Attributes[0].Value)
	require.Empty(t, v.RenderError)
	require.Equal(t, "t0", v.Token.Metadata.Name, "stored metadata is untouched")
	list, err := f.query.Tokens(f.ctx, &types.QueryTokensRequest{ClassId: id})
	require.NoError(t, err)
	require.Equal(t, types.DisplayStored, list.Tokens[0].DisplaySource, "list queries never render")

	// Every failure falls back to stored metadata and says why.
	f.gnovm.eval = func(_, _ string) (string, error) { return "", fmt.Errorf("out of gas") }
	v = f.tokenQuery(id, 1)
	require.Equal(t, types.DisplayStored, v.DisplaySource)
	require.Contains(t, v.RenderError, "out of gas")
	f.gnovm.eval = func(_, _ string) (string, error) { return "not json", nil }
	require.Contains(t, f.tokenQuery(id, 1).RenderError, "not TokenMetadata JSON")
	f.gnovm.eval = func(_, _ string) (string, error) { return `{"uri":"data:text/plain,evil"}`, nil }
	require.Contains(t, f.tokenQuery(id, 1).RenderError, "data URI")
	f.gnovm.eval = func(_, _ string) (string, error) { panic("vm crashed") }
	require.Contains(t, f.tokenQuery(id, 1).RenderError, "panic")
	f.gnovm.eval = func(_, _ string) (string, error) { return `{"name":"ok"}`, nil }
	f.setParams(func(p *types.Params) { p.RenderersEnabled = false })
	v = f.tokenQuery(id, 1)
	require.Equal(t, types.DisplayStored, v.DisplaySource)
	require.Contains(t, v.RenderError, "disabled")
	f.setParams(func(p *types.Params) { p.RenderersEnabled = true })

	// The holder's media choice outranks the renderer.
	_, err = f.msg.ProposeMedia(f.ctx, &types.MsgProposeMedia{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: types.TokenMetadata{Name: "still"}})
	require.NoError(t, err)
	_, err = f.msg.AcceptMedia(f.ctx, &types.MsgAcceptMedia{Owner: f.bob.String(), ClassId: id, TokenId: 1, ProposalId: 1, Display: true})
	require.NoError(t, err)
	calls := len(f.gnovm.calls)
	v = f.tokenQuery(id, 1)
	require.Equal(t, types.DisplayMediaVersion, v.DisplaySource)
	require.Equal(t, calls, len(f.gnovm.calls), "renderer not evaluated")

	// Without a wired gnovm, renderer classes cannot be created.
	f.keeper.SetGnoVMKeeper(nil)
	require.ErrorIs(t, create(types.Renderer{PkgPath: pkgPath, SourceHash: hash}), types.ErrRendererUnavailable)
	f.checkInvariants()
}

func TestWaveGenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	seed := []byte("seed")
	deadline := f.ctx.BlockTime().Unix() + types.MinRevealWindow + 10
	gen := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1)}
		m.Reveal = types.RevealPolicy{Kind: types.RevealKind_REVEAL_KIND_GENERATIVE_SEED, Commitment: types.SeedCommitment(seed), Deadline: deadline}
		m.Expiry = types.ExpiryPolicy{Mode: types.ExpiryMode_EXPIRY_MODE_BURN, Term: 500}
	})
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.bob.String(), ClassId: gen, Quantity: 2, MaxPrice: spark(1)})
	require.NoError(t, err)
	_, err = f.msg.RevealSeed(f.ctx, &types.MsgRevealSeed{Owner: f.alice.String(), ClassId: gen, Seed: hex.EncodeToString(seed)})
	require.NoError(t, err)
	_, err = f.msg.ProposeMedia(f.ctx, &types.MsgProposeMedia{Owner: f.alice.String(), ClassId: gen, TokenId: 1, Metadata: meta("alt")})
	require.NoError(t, err)

	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.MediaProposals, 1)
	require.Len(t, gs.ClassReleaseQueue, 1)
	require.NoError(t, gs.Validate())

	g := initFixture(t)
	g.bank.balances = f.bank.balances
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	g.checkInvariants()
	n := 0
	require.NoError(t, g.keeper.TokenExpiry.Walk(g.ctx, nil, func(_ keeperTimeKey) (bool, error) { n++; return false, nil }))
	require.Equal(t, 2, n, "expiry index rebuilt")
	again, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, gs, again)

	// Genesis validation catches wave-specific inconsistencies.
	bad := *gs
	bad.Tokens = append([]types.Token(nil), gs.Tokens...)
	bad.Tokens[0].Unrevealed = true
	require.Error(t, bad.Validate(), "unrevealed on a generative class")
	bad = *gs
	bad.MediaProposals = []types.MediaProposal{{ClassId: 9, TokenId: 1, Id: 1}}
	require.Error(t, bad.Validate())
}

type keeperTimeKey = collections.Triple[int64, uint64, uint64]

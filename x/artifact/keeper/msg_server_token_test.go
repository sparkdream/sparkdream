package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestMintDirectAndDeposits(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	before := f.bank.bal(f.alice)

	res := f.mint(f.alice, id, f.alice, 3)
	require.Len(t, res, 3)
	for i, r := range res {
		require.Equal(t, uint64(i+1), r.TokenId)
		require.True(t, r.Live)
	}
	deposit := f.params().TokenDeposit
	require.Equal(t, before.Sub(deposit.MulRaw(3)), f.bank.bal(f.alice))
	require.Equal(t, deposit.MulRaw(3), f.moduleBalance())

	tok := f.token(id, 2)
	require.Equal(t, f.alice.String(), tok.Owner)
	require.Equal(t, f.alice.String(), tok.Minter)
	require.Equal(t, deposit, tok.Deposit)
	require.False(t, tok.MetadataFrozen, "mutable class mints unfrozen tokens")
	require.Equal(t, uint32(8), tok.MediaFlags)

	c := f.class(id)
	require.Equal(t, uint64(3), c.Supply)
	require.Equal(t, uint64(4), c.NextTokenId)
	f.checkInvariants()
}

func TestMintValidation(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	mintOne := func(minter string, m types.TokenMetadata, license string) error {
		_, err := f.msg.Mint(f.ctx, &types.MsgMint{Minter: minter, ClassId: id,
			Entries: []types.MintEntry{{Metadata: m}}, AcceptedContentLicense: license})
		return err
	}
	require.ErrorIs(t, mintOne(f.alice.String(), meta("a"), ""), types.ErrContentLicenseNotAccepted)
	require.ErrorIs(t, mintOne(f.bob.String(), meta("a"), types.ContentLicense), types.ErrNotMinter)
	require.ErrorIs(t, mintOne(f.alice.String(), types.TokenMetadata{Uri: "data:text/plain,hi"}, types.ContentLicense), types.ErrInlineData)
	require.ErrorIs(t, mintOne(f.alice.String(), types.TokenMetadata{Uri: "javascript:alert(1)"}, types.ContentLicense), types.ErrInvalidMetadata)
	require.ErrorIs(t, mintOne(f.alice.String(), types.TokenMetadata{Attributes: []types.Attribute{{Key: "A", Value: "x"}}}, types.ContentLicense), types.ErrInvalidMetadata)
	require.ErrorIs(t, mintOne(f.alice.String(), types.TokenMetadata{Attributes: []types.Attribute{{Key: "a", Value: "x"}, {Key: "a", Value: "y"}}}, types.ContentLicense), types.ErrInvalidMetadata)

	_, err := f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInvalidBatch)

	// A blocked (module) recipient would strand the token.
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id,
		Entries: []types.MintEntry{{Recipient: types.ModuleAddressString()}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)

	// A failed batch changes nothing.
	require.Equal(t, uint64(0), f.class(id).Supply)
	require.True(t, f.moduleBalance().IsZero())
}

func TestImmutableClassMintsFrozenTokens(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Flags.TokenMetadataMutable = false })
	f.mint(f.alice, id, f.alice, 1)
	require.True(t, f.token(id, 1).MetadataFrozen)
	_, err := f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: meta("new")})
	require.ErrorIs(t, err, types.ErrMetadataFrozen)
}

func TestReceivePolicyMatrix(t *testing.T) {
	cases := []struct {
		name      string
		policy    types.ReceivePolicy
		trusted   bool
		delivered bool
	}{
		{"default members / trusted", types.ReceivePolicy_RECEIVE_POLICY_UNSPECIFIED, true, true},
		{"default members / untrusted", types.ReceivePolicy_RECEIVE_POLICY_UNSPECIFIED, false, false},
		{"open / untrusted", types.ReceivePolicy_RECEIVE_POLICY_OPEN, false, true},
		{"inbox / trusted", types.ReceivePolicy_RECEIVE_POLICY_INBOX, true, false},
		{"members / trusted", types.ReceivePolicy_RECEIVE_POLICY_MEMBERS, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			id := f.createClass(f.alice)
			sender := f.alice
			if !tc.trusted {
				// dave is not a member: make him the class minter's token holder.
				f.mint(f.alice, id, f.alice, 1)
				_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_OPEN})
				require.NoError(t, err)
				_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.alice.String(),
					Entries: []types.TransferEntry{{ClassId: id, TokenId: 1, Recipient: f.dave.String()}}})
				require.NoError(t, err)
				require.Equal(t, f.dave.String(), f.token(id, 1).Owner)
				sender = f.dave
			} else {
				f.mint(f.alice, id, f.alice, 1)
			}
			_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.bob.String(), Policy: tc.policy})
			require.NoError(t, err)

			resp, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: sender.String(),
				Entries: []types.TransferEntry{{ClassId: id, TokenId: 1, Recipient: f.bob.String()}}})
			require.NoError(t, err)
			require.Equal(t, tc.delivered, resp.Results[0].Delivered)
			tok := f.token(id, 1)
			if tc.delivered {
				require.Equal(t, f.bob.String(), tok.Owner)
				require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, tok.Lock)
			} else {
				require.Equal(t, sender.String(), tok.Owner)
				require.Equal(t, types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER, tok.Lock)
			}
			f.checkInvariants()
		})
	}
}

func TestPendingMintAcceptRejectExpire(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.bob.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)

	burnedBefore := f.bank.burned.AmountOf(bond)
	res := f.mint(f.alice, id, f.bob, 3)
	for _, r := range res {
		require.False(t, r.Live)
	}
	p := f.params()
	require.Equal(t, burnedBefore.Add(p.InboxFee.MulRaw(3)), f.bank.burned.AmountOf(bond), "inbox fee burned per item")
	c := f.class(id)
	require.Equal(t, uint64(0), c.Supply)
	require.Equal(t, uint64(3), c.ReservedSupply)
	require.Equal(t, p.TokenDeposit.MulRaw(3), f.moduleBalance())

	inbox, err := f.query.Inbox(f.ctx, &types.QueryInboxRequest{Address: f.bob.String()})
	require.NoError(t, err)
	require.Len(t, inbox.Items, 3)
	require.Equal(t, uint32(3), inbox.InboxCount)
	require.Equal(t, types.InboxKind_INBOX_KIND_MINT, inbox.Items[0].Kind)
	hash := inbox.Items[0].MetadataHash

	// Accept #1 with a pinned hash; a wrong hash is refused.
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(),
		Entries: []types.AcceptEntry{{ClassId: id, TokenId: 1, ExpectedMetadataHash: "00"}}})
	require.ErrorIs(t, err, types.ErrMetadataChanged)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(),
		Entries: []types.AcceptEntry{{ClassId: id, TokenId: 1, ExpectedMetadataHash: hash}}})
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), f.token(id, 1).Owner)
	require.Equal(t, f.alice.String(), f.token(id, 1).Minter)

	// Only the recipient can accept.
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.carol.String(),
		Entries: []types.AcceptEntry{{ClassId: id, TokenId: 2}}})
	require.ErrorIs(t, err, types.ErrPendingNotFound)

	// Reject #2: deposit back to the minter, id consumed, slot freed.
	aliceBefore := f.bank.bal(f.alice)
	_, err = f.msg.RejectIncoming(f.ctx, &types.MsgRejectIncoming{Recipient: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 2}}})
	require.NoError(t, err)
	require.Equal(t, aliceBefore.Add(p.TokenDeposit), f.bank.bal(f.alice))
	c = f.class(id)
	require.Equal(t, uint64(1), c.Supply)
	require.Equal(t, uint64(1), c.ReservedSupply)
	require.Equal(t, uint64(0), c.Burned, "a rejected mint is not a burn")

	// #3 expires: refused at use, then drained by the EndBlocker.
	f.advance(p.PendingTtl, 1)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(),
		Entries: []types.AcceptEntry{{ClassId: id, TokenId: 3}}})
	require.ErrorIs(t, err, types.ErrPendingExpired)
	inbox, err = f.query.Inbox(f.ctx, &types.QueryInboxRequest{Address: f.bob.String()})
	require.NoError(t, err)
	require.Empty(t, inbox.Items, "expired items are filtered from queries")
	f.endBlock()
	c = f.class(id)
	require.Equal(t, uint64(0), c.ReservedSupply)
	require.Equal(t, uint64(4), c.NextTokenId, "ids are never reused")
	require.Equal(t, p.TokenDeposit, f.moduleBalance())
}

func TestInboxCaps(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.MaxPendingPerPair = 2; p.MaxInboxPerRecipient = 10 })
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Minters = []string{f.carol.String()} })
	// dave (non-member, default MEMBERS policy) gets everything in his inbox
	// only from untrusted senders; members deliver directly, so make dave INBOX.
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)

	f.mint(f.alice, id, f.dave, 2)
	// The pair cap is keyed by class for mints: a second minter of the same
	// class cannot add more.
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.carol.String(), ClassId: id,
		Entries: []types.MintEntry{{Recipient: f.dave.String(), Metadata: meta("x")}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInboxFull)

	// Cancelling one frees a slot.
	_, err = f.msg.CancelOutgoing(f.ctx, &types.MsgCancelOutgoing{Signer: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	f.mint(f.carol, id, f.dave, 1)

	// The class owner may cancel a pending mint signed by a minter; others may not.
	_, err = f.msg.CancelOutgoing(f.ctx, &types.MsgCancelOutgoing{Signer: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 3}}})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.msg.CancelOutgoing(f.ctx, &types.MsgCancelOutgoing{Signer: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 3}}})
	require.NoError(t, err)
	f.checkInvariants()
}

func TestInboxRecipientCap(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.MaxPendingPerPair = 50; p.MaxInboxPerRecipient = 10 })
	id := f.createClass(f.alice)
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	f.mint(f.alice, id, f.dave, 10)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id,
		Entries: []types.MintEntry{{Recipient: f.dave.String()}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrInboxFull)
}

func TestTransferRules(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 2)
	xfer := func(sender, to string, tokenID uint64) error {
		_, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: sender, Entries: []types.TransferEntry{{ClassId: id, TokenId: tokenID, Recipient: to}}})
		return err
	}
	require.ErrorIs(t, xfer(f.bob.String(), f.carol.String(), 1), types.ErrNotTokenOwner)
	require.ErrorIs(t, xfer(f.alice.String(), f.alice.String(), 1), types.ErrInvalidRecipient)
	require.ErrorIs(t, xfer(f.alice.String(), types.ModuleAddressString(), 1), types.ErrInvalidRecipient)
	require.ErrorIs(t, xfer(f.alice.String(), f.bob.String(), 99), types.ErrTokenNotFound)

	// Duplicate entries are refused.
	_, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.alice.String(), Entries: []types.TransferEntry{
		{ClassId: id, TokenId: 1, Recipient: f.bob.String()}, {ClassId: id, TokenId: 1, Recipient: f.carol.String()}}})
	require.ErrorIs(t, err, types.ErrInvalidBatch)

	// A pending transfer locks the token.
	_, err = f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.bob.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	require.NoError(t, xfer(f.alice.String(), f.bob.String(), 1))
	require.ErrorIs(t, xfer(f.alice.String(), f.carol.String(), 1), types.ErrTokenLocked)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.ErrorIs(t, err, types.ErrTokenLocked)

	// Accept moves ownership and clears the lock.
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(), Entries: []types.AcceptEntry{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	tok := f.token(id, 1)
	require.Equal(t, f.bob.String(), tok.Owner)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, tok.Lock)

	// A cancelled transfer returns control to the sender.
	require.NoError(t, xfer(f.alice.String(), f.bob.String(), 2))
	_, err = f.msg.CancelOutgoing(f.ctx, &types.MsgCancelOutgoing{Signer: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 2}}})
	require.NoError(t, err)
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, 2).Lock)
	require.Equal(t, f.alice.String(), f.token(id, 2).Owner)

	// An expired transfer is returned by the EndBlocker.
	require.NoError(t, xfer(f.alice.String(), f.bob.String(), 2))
	f.advance(f.params().PendingTtl+1, 1)
	f.endBlock()
	require.Equal(t, types.TokenLock_TOKEN_LOCK_NONE, f.token(id, 2).Lock)
	require.Equal(t, f.alice.String(), f.token(id, 2).Owner)
	f.checkInvariants()
}

func TestSoulboundAndRevoke(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.council, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}
		m.RoyaltyBps = 0
	})
	f.mint(f.council, id, f.bob, 2)
	require.Equal(t, f.bob.String(), f.token(id, 1).Owner, "council bodies deliver directly")

	_, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.bob.String(), Entries: []types.TransferEntry{{ClassId: id, TokenId: 1, Recipient: f.carol.String()}}})
	require.ErrorIs(t, err, types.ErrNotTransferable)
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.bob.String(), ClassId: id, TokenId: 1, Price: spark(1), Duration: 100})
	require.ErrorIs(t, err, types.ErrNotTransferable)

	// Revocation refunds the deposit to the holder, never the issuer.
	bobBefore, councilBefore := f.bank.bal(f.bob), f.bank.bal(f.council)
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.council.String(), ClassId: id, TokenIds: []uint64{1}, Reason: "term ended"})
	require.NoError(t, err)
	require.Equal(t, bobBefore.Add(f.params().TokenDeposit), f.bank.bal(f.bob))
	require.Equal(t, councilBefore, f.bank.bal(f.council))
	c := f.class(id)
	require.Equal(t, uint64(1), c.Supply)
	require.Equal(t, uint64(1), c.Burned)

	// The holder can always burn a soulbound token.
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 2}}})
	require.NoError(t, err)

	// A non-revocable class cannot revoke.
	other := f.createClass(f.alice)
	f.mint(f.alice, other, f.bob, 1)
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.alice.String(), ClassId: other, TokenIds: []uint64{1}})
	require.ErrorIs(t, err, types.ErrNotRevocable)
	f.checkInvariants()
}

func TestIssuerOnlyBurn(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.council, func(m *types.MsgCreateClass) {
		m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_ISSUER}
		m.RoyaltyBps = 0
	})
	f.mint(f.council, id, f.bob, 1)

	// Even a council body's token waits in the inbox: the holder must consent
	// to a token they can never burn.
	_, err := f.keeper.Tokens.Get(f.ctx, keeperPair(id, 1))
	require.Error(t, err, "issuer-only tokens are never delivered directly")
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.bob.String(),
		Entries: []types.AcceptEntry{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), f.token(id, 1).Owner)

	// The holder cannot burn an issuer-only token.
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.ErrorIs(t, err, types.ErrHolderBurnNotAllowed)

	// The issuer can, and the deposit still goes to the holder.
	bobBefore := f.bank.bal(f.bob)
	_, err = f.msg.Revoke(f.ctx, &types.MsgRevoke{Owner: f.council.String(), ClassId: id, TokenIds: []uint64{1}, Reason: "expired credential"})
	require.NoError(t, err)
	require.Equal(t, bobBefore.Add(f.params().TokenDeposit), f.bank.bal(f.bob))
	f.checkInvariants()
}

func TestBurnRefundsBurner(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 1) // alice pays the deposit
	bobBefore := f.bank.bal(f.bob)
	_, err := f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.ErrorIs(t, err, types.ErrNotTokenOwner)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	require.Equal(t, bobBefore.Add(f.params().TokenDeposit), f.bank.bal(f.bob), "spam pays its victims")
	require.True(t, f.moduleBalance().IsZero())
	f.checkInvariants()
}

func TestUpdateAndFreezeToken(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.bob, 2)

	resp, err := f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: meta("v2")})
	require.NoError(t, err)
	require.Equal(t, types.MetadataHash(meta("v2")), resp.MetadataHash)
	require.Equal(t, "v2", f.token(id, 1).Metadata.Name)

	_, err = f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.bob.String(), ClassId: id, TokenId: 1, Metadata: meta("v3")})
	require.ErrorIs(t, err, types.ErrNotClassOwner)

	// The holder freezes their own token.
	_, err = f.msg.FreezeTokenMetadata(f.ctx, &types.MsgFreezeTokenMetadata{Signer: f.bob.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	_, err = f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: 1, Metadata: meta("v3")})
	require.ErrorIs(t, err, types.ErrMetadataFrozen)
	// A stranger cannot freeze.
	_, err = f.msg.FreezeTokenMetadata(f.ctx, &types.MsgFreezeTokenMetadata{Signer: f.carol.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 2}}})
	require.ErrorIs(t, err, types.ErrNotTokenOwner)

	// Freezing the class freezes every token.
	_, err = f.msg.FreezeClassMetadata(f.ctx, &types.MsgFreezeClassMetadata{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	_, err = f.msg.UpdateToken(f.ctx, &types.MsgUpdateToken{Owner: f.alice.String(), ClassId: id, TokenId: 2, Metadata: meta("v3")})
	require.ErrorIs(t, err, types.ErrMetadataFrozen)
}

func TestPublicMint(t *testing.T) {
	f := initFixture(t)
	start := f.ctx.BlockTime().Unix() + 100
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MaxSupply = 5
		m.PayoutAddress = f.carol.String()
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(2), StartTime: start, PerAddressLimit: 3}
	})
	buy := func(buyer string, qty uint32, max int64) error {
		_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: buyer, ClassId: id, Quantity: qty, MaxPrice: spark(max)})
		return err
	}
	require.ErrorIs(t, buy(f.dave.String(), 1, 2), types.ErrMintWindow, "not started")
	f.advance(100, 1)

	require.ErrorIs(t, buy(f.dave.String(), 1, 1), types.ErrPriceExceeded)
	_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.dave.String(), ClassId: id, Quantity: 1, MaxPrice: sdkCoin(dream, 9)})
	require.ErrorIs(t, err, types.ErrDreamNotAccepted)

	daveBefore, carolBefore := f.bank.bal(f.dave), f.bank.bal(f.carol)
	require.NoError(t, buy(f.dave.String(), 2, 2))
	total := math.NewInt(4_000_000)
	fee := total.MulRaw(int64(f.params().SaleFeeBps)).QuoRaw(10000)
	deposits := f.params().TokenDeposit.MulRaw(2)
	require.Equal(t, daveBefore.Sub(total).Sub(deposits), f.bank.bal(f.dave))
	require.Equal(t, carolBefore.Add(total.Sub(fee)), f.bank.bal(f.carol), "payout minus fee, no royalty on primary sale")
	require.Equal(t, fee, f.distr.pool.AmountOf(bond))
	require.Equal(t, f.dave.String(), f.token(id, 1).Owner, "receive policy does not apply to the buyer")

	require.ErrorIs(t, buy(f.dave.String(), 2, 2), types.ErrPerAddressLimit)
	require.NoError(t, buy(f.bob.String(), 3, 2))
	require.ErrorIs(t, buy(f.carol.String(), 1, 2), types.ErrSupplyExceeded)

	quote, err := f.query.PublicMintQuote(f.ctx, &types.QueryPublicMintQuoteRequest{ClassId: id, Buyer: f.dave.String(), Quantity: 1})
	require.NoError(t, err)
	require.False(t, quote.Available)
	require.Equal(t, int64(1), quote.RemainingAllowance)

	// Circuit breaker.
	f.setParams(func(p *types.Params) { p.PublicMintEnabled = false })
	require.ErrorIs(t, buy(f.dave.String(), 1, 2), types.ErrMintWindow)
	f.checkInvariants()
}

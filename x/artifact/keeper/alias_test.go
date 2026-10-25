package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
	reptypes "sparkdream/x/rep/types"
)

// migrate tombstones from and makes to the canonical member address.
func (f *fixture) migrate(from, to string) {
	f.rep.migrated[from] = to
	delete(f.rep.members, from)
	f.rep.members[to] = reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED
}

// After a member key migration every record still names the old address;
// the successor acts through the alias and the old key is refused.
func TestAliasAuthorization(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 3)
	_, err := f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 3,
		Price: spark(1), Duration: 3600})
	require.NoError(t, err)

	f.migrate(f.alice.String(), f.dave.String())

	// Token owner: the successor transfers, the tombstoned key does not.
	xfer := func(sender string, tokenID uint64) error {
		_, err := f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: sender,
			Entries: []types.TransferEntry{{ClassId: id, TokenId: tokenID, Recipient: f.bob.String()}}})
		return err
	}
	require.ErrorIs(t, xfer(f.alice.String(), 1), types.ErrNotTokenOwner)
	require.NoError(t, xfer(f.dave.String(), 1))
	require.Equal(t, f.bob.String(), f.token(id, 1).Owner)

	// Self-dealing ban compares members, not addresses.
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.dave.String(),
		Entries: []types.TransferEntry{{ClassId: id, TokenId: 2, Recipient: f.alice.String()}}})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)
	_, err = f.msg.Buy(f.ctx, &types.MsgBuy{Buyer: f.dave.String(), ClassId: id, TokenId: 3,
		ExpectedPrice: spark(1), ExpectedNonce: f.token(id, 3).ListingSeq})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)

	// Listing seller and class owner resolve through the alias.
	_, err = f.msg.Delist(f.ctx, &types.MsgDelist{Seller: f.dave.String(), ClassId: id, TokenId: 3})
	require.NoError(t, err)
	_, err = f.msg.CloseMinting(f.ctx, &types.MsgCloseMinting{Owner: f.alice.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrNotClassOwner)
	_, err = f.msg.CloseMinting(f.ctx, &types.MsgCloseMinting{Owner: f.dave.String(), ClassId: id})
	require.NoError(t, err)

	// By-owner queries union over every address of the member.
	for _, who := range []string{f.dave.String(), f.alice.String()} {
		res, err := f.query.TokensByOwner(f.ctx, &types.QueryTokensByOwnerRequest{Owner: who})
		require.NoError(t, err)
		require.Len(t, res.Tokens, 2, who)
		cls, err := f.query.ClassesByOwner(f.ctx, &types.QueryClassesByOwnerRequest{Owner: who})
		require.NoError(t, err)
		require.Len(t, cls.Classes, 1, who)
	}
	f.checkInvariants()
}

// The per-address public mint limit counts the member's recent addresses.
func TestAliasPublicMintLimit(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, PerAddressLimit: 2}
	})
	mint := func(buyer string, n uint32) error {
		_, err := f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: buyer, ClassId: id, Quantity: n, MaxPrice: spark(0)})
		return err
	}
	require.NoError(t, mint(f.bob.String(), 2))
	f.migrate(f.bob.String(), f.dave.String())
	require.ErrorIs(t, mint(f.dave.String(), 1), types.ErrPerAddressLimit)
}

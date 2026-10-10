package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/artifact/types"
)

// populate builds a state touching every exported collection.
func populate(f *fixture) uint64 {
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.Minters = []string{f.carol.String()} })
	f.mint(f.alice, id, f.alice, 4)
	_, err := f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(f.t, err)
	f.mint(f.alice, id, f.dave, 1) // pending mint
	_, err = f.msg.Transfer(f.ctx, &types.MsgTransfer{Sender: f.alice.String(), Entries: []types.TransferEntry{{ClassId: id, TokenId: 1, Recipient: f.dave.String()}}})
	require.NoError(f.t, err) // pending transfer
	_, err = f.msg.List(f.ctx, &types.MsgList{Seller: f.alice.String(), ClassId: id, TokenId: 2, Price: spark(3), Duration: 600})
	require.NoError(f.t, err)
	_, err = f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(f.t, err)
	_, err = f.msg.HideContent(f.ctx, &types.MsgHideContent{Authority: f.sentinel.String(), TargetKind: types.HideTargetKind_HIDE_TARGET_KIND_TOKEN,
		ClassId: id, TokenId: 3, Reason: commontypes.ModerationReason_MODERATION_REASON_SPAM})
	require.NoError(f.t, err)
	id2 := f.createClass(f.bob, func(m *types.MsgCreateClass) {
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1)}
	})
	_, err = f.msg.PublicMint(f.ctx, &types.MsgPublicMint{Buyer: f.carol.String(), ClassId: id2, Quantity: 2, MaxPrice: spark(1)})
	require.NoError(f.t, err)
	f.checkInvariants()
	return id
}

func TestGenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	populate(f)
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	require.Len(t, exported.Classes, 2)
	require.Len(t, exported.PendingMints, 1)
	require.Len(t, exported.PendingTransfers, 1)
	require.Len(t, exported.Listings, 1)
	require.Len(t, exported.PendingClassOwners, 1)
	require.Len(t, exported.HideRecords, 1)
	require.Len(t, exported.ReceivePolicies, 1)
	require.Len(t, exported.PublicMintCounts, 1)

	g := initFixture(t)
	// The fresh fixture's module account must hold the escrow too.
	g.bank.balances[moduleAddr(types.ModuleName)] = f.bank.balances[moduleAddr(types.ModuleName)]
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	g.checkInvariants()
	reexported, err := g.keeper.ExportGenesis(g.ctx)
	require.NoError(t, err)
	require.Equal(t, exported, reexported)

	// Derived counters were rebuilt: the next class id and inbox caps work.
	require.Equal(t, uint64(3), g.createClass(g.alice))
	inbox, err := g.query.Inbox(g.ctx, &types.QueryInboxRequest{Address: g.dave.String()})
	require.NoError(t, err)
	require.Len(t, inbox.Items, 2)
	require.Equal(t, uint32(2), inbox.InboxCount)
}

func TestGenesisValidate(t *testing.T) {
	f := initFixture(t)
	populate(f)
	good, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)

	cases := []struct {
		name string
		mut  func(gs *types.GenesisState)
	}{
		{"class above seq", func(gs *types.GenesisState) { gs.ClassSeq = 1 }},
		{"supply mismatch", func(gs *types.GenesisState) { gs.Classes[0].Supply++ }},
		{"reserved mismatch", func(gs *types.GenesisState) { gs.Classes[0].ReservedSupply = 0 }},
		{"token id beyond next", func(gs *types.GenesisState) { gs.Classes[0].NextTokenId = 2 }},
		{"listing without lock", func(gs *types.GenesisState) {
			for i := range gs.Tokens {
				gs.Tokens[i].Lock = types.TokenLock_TOKEN_LOCK_NONE
			}
		}},
		{"issuer-burnable transferable", func(gs *types.GenesisState) {
			gs.Classes[0].Flags.BurnAuthorization = types.BurnAuthorization_BURN_AUTHORIZATION_ISSUER
		}},
		{"unknown burn authorization", func(gs *types.GenesisState) { gs.Classes[0].Flags.BurnAuthorization = 9 }},
		{"empty royalty split", func(gs *types.GenesisState) { gs.Classes[0].RoyaltyRecipients = nil }},
		{"hide above seq", func(gs *types.GenesisState) { gs.HideSeq = 0 }},
		{"unspecified policy", func(gs *types.GenesisState) { gs.ReceivePolicies[0].Policy = 0 }},
		{"bad params", func(gs *types.GenesisState) { gs.Params.SaleFeeBps = 5000 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs, err := f.keeper.ExportGenesis(f.ctx)
			require.NoError(t, err)
			tc.mut(gs)
			require.Error(t, gs.Validate())
		})
	}
	require.NoError(t, good.Validate())
	require.NoError(t, types.DefaultGenesis().Validate())
}

func TestUpdateParamsAndOperationalParams(t *testing.T) {
	f := initFixture(t)
	p := f.params()
	p.MaxBatchSize = 20
	_, err := f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.alice.String(), Params: p})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	_, err = f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.gov, Params: p})
	require.NoError(t, err)
	require.Equal(t, uint32(20), f.params().MaxBatchSize)

	bad := p
	bad.AllowedUriSchemes = []string{"https", "http"}
	_, err = f.msg.UpdateParams(f.ctx, &types.MsgUpdateParams{Authority: f.gov, Params: bad})
	require.ErrorIs(t, err, types.ErrInvalidParams)

	op := f.params().OperationalParams()
	op.InboxFee = op.InboxFee.MulRaw(2)
	_, err = f.msg.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.dave.String(), OperationalParams: op})
	require.ErrorIs(t, err, types.ErrInvalidSigner)
	_, err = f.msg.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.ops.String(), OperationalParams: op})
	require.NoError(t, err)
	require.Equal(t, op.InboxFee, f.params().InboxFee)
	require.Equal(t, uint32(20), f.params().MaxBatchSize, "governance fields untouched")

	// Hard bounds stop an individual ops member from zeroing spam backing.
	zero := op
	zero.TokenDeposit = zero.TokenDeposit.SubRaw(zero.TokenDeposit.Int64())
	_, err = f.msg.UpdateOperationalParams(f.ctx, &types.MsgUpdateOperationalParams{Authority: f.ops.String(), OperationalParams: zero})
	require.ErrorIs(t, err, types.ErrInvalidParams)
}

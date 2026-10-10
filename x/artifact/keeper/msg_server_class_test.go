package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestCreateClass(t *testing.T) {
	f := initFixture(t)
	before := f.bank.bal(f.alice)
	id := f.createClass(f.alice)
	require.Equal(t, uint64(1), id)

	c := f.class(id)
	require.Equal(t, f.alice.String(), c.Owner)
	require.Equal(t, f.alice.String(), c.Creator)
	require.Equal(t, f.alice.String(), c.PayoutAddress)
	require.Equal(t, types.SoleRoyaltyRecipient(f.alice.String()), c.RoyaltyRecipients)
	require.Equal(t, uint64(1), c.NextTokenId)
	require.Equal(t, types.ContentStatus_CONTENT_STATUS_ACTIVE, c.Status)
	require.Equal(t, uint32(8), c.MediaFlags) // EXTERNAL_URI
	require.Equal(t, bond, c.MintPolicy.Price.Denom)

	// Creation fee burned, not held.
	fee := f.params().ClassCreationFee
	require.Equal(t, before.Sub(fee), f.bank.bal(f.alice))
	require.Equal(t, fee, f.bank.burned.AmountOf(bond))
	require.True(t, f.moduleBalance().IsZero())

	require.Equal(t, uint64(2), f.createClass(f.alice))
	f.checkInvariants()
}

func TestCreateClassGates(t *testing.T) {
	base := func(f *fixture) *types.MsgCreateClass {
		return &types.MsgCreateClass{
			Creator: f.alice.String(), Name: "Zenith", Symbol: "ZEN",
			Flags: types.ClassFlags{Transferable: true}, AcceptedContentLicense: types.ContentLicense,
		}
	}
	cases := []struct {
		name string
		mut  func(f *fixture, m *types.MsgCreateClass)
		err  error
	}{
		{"non-member", func(f *fixture, m *types.MsgCreateClass) { m.Creator = f.dave.String() }, types.ErrInsufficientTrust},
		{"license missing", func(f *fixture, m *types.MsgCreateClass) { m.AcceptedContentLicense = "" }, types.ErrContentLicenseNotAccepted},
		{"license wrong", func(f *fixture, m *types.MsgCreateClass) { m.AcceptedContentLicense = "MIT" }, types.ErrContentLicenseNotAccepted},
		{"issuer-burnable transferable", func(f *fixture, m *types.MsgCreateClass) {
			m.Flags.BurnAuthorization = types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER
		}, types.ErrInvalidFlags},
		{"unknown burn authorization", func(f *fixture, m *types.MsgCreateClass) { m.Flags.BurnAuthorization = 9 }, types.ErrInvalidFlags},
		{"royalty weights short of total", func(f *fixture, m *types.MsgCreateClass) {
			m.RoyaltyRecipients = []types.RoyaltyShare{{Address: f.alice.String(), WeightBps: 6000}, {Address: f.carol.String(), WeightBps: 3000}}
		}, types.ErrInvalidRoyaltySplit},
		{"duplicate royalty recipient", func(f *fixture, m *types.MsgCreateClass) {
			m.RoyaltyRecipients = []types.RoyaltyShare{{Address: f.alice.String(), WeightBps: 5000}, {Address: f.alice.String(), WeightBps: 5000}}
		}, types.ErrInvalidRoyaltySplit},
		{"zero royalty weight", func(f *fixture, m *types.MsgCreateClass) {
			m.RoyaltyRecipients = []types.RoyaltyShare{{Address: f.alice.String(), WeightBps: 10000}, {Address: f.carol.String()}}
		}, types.ErrInvalidRoyaltySplit},
		{"royalty too high", func(f *fixture, m *types.MsgCreateClass) { m.RoyaltyBps = 1001 }, types.ErrRoyaltyTooHigh},
		{"soulbound royalty", func(f *fixture, m *types.MsgCreateClass) {
			m.Flags.Transferable = false
			m.RoyaltyBps = 10
		}, types.ErrInvalidFlags},
		{"soulbound priced mint", func(f *fixture, m *types.MsgCreateClass) {
			m.Flags.Transferable = false
			m.MintPolicy.Price = spark(1)
		}, types.ErrInvalidFlags},
		{"dream price", func(f *fixture, m *types.MsgCreateClass) {
			m.MintPolicy.Price.Denom = dream
			m.MintPolicy.Price.Amount = math.NewInt(1)
		}, types.ErrDreamNotAccepted},
		{"other denom", func(f *fixture, m *types.MsgCreateClass) {
			m.MintPolicy.Price.Denom = "uatom"
			m.MintPolicy.Price.Amount = math.NewInt(1)
		}, types.ErrInvalidDenom},
		{"empty name", func(f *fixture, m *types.MsgCreateClass) { m.Name = "" }, types.ErrInvalidMetadata},
		{"bad symbol", func(f *fixture, m *types.MsgCreateClass) { m.Symbol = "phx" }, types.ErrInvalidMetadata},
		{"data uri in description", func(f *fixture, m *types.MsgCreateClass) { m.Description = "see data:image/png;base64,AAAA" }, types.ErrInlineData},
		{"http uri", func(f *fixture, m *types.MsgCreateClass) { m.Uri = "http://example.org/x.png" }, types.ErrInvalidMetadata},
		{"bad uri hash", func(f *fixture, m *types.MsgCreateClass) { m.UriHash = "ABC" }, types.ErrInvalidMetadata},
		{"blocked payout", func(f *fixture, m *types.MsgCreateClass) { m.PayoutAddress = types.ModuleAddressString() }, types.ErrInvalidRecipient},
		{"owner as minter", func(f *fixture, m *types.MsgCreateClass) { m.Minters = []string{f.alice.String()} }, types.ErrInvalidBatch},
		{"window inverted", func(f *fixture, m *types.MsgCreateClass) { m.MintPolicy.StartTime = 10; m.MintPolicy.EndTime = 5 }, types.ErrMintWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			m := base(f)
			tc.mut(f, m)
			_, err := f.msg.CreateClass(f.ctx, m)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestCreateClassTrustLevelAndCouncil(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.MinTrustLevelCreateClass = 2 }) // ESTABLISHED
	_, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{
		Creator: f.carol.String(), Name: "Aurora", Flags: types.ClassFlags{Transferable: true},
		AcceptedContentLicense: types.ContentLicense,
	})
	require.ErrorIs(t, err, types.ErrInsufficientTrust)

	// A council body may create classes without membership.
	f.createClass(f.council, func(m *types.MsgCreateClass) { m.Flags = types.ClassFlags{BurnAuthorization: types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER}; m.RoyaltyBps = 0 })
}

func TestCreateClassPerCreatorCap(t *testing.T) {
	f := initFixture(t)
	f.setParams(func(p *types.Params) { p.MaxClassesPerCreator = 1 })
	id := f.createClass(f.alice)
	_, err := f.msg.CreateClass(f.ctx, &types.MsgCreateClass{
		Creator: f.alice.String(), Name: "Second", Flags: types.ClassFlags{Transferable: true}, AcceptedContentLicense: types.ContentLicense,
	})
	require.ErrorIs(t, err, types.ErrTooManyClasses)

	// Deleting the empty class frees the slot.
	_, err = f.msg.DeleteClass(f.ctx, &types.MsgDeleteClass{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	f.createClass(f.alice)
}

func TestUpdateClassAndFreeze(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)

	_, err := f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.bob.String(), ClassId: id, Name: "x", UpdateMask: []string{"name"}})
	require.ErrorIs(t, err, types.ErrNotClassOwner)

	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, Name: "Renamed", UpdateMask: []string{"name"}})
	require.NoError(t, err)
	require.Equal(t, "Renamed", f.class(id).Name)
	require.Equal(t, "Aurora studies", f.class(id).Description, "unmasked fields unchanged")

	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, UpdateMask: []string{"symbol"}})
	require.ErrorIs(t, err, types.ErrInvalidMetadata)

	// Royalty can only go down.
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, RoyaltyBps: 600, UpdateMask: []string{"royalty_bps"}})
	require.ErrorIs(t, err, types.ErrRoyaltyIncrease)
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, RoyaltyBps: 100, UpdateMask: []string{"royalty_bps"}})
	require.NoError(t, err)
	require.Equal(t, uint32(100), f.class(id).RoyaltyBps)

	_, err = f.msg.FreezeClassMetadata(f.ctx, &types.MsgFreezeClassMetadata{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	_, err = f.msg.FreezeClassMetadata(f.ctx, &types.MsgFreezeClassMetadata{Owner: f.alice.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrMetadataFrozen)

	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, Name: "Again", UpdateMask: []string{"name"}})
	require.ErrorIs(t, err, types.ErrMetadataFrozen)
	// Payment routing stays editable after a freeze.
	_, err = f.msg.UpdateClass(f.ctx, &types.MsgUpdateClass{Owner: f.alice.String(), ClassId: id, PayoutAddress: f.bob.String(), UpdateMask: []string{"payout_address"}})
	require.NoError(t, err)
	require.Equal(t, f.bob.String(), f.class(id).PayoutAddress)
}

func TestSetMaxSupplyRatchet(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 3)

	set := func(n uint64) error {
		_, err := f.msg.SetMaxSupply(f.ctx, &types.MsgSetMaxSupply{Owner: f.alice.String(), ClassId: id, MaxSupply: n})
		return err
	}
	require.ErrorIs(t, set(0), types.ErrSupplyIncrease)
	require.ErrorIs(t, set(2), types.ErrSupplyExceeded) // below issued
	require.NoError(t, set(10))                         // unlimited -> capped
	require.ErrorIs(t, set(10), types.ErrSupplyIncrease)
	require.ErrorIs(t, set(11), types.ErrSupplyIncrease)
	require.NoError(t, set(5))

	// Burned tokens still count toward the cap.
	_, err := f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	f.mint(f.alice, id, f.alice, 2)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id,
		Entries: []types.MintEntry{{Metadata: meta("over")}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrSupplyExceeded)
	f.checkInvariants()
}

func TestCloseMinting(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	_, err := f.msg.CloseMinting(f.ctx, &types.MsgCloseMinting{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	_, err = f.msg.CloseMinting(f.ctx, &types.MsgCloseMinting{Owner: f.alice.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrMintingClosed)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.alice.String(), ClassId: id,
		Entries: []types.MintEntry{{Metadata: meta("x")}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrMintingClosed)
	_, err = f.msg.SetMintPolicy(f.ctx, &types.MsgSetMintPolicy{Owner: f.alice.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrMintingClosed)
	_, err = f.msg.SetMaxSupply(f.ctx, &types.MsgSetMaxSupply{Owner: f.alice.String(), ClassId: id, MaxSupply: 1})
	require.ErrorIs(t, err, types.ErrMintingClosed)
}

func TestSetMinters(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	_, err := f.msg.SetMinters(f.ctx, &types.MsgSetMinters{Owner: f.alice.String(), ClassId: id, Add: []string{f.bob.String(), f.bob.String()}})
	require.NoError(t, err)
	require.Equal(t, []string{f.bob.String()}, f.class(id).Minters)

	f.mint(f.bob, id, f.bob, 1)
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.carol.String(), ClassId: id,
		Entries: []types.MintEntry{{Metadata: meta("x")}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrNotMinter)

	_, err = f.msg.SetMinters(f.ctx, &types.MsgSetMinters{Owner: f.alice.String(), ClassId: id, Remove: []string{f.bob.String()}})
	require.NoError(t, err)
	require.Empty(t, f.class(id).Minters)

	f.setParams(func(p *types.Params) { p.MaxMintersPerClass = 1 })
	_, err = f.msg.SetMinters(f.ctx, &types.MsgSetMinters{Owner: f.alice.String(), ClassId: id, Add: []string{f.bob.String(), f.carol.String()}})
	require.ErrorIs(t, err, types.ErrInvalidBatch)
}

func TestClassHandover(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) {
		m.Minters = []string{f.carol.String()}
		m.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: spark(1)}
	})

	_, err := f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.alice.String()})
	require.ErrorIs(t, err, types.ErrInvalidRecipient)
	_, err = f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(t, err)

	// Only the proposed owner can accept.
	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.carol.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrPendingOwnerNotFound)

	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.bob.String(), ClassId: id})
	require.NoError(t, err)
	c := f.class(id)
	require.Equal(t, f.bob.String(), c.Owner)
	require.Equal(t, f.alice.String(), c.Creator)
	require.Empty(t, c.Minters, "minters cleared on handover")
	require.False(t, c.MintPolicy.PublicMintEnabled, "public mint disabled on handover")
	require.Equal(t, f.bob.String(), c.PayoutAddress)
	require.Equal(t, types.SoleRoyaltyRecipient(f.bob.String()), c.RoyaltyRecipients)

	resp, err := f.query.ClassesByOwner(f.ctx, &types.QueryClassesByOwnerRequest{Owner: f.bob.String()})
	require.NoError(t, err)
	require.Len(t, resp.Classes, 1)
	resp, err = f.query.ClassesByOwner(f.ctx, &types.QueryClassesByOwnerRequest{Owner: f.alice.String()})
	require.NoError(t, err)
	require.Empty(t, resp.Classes)

	// The old owner's former minter can no longer mint.
	_, err = f.msg.Mint(f.ctx, &types.MsgMint{Minter: f.carol.String(), ClassId: id,
		Entries: []types.MintEntry{{Metadata: meta("x")}}, AcceptedContentLicense: types.ContentLicense})
	require.ErrorIs(t, err, types.ErrNotMinter)
}

func TestClassHandoverExpiryAndCancel(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	_, err := f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(t, err)
	_, err = f.msg.CancelClassOwner(f.ctx, &types.MsgCancelClassOwner{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.bob.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrPendingOwnerNotFound)

	_, err = f.msg.ProposeClassOwner(f.ctx, &types.MsgProposeClassOwner{Owner: f.alice.String(), ClassId: id, ProposedOwner: f.bob.String()})
	require.NoError(t, err)
	f.advance(f.params().PendingTtl, 1)
	// Expired before the EndBlocker drains it: still refused.
	_, err = f.msg.AcceptClassOwner(f.ctx, &types.MsgAcceptClassOwner{ProposedOwner: f.bob.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrPendingOwnerNotFound)
	f.endBlock()
	has, err := f.keeper.PendingClassOwners.Has(f.ctx, id)
	require.NoError(t, err)
	require.False(t, has)
}

func TestDeleteClass(t *testing.T) {
	f := initFixture(t)
	id := f.createClass(f.alice)
	f.mint(f.alice, id, f.alice, 1)
	_, err := f.msg.DeleteClass(f.ctx, &types.MsgDeleteClass{Owner: f.alice.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrClassNotEmpty)

	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.alice.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: 1}}})
	require.NoError(t, err)
	_, err = f.msg.DeleteClass(f.ctx, &types.MsgDeleteClass{Owner: f.bob.String(), ClassId: id})
	require.ErrorIs(t, err, types.ErrNotClassOwner)
	_, err = f.msg.DeleteClass(f.ctx, &types.MsgDeleteClass{Owner: f.alice.String(), ClassId: id})
	require.NoError(t, err)
	_, found, err := f.keeper.GetClass(f.ctx, id)
	require.NoError(t, err)
	require.False(t, found)
	// Ids are never reused.
	require.Equal(t, id+1, f.createClass(f.alice))
	f.checkInvariants()
}

package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/artifact/types"
)

// sessionCtx is the context x/session's MsgExecSession hands an inner
// message signed by granter's delegated key.
func (f *fixture) sessionCtx(granter sdk.AccAddress) sdk.Context {
	return f.ctx.WithValue(commontypes.ContextKeySessionExec, granter.String())
}

// TestSessionMintDepositRefundsGranter (SES-101 / ART-103): a leaked
// game-server session key mints the granter's tokens and units to an
// address it controls and burns them. The deposits must go back to the
// granter, never to the burner; a mint signed by the granter's own key
// keeps the burner refund.
func TestSessionMintDepositRefundsGranter(t *testing.T) {
	f := initFixture(t)
	dep := f.params().TokenDeposit
	id := f.createClass(f.alice)

	// Direct delivery through a session key (alice is a member, so dave's
	// default MEMBERS policy delivers directly).
	resp, err := f.msg.Issue(f.sessionCtx(f.alice), &types.MsgIssue{
		Minter: f.alice.String(), ClassId: id, Metadata: meta("loot"),
		Recipients: []string{f.dave.String()}, AcceptedContentLicense: types.ContentLicense,
	})
	require.NoError(t, err)
	more, err := f.msg.Mint(f.sessionCtx(f.alice), &types.MsgMint{
		Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.dave.String(), Metadata: meta("a")}, {Recipient: f.dave.String(), Metadata: meta("b")}},
	})
	require.NoError(t, err)
	resp.Results = append(resp.Results, more.Results...)
	require.Len(t, resp.Results, 3)
	for _, r := range resp.Results {
		require.Equal(t, f.alice.String(), f.token(id, r.TokenId).DepositPayer)
	}
	alice0, dave0 := f.bank.bal(f.alice), f.bank.bal(f.dave)
	refs := make([]types.TokenRef, 0, len(resp.Results))
	for _, r := range resp.Results {
		refs = append(refs, types.TokenRef{ClassId: id, TokenId: r.TokenId})
	}
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.dave.String(), Refs: refs})
	require.NoError(t, err)
	require.Equal(t, dave0, f.bank.bal(f.dave), "the burner collects nothing")
	require.Equal(t, alice0.Add(dep.MulRaw(3)), f.bank.bal(f.alice), "the granter gets its deposits back")

	// Inbox delivery: the payer is captured at mint and carried to the
	// token on acceptance (even when the recipient accepts through its own
	// session key).
	_, err = f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	mresp, err := f.msg.Mint(f.sessionCtx(f.alice), &types.MsgMint{
		Minter: f.alice.String(), ClassId: id, AcceptedContentLicense: types.ContentLicense,
		Entries: []types.MintEntry{{Recipient: f.dave.String(), Metadata: meta("inboxed")}},
	})
	require.NoError(t, err)
	tid := mresp.Results[0].TokenId
	require.False(t, mresp.Results[0].Live)
	_, err = f.msg.AcceptIncoming(f.sessionCtx(f.dave), &types.MsgAcceptIncoming{Recipient: f.dave.String(),
		Entries: []types.AcceptEntry{{ClassId: id, TokenId: tid}}})
	require.NoError(t, err)
	require.Equal(t, f.alice.String(), f.token(id, tid).DepositPayer)

	// Genesis carries the payer.
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, exported.Validate())
	g := initFixture(t)
	g.bank.balances[moduleAddr(types.ModuleName)] = f.bank.balances[moduleAddr(types.ModuleName)]
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	require.Equal(t, f.alice.String(), g.token(id, tid).DepositPayer)
	bad := *exported
	bad.Tokens = append([]types.Token(nil), exported.Tokens...)
	for i := range bad.Tokens {
		if bad.Tokens[i].DepositPayer != "" {
			bad.Tokens[i].DepositPayer = "not-an-address"
		}
	}
	require.ErrorContains(t, bad.Validate(), "deposit_payer")

	alice0, dave0 = f.bank.bal(f.alice), f.bank.bal(f.dave)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.dave.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: tid}}})
	require.NoError(t, err)
	require.Equal(t, dave0, f.bank.bal(f.dave))
	require.Equal(t, alice0.Add(dep), f.bank.bal(f.alice))

	// A mint signed by the granter's own key keeps the burner refund.
	_, err = f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_MEMBERS})
	require.NoError(t, err)
	own := f.mint(f.alice, id, f.dave, 1)
	require.Empty(t, f.token(id, own[0].TokenId).DepositPayer)
	dave0 = f.bank.bal(f.dave)
	_, err = f.msg.Burn(f.ctx, &types.MsgBurn{Owner: f.dave.String(), Refs: []types.TokenRef{{ClassId: id, TokenId: own[0].TokenId}}})
	require.NoError(t, err)
	require.Equal(t, dave0.Add(dep), f.bank.bal(f.dave))
	f.checkInvariants()
}

// TestSessionMintUnitsDepositRefundsGranter is the editions sibling: the
// record deposit of a session-minted balance goes back to the granter when
// the holder empties the record.
func TestSessionMintUnitsDepositRefundsGranter(t *testing.T) {
	f := initFixture(t)
	dep := f.params().TokenDeposit
	classID := f.createEditionsClass(f.alice)
	editionID := f.createEdition(f.alice, classID, 0, types.ImpactClaim{})

	_, err := f.msg.MintUnits(f.sessionCtx(f.alice), &types.MsgMintUnits{Minter: f.alice.String(), ClassId: classID, EditionId: editionID,
		Entries: []types.UnitMintEntry{{Recipient: f.dave.String(), Amount: 5}}})
	require.NoError(t, err)
	require.Equal(t, f.alice.String(), f.balanceOf(classID, editionID, f.dave).DepositPayer)

	alice0, dave0 := f.bank.bal(f.alice), f.bank.bal(f.dave)
	_, err = f.msg.BurnUnits(f.ctx, &types.MsgBurnUnits{Owner: f.dave.String(), ClassId: classID, EditionId: editionID, Amount: 5})
	require.NoError(t, err)
	require.Equal(t, dave0, f.bank.bal(f.dave), "emptying the record pays the holder nothing")
	require.Equal(t, alice0.Add(dep), f.bank.bal(f.alice))

	// Through the inbox: the payer rides on the pending item.
	_, err = f.msg.SetReceivePolicy(f.ctx, &types.MsgSetReceivePolicy{Owner: f.dave.String(), Policy: types.ReceivePolicy_RECEIVE_POLICY_INBOX})
	require.NoError(t, err)
	resp, err := f.msg.MintUnits(f.sessionCtx(f.alice), &types.MsgMintUnits{Minter: f.alice.String(), ClassId: classID, EditionId: editionID,
		Entries: []types.UnitMintEntry{{Recipient: f.dave.String(), Amount: 2}}})
	require.NoError(t, err)
	pid := resp.Results[0].PendingId
	require.NotZero(t, pid)
	_, err = f.msg.AcceptIncoming(f.ctx, &types.MsgAcceptIncoming{Recipient: f.dave.String(), Entries: []types.AcceptEntry{{ClassId: 0, TokenId: pid}}})
	require.NoError(t, err)
	require.Equal(t, f.alice.String(), f.balanceOf(classID, editionID, f.dave).DepositPayer)
	alice0 = f.bank.bal(f.alice)
	_, err = f.msg.BurnUnits(f.ctx, &types.MsgBurnUnits{Owner: f.dave.String(), ClassId: classID, EditionId: editionID, Amount: 2})
	require.NoError(t, err)
	require.Equal(t, alice0.Add(dep), f.bank.bal(f.alice))
	require.True(t, f.bank.bal(f.dave).Equal(dave0), "dave never gains: %s vs %s", f.bank.bal(f.dave), dave0)
	f.checkInvariants()
}

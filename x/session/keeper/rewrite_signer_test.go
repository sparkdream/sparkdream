package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/stretchr/testify/require"

	artifacttypes "sparkdream/x/artifact/types"
	blogtypes "sparkdream/x/blog/types"
	collecttypes "sparkdream/x/collect/types"
	federationtypes "sparkdream/x/federation/types"
	forumtypes "sparkdream/x/forum/types"
	nametypes "sparkdream/x/name/types"
	seasontypes "sparkdream/x/season/types"
	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// The allowlisted x/artifact messages name their signer "minter" and
// "owner": ExecSession must be able to rewrite both to the granter, or a
// session key could mint and edit as itself rather than for the granter.
func TestRewriteSignerFieldArtifact(t *testing.T) {
	mint := &artifacttypes.MsgMint{Minter: "grantee", ClassId: 7}
	require.NoError(t, keeper.RewriteSignerField(mint, "granter"))
	require.Equal(t, "granter", mint.Minter)

	update := &artifacttypes.MsgUpdateToken{Owner: "grantee", ClassId: 7, TokenId: 1}
	require.NoError(t, keeper.RewriteSignerField(update, "granter"))
	require.Equal(t, "granter", update.Owner)
}

// The six artifact messages whose signer field is not one of the old guess
// list's names (recipient, holder, seller, bidder) now rewrite, because the
// field is read from the proto signer option (SES-05).
func TestRewriteSignerFieldFromProtoOption(t *testing.T) {
	cases := []struct {
		msg sdk.Msg
		get func() string
	}{}
	acc := &artifacttypes.MsgAcceptIncoming{Recipient: "grantee"}
	rej := &artifacttypes.MsgRejectIncoming{Recipient: "grantee"}
	claim := &artifacttypes.MsgClaimEditionRewards{Holder: "grantee"}
	delist := &artifacttypes.MsgDelist{Seller: "grantee"}
	delistU := &artifacttypes.MsgDelistUnits{Seller: "grantee"}
	cancel := &artifacttypes.MsgCancelOffer{Bidder: "grantee"}
	cases = append(cases,
		struct {
			msg sdk.Msg
			get func() string
		}{acc, func() string { return acc.Recipient }},
		struct {
			msg sdk.Msg
			get func() string
		}{rej, func() string { return rej.Recipient }},
		struct {
			msg sdk.Msg
			get func() string
		}{claim, func() string { return claim.Holder }},
		struct {
			msg sdk.Msg
			get func() string
		}{delist, func() string { return delist.Seller }},
		struct {
			msg sdk.Msg
			get func() string
		}{delistU, func() string { return delistU.Seller }},
		struct {
			msg sdk.Msg
			get func() string
		}{cancel, func() string { return cancel.Bidder }},
	)
	for _, c := range cases {
		require.NoError(t, keeper.RewriteSignerField(c.msg, "granter"), "%T", c.msg)
		require.Equal(t, "granter", c.get(), "%T", c.msg)
	}
}

// Every DefaultAllowedMsgTypes URL resolves to a registered message and its
// signer rewrite yields exactly the granter as the derived signer. Catches
// dead allowlist URLs and messages the rewrite cannot handle (SES-05).
func TestDefaultAllowlistResolvesAndRewrites(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig()
	reg := encCfg.InterfaceRegistry
	artifacttypes.RegisterInterfaces(reg)
	blogtypes.RegisterInterfaces(reg)
	collecttypes.RegisterInterfaces(reg)
	federationtypes.RegisterInterfaces(reg)
	forumtypes.RegisterInterfaces(reg)
	nametypes.RegisterInterfaces(reg)
	seasontypes.RegisterInterfaces(reg)
	servicetypes.RegisterInterfaces(reg)
	cdc := encCfg.Codec

	granterBz := sdk.AccAddress([]byte("granter_____________"))
	granter := granterBz.String()
	for _, url := range types.DefaultAllowedMsgTypes {
		m, err := reg.Resolve(url)
		require.NoError(t, err, "allowlisted %s is not a registered message", url)
		msg := m
		require.NoError(t, keeper.RewriteAndCheckSigner(cdc, msg, granter, granterBz), url)
	}
}

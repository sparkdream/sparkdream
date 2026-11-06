package app

import (
	"strings"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/require"

	commonstypes "sparkdream/x/commons/types"
	vaulttypes "sparkdream/x/vault/types"
)

// CMN-12: a commons MsgSubmitProposal carried by an x/vault proposal never
// passes the ProposalFeeDecorator, so the commons handler charges the
// proposal fee itself, from the vault (the inner message's signer and
// proposer) to the fee collector. The vault must hold the fee: an unfunded
// vault's submission is refused and nothing is stored.
func TestVaultInnerProposalPaysFee(t *testing.T) {
	var genFile string
	for _, f := range snapshotGenesisFiles(t) {
		if strings.Contains(f, "/commons/") {
			genFile = f
		}
	}
	if genFile == "" {
		t.Skip("no x/commons e2e snapshot; run /save-setup commons")
	}
	app, ctx, err := newAppFromGenesisFile(t, genFile)
	if err != nil {
		t.Skipf("snapshot %s does not import (%v); re-run /save-setup", genFile, err)
	}

	fee, err := app.CommonsKeeper.ProposalFee(ctx)
	require.NoError(t, err)
	require.False(t, fee.IsZero(), "the snapshot charges a proposal fee")
	denom := fee[0].Denom

	// A 1-of-1 vault owned by a fresh account that pays the creation fee.
	owner := sdk.AccAddress([]byte("vault-fee-owner_____"))
	app.AuthKeeper.SetAccount(ctx, app.AuthKeeper.NewAccountWithAddress(ctx, owner))
	fund := func(to sdk.AccAddress, coins sdk.Coins) {
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, to, coins))
	}
	vaultParams, err := app.VaultKeeper.Params.Get(ctx)
	require.NoError(t, err)
	fund(owner, sdk.NewCoins(sdk.NewCoin(denom, vaultParams.CreationFeeAmount.AddRaw(1_000_000))))
	v, err := app.VaultKeeper.CreateVault(ctx, owner,
		[]vaulttypes.VaultSigner{{Address: owner.String(), Weight: 1}}, 1, vaulttypes.MinProposalTTL, "fee vault")
	require.NoError(t, err)
	vaultAddr, err := sdk.AccAddressFromBech32(v.Address)
	require.NoError(t, err)

	// The vault sits on the Commons Council, so it may propose there.
	const council = "Commons Council"
	group, err := app.CommonsKeeper.Groups.Get(ctx, council)
	require.NoError(t, err)
	require.NoError(t, app.CommonsKeeper.AddMember(ctx, council, commonstypes.Member{Address: v.Address, Weight: "1"}))

	submit, err := codectypes.NewAnyWithValue(&commonstypes.MsgSubmitProposal{
		Proposer: v.Address, PolicyAddress: group.PolicyAddress, Metadata: "paid from the vault",
	})
	require.NoError(t, err)
	collector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)

	// Unfunded vault: the submission is refused with the typed commons
	// error, so the vault proposal is not executed.
	p, err := app.VaultKeeper.Propose(ctx, owner.String(), v.VaultId, []*codectypes.Any{submit}, "")
	require.NoError(t, err)
	err = app.VaultKeeper.Execute(ctx, owner.String(), p.ProposalId)
	require.ErrorIs(t, err, commonstypes.ErrInsufficientProposalFee)

	// Funded vault: executed, and exactly one fee moved vault -> fee collector.
	fund(vaultAddr, fee)
	vaultBefore := app.BankKeeper.GetBalance(ctx, vaultAddr, denom)
	collectorBefore := app.BankKeeper.GetBalance(ctx, collector, denom)
	nextID, err := app.CommonsKeeper.ProposalSeq.Peek(ctx)
	require.NoError(t, err)
	require.NoError(t, app.VaultKeeper.Execute(ctx, owner.String(), p.ProposalId))

	require.Equal(t, vaultBefore.Sub(fee[0]).String(), app.BankKeeper.GetBalance(ctx, vaultAddr, denom).String(), "the vault paid the proposal fee")
	require.Equal(t, collectorBefore.Add(fee[0]).String(), app.BankKeeper.GetBalance(ctx, collector, denom).String(), "the fee collector received it")
	proposal, err := app.CommonsKeeper.Proposals.Get(ctx, nextID)
	require.NoError(t, err)
	require.Equal(t, v.Address, proposal.Proposer)
	require.Equal(t, council, proposal.CouncilName)
	require.Equal(t, commonstypes.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, proposal.Status)
}

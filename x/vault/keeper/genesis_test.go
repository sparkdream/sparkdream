package keeper_test

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vault/types"
)

func genesisVault(id uint64) types.Vault {
	return types.Vault{
		VaultId: id,
		Address: types.VaultAddress(id).String(),
		Signers: []types.VaultSigner{
			{Address: addr("phoenix"), Weight: 1, InviteExpiresAt: 1_700_100_000},
			{Address: addr("aurora"), Weight: 1, InviteExpiresAt: 1_700_100_000},
		},
		Threshold:     2,
		ConfigVersion: 1,
		ProposalTtl:   types.MinProposalTTL,
	}
}

// genesisProposal builds a PENDING proposal of vault vaultID carrying msgs,
// expiring an hour after the fixture's block time.
func genesisProposal(id, vaultID uint64, msgs ...*codectypes.Any) types.VaultProposal {
	return types.VaultProposal{
		ProposalId: id, VaultId: vaultID, Proposer: addr("phoenix"),
		Messages: msgs, MessagesHash: types.MessagesHash(msgs), ConfigVersion: 1,
		Approvals: []string{addr("phoenix")}, Status: types.ProposalStatus_PROPOSAL_STATUS_PENDING,
		ExpiresAt: 1_700_003_600,
	}
}

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:   types.DefaultParams(),
		VaultMap: []types.Vault{genesisVault(1), genesisVault(2)},
		VaultProposalMap: []types.VaultProposal{
			genesisProposal(1, 1, anySend(t, types.VaultAddress(1).String(), sink, 1)),
			genesisProposal(2, 2, anySend(t, types.VaultAddress(2).String(), sink, 1)),
		},
	}
	genesisState.VaultProposalMap[1].Status = types.ProposalStatus_PROPOSAL_STATUS_EXECUTED
	genesisState.VaultProposalMap[1].ResolvedAt = 1_699_999_000

	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.EqualExportedValues(t, genesisState.VaultMap, got.VaultMap)
	require.EqualExportedValues(t, genesisState.VaultProposalMap, got.VaultProposalMap)
}

// TestInitGenesisRefusesForeignVaultAddress: a genesis must never register an
// arbitrary account as a vault (proposals would execute as that account), nor
// a vault whose threshold exceeds its weight (permanently locked).
func TestInitGenesisRefusesForeignVaultAddress(t *testing.T) {
	f := initFixture(t)
	v := genesisVault(1)
	v.Address = addr("zenith")
	require.Error(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Params: types.DefaultParams(), VaultMap: []types.Vault{v}}))

	f = initFixture(t)
	v = genesisVault(1)
	v.Threshold = 5
	require.Error(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{Params: types.DefaultParams(), VaultMap: []types.Vault{v}}))

	f = initFixture(t)
	require.Error(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{
		Params:           types.DefaultParams(),
		VaultMap:         []types.Vault{genesisVault(1)},
		VaultProposalMap: []types.VaultProposal{genesisProposal(1, 2, anySend(t, types.VaultAddress(2).String(), sink, 1))},
	}))
}

// VLT-09: a PENDING genesis proposal must be one Propose could have written.
// Its inner messages pass the denylist and are signed by the vault, and its
// expiry is within MaxProposalTTL of genesis.
func TestInitGenesisRefusesMalformedPendingProposal(t *testing.T) {
	v1 := types.VaultAddress(1).String()
	cases := map[string]types.VaultProposal{
		"denylisted inner msg":          genesisProposal(1, 1, anyMsg(t, &types.MsgCreateVault{Creator: v1})),
		"inner signer is not the vault": genesisProposal(1, 1, anySend(t, alice, sink, 1)),
	}
	far := genesisProposal(1, 1, anySend(t, v1, sink, 1))
	far.ExpiresAt = 1_700_000_000 + types.MaxProposalTTL + 1
	cases["expiry beyond MaxProposalTTL"] = far
	stale := genesisProposal(1, 1, anySend(t, v1, sink, 1))
	stale.MessagesHash = types.MessagesHash([]*codectypes.Any{anySend(t, v1, sink, 2)})
	cases["messages_hash of another batch"] = stale

	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			f := initFixture(t)
			err := f.keeper.InitGenesis(f.ctx, types.GenesisState{
				Params: types.DefaultParams(), VaultMap: []types.Vault{genesisVault(1)}, VaultProposalMap: []types.VaultProposal{p},
			})
			require.Error(t, err)
		})
	}

	// The same far-future expiry is fine on a resolved record (it is never
	// queued for expiry), and the valid baseline imports.
	f := initFixture(t)
	far.Status = types.ProposalStatus_PROPOSAL_STATUS_CANCELLED
	far.ResolvedAt = 1_699_999_000
	ok := genesisProposal(2, 1, anySend(t, v1, sink, 1))
	require.NoError(t, f.keeper.InitGenesis(f.ctx, types.GenesisState{
		Params: types.DefaultParams(), VaultMap: []types.Vault{genesisVault(1)}, VaultProposalMap: []types.VaultProposal{far, ok},
	}))
}

// TestGenesisKeepsSequencesPastPrunedIDs: proposals are pruned after their
// retention, so the sequence can run ahead of every live id. The exported
// sequences survive an import; a re-import never hands out a used id.
func TestGenesisKeepsSequencesPastPrunedIDs(t *testing.T) {
	genesisState := types.GenesisState{
		Params:           types.DefaultParams(),
		VaultMap:         []types.Vault{genesisVault(1)},
		VaultProposalMap: []types.VaultProposal{genesisProposal(2, 1, anySend(t, types.VaultAddress(1).String(), sink, 1))},
		VaultSeq:         4,
		ProposalSeq:      9, // ids 3..9 were pruned
	}
	f := initFixture(t)
	require.NoError(t, f.keeper.InitGenesis(f.ctx, genesisState))
	pseq, err := f.keeper.ProposalSeq.Peek(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(9), pseq)
	vseq, err := f.keeper.VaultSeq.Peek(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), vseq)

	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(9), got.ProposalSeq)
	require.Equal(t, uint64(4), got.VaultSeq)

	// A genesis without sequences (older export) falls back to the max id.
	f2 := initFixture(t)
	genesisState.VaultSeq, genesisState.ProposalSeq = 0, 0
	require.NoError(t, f2.keeper.InitGenesis(f2.ctx, genesisState))
	pseq, err = f2.keeper.ProposalSeq.Peek(f2.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), pseq)
}

package types_test

import (
	"math"
	"strings"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vault/types"
)

func signerAddr(name string) string {
	b := make([]byte, 20)
	copy(b, name)
	return sdk.AccAddress(b).String()
}

// genInviteExpiry is the invite_expires_at of the fixtures' unaccepted
// entries (every non-sole unaccepted entry carries one, VLT-205).
const genInviteExpiry = int64(1_700_100_000)

// genVault builds a vault that passes every on-chain config rule.
func genVault(id uint64) types.Vault {
	return types.Vault{
		VaultId: id,
		Address: types.VaultAddress(id).String(),
		Signers: []types.VaultSigner{
			{Address: signerAddr("phoenix"), Weight: 1, InviteExpiresAt: genInviteExpiry},
			{Address: signerAddr("aurora"), Weight: 1, InviteExpiresAt: genInviteExpiry},
		},
		Threshold:     2,
		ConfigVersion: 1,
		ProposalTtl:   types.MinProposalTTL,
	}
}

// genProposal builds a PENDING proposal that passes every genesis rule.
func genProposal(t *testing.T, id, vaultID uint64) types.VaultProposal {
	t.Helper()
	a, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		FromAddress: types.VaultAddress(vaultID).String(), ToAddress: signerAddr("zenith"),
		Amount: sdk.NewCoins(sdk.NewInt64Coin("uspark", 1)),
	})
	require.NoError(t, err)
	msgs := []*codectypes.Any{a}
	return types.VaultProposal{
		ProposalId: id, VaultId: vaultID, Proposer: signerAddr("phoenix"),
		Messages: msgs, MessagesHash: types.MessagesHash(msgs), ConfigVersion: 1,
		Approvals: []string{signerAddr("phoenix")}, Status: types.ProposalStatus_PROPOSAL_STATUS_PENDING,
		ExpiresAt: 1_700_003_600, Memo: "m",
	}
}

func TestGenesisState_Validate(t *testing.T) {
	withVaults := func(mut func(*types.GenesisState)) *types.GenesisState {
		gs := &types.GenesisState{
			Params:           types.DefaultParams(),
			VaultMap:         []types.Vault{genVault(1), genVault(2)},
			VaultProposalMap: []types.VaultProposal{genProposal(t, 1, 1), genProposal(t, 2, 2)},
		}
		gs.VaultProposalMap[1].Status = types.ProposalStatus_PROPOSAL_STATUS_EXECUTED
		gs.VaultProposalMap[1].ResolvedAt = 1_700_000_100
		if mut != nil {
			mut(gs)
		}
		return gs
	}
	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{desc: "default is valid", genState: types.DefaultGenesis(), valid: true},
		{desc: "valid genesis state", genState: withVaults(nil), valid: true},
		{desc: "vault_seq at the uint64 ceiling", genState: withVaults(func(gs *types.GenesisState) { gs.VaultSeq = math.MaxUint64 })},
		{desc: "proposal_seq at the uint64 ceiling", genState: withVaults(func(gs *types.GenesisState) { gs.ProposalSeq = math.MaxUint64 })},
		{desc: "duplicated vault", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[1] = genVault(1) })},
		{desc: "duplicated vaultProposal", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[1].ProposalId = 1 })},
		{desc: "vault id zero", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0] = genVault(0) })},
		{desc: "address not derived from id", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Address = signerAddr("zenith") })},
		{desc: "address of another vault", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Address = types.VaultAddress(2).String() })},
		{desc: "threshold above total weight", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Threshold = 3 })},
		{desc: "duplicate signer", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Signers[1].Address = signerAddr("phoenix") })},
		{desc: "ttl out of bounds", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].ProposalTtl = 0 })},
		{desc: "proposal of unknown vault", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].VaultId = 9 })},
		{desc: "proposal id zero", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].ProposalId = 0 })},
		{desc: "vault config_version zero", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].ConfigVersion = 0 })},
		// VLT-205: a non-sole unaccepted entry must carry an expiry (the
		// chain always stamps one); a sole one never does; an accepted one
		// never does either.
		{desc: "non-sole invitee without expiry", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Signers[1].InviteExpiresAt = 0 })},
		{desc: "accepted entry with expiry", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Signers[1].Accepted = true })},
		{desc: "negative invite expiry", genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Signers[1].InviteExpiresAt = -1 })},
		{desc: "sole invitee without expiry", valid: true, genState: withVaults(func(gs *types.GenesisState) {
			gs.VaultMap[0].Signers = gs.VaultMap[0].Signers[:1]
			gs.VaultMap[0].Signers[0].InviteExpiresAt = 0
			gs.VaultMap[0].Threshold = 1
		})},
		{desc: "accepted entries without expiry", valid: true, genState: withVaults(func(gs *types.GenesisState) {
			for i := range gs.VaultMap[0].Signers {
				gs.VaultMap[0].Signers[i].Accepted = true
				gs.VaultMap[0].Signers[i].InviteExpiresAt = 0
			}
		})},
		// VLT-203: a signer-less vault (its only invitees declined while it
		// held funds or proposal records) is exportable; its
		// signer-independent rules still apply.
		{desc: "signer-less vault", valid: true, genState: withVaults(func(gs *types.GenesisState) { gs.VaultMap[0].Signers = nil })},
		{desc: "signer-less vault with threshold zero", genState: withVaults(func(gs *types.GenesisState) {
			gs.VaultMap[0].Signers = nil
			gs.VaultMap[0].Threshold = 0
		})},
		{desc: "signer-less vault with ttl out of bounds", genState: withVaults(func(gs *types.GenesisState) {
			gs.VaultMap[0].Signers = nil
			gs.VaultMap[0].ProposalTtl = 0
		})},
		// VLT-09: proposal records are validated too.
		{desc: "proposal status unspecified", genState: withVaults(func(gs *types.GenesisState) {
			gs.VaultProposalMap[0].Status = types.ProposalStatus_PROPOSAL_STATUS_UNSPECIFIED
		})},
		{desc: "proposal status unknown", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].Status = 99 })},
		{desc: "messages_hash mismatch", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].MessagesHash = make([]byte, 32) })},
		{desc: "no messages", genState: withVaults(func(gs *types.GenesisState) {
			gs.VaultProposalMap[0].Messages = nil
			gs.VaultProposalMap[0].MessagesHash = types.MessagesHash(nil)
		})},
		{desc: "too many messages", genState: withVaults(func(gs *types.GenesisState) {
			p := &gs.VaultProposalMap[0]
			for len(p.Messages) <= types.MaxMessagesPerProposal {
				p.Messages = append(p.Messages, p.Messages[0])
			}
			p.MessagesHash = types.MessagesHash(p.Messages)
		})},
		{desc: "nil message", genState: withVaults(func(gs *types.GenesisState) {
			p := &gs.VaultProposalMap[0]
			p.Messages = append(p.Messages, nil)
			p.MessagesHash = types.MessagesHash(p.Messages)
		})},
		{desc: "invalid proposer", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].Proposer = "zenith" })},
		{desc: "invalid approval", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].Approvals = []string{"zenith"} })},
		{desc: "duplicate approval", genState: withVaults(func(gs *types.GenesisState) {
			gs.VaultProposalMap[0].Approvals = []string{signerAddr("phoenix"), signerAddr("phoenix")}
		})},
		{desc: "config_version zero", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].ConfigVersion = 0 })},
		{desc: "config_version ahead of vault", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].ConfigVersion = 2 })},
		{desc: "memo too long", genState: withVaults(func(gs *types.GenesisState) {
			gs.VaultProposalMap[0].Memo = strings.Repeat("m", types.MaxMemoLength+1)
		})},
		{desc: "expires_at zero", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].ExpiresAt = 0 })},
		{desc: "pending with resolved_at", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[0].ResolvedAt = 5 })},
		{desc: "resolved without resolved_at", genState: withVaults(func(gs *types.GenesisState) { gs.VaultProposalMap[1].ResolvedAt = 0 })},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

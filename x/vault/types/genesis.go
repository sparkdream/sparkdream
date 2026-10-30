package types

import (
	"bytes"
	"fmt"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:   DefaultParams(),
		VaultMap: []Vault{}, VaultProposalMap: []VaultProposal{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure. Beyond unique ids it re-runs the config rules every vault had to
// pass on chain (ValidateConfig), requires each vault's address to be the
// keyless VaultAddress(vault_id) (a genesis must never register a user's or a
// module's account as a vault) and requires every proposal to reference a
// vault in the same genesis and to be a record the chain could have written
// (VaultProposal.ValidateGenesis). InitGenesis sets each sequence to the
// larger of its exported value and the maximum imported id, so it can never
// fall behind an imported record nor reuse a pruned proposal's id.
func (gs GenesisState) Validate() error {
	vaults := make(map[uint64]uint64, len(gs.VaultMap)) // id -> config_version
	addrs := make(map[string]struct{}, len(gs.VaultMap))
	for _, elem := range gs.VaultMap {
		if _, ok := vaults[elem.VaultId]; ok {
			return fmt.Errorf("duplicated index for vault")
		}
		vaults[elem.VaultId] = elem.ConfigVersion
		if err := elem.ValidateGenesis(); err != nil {
			return err
		}
		if _, ok := addrs[elem.Address]; ok {
			return fmt.Errorf("vault %d: duplicated address %s", elem.VaultId, elem.Address)
		}
		addrs[elem.Address] = struct{}{}
	}
	proposals := make(map[uint64]struct{}, len(gs.VaultProposalMap))
	for _, elem := range gs.VaultProposalMap {
		if _, ok := proposals[elem.ProposalId]; ok {
			return fmt.Errorf("duplicated index for vaultProposal")
		}
		proposals[elem.ProposalId] = struct{}{}
		if elem.ProposalId == 0 {
			return fmt.Errorf("vaultProposal id must be positive")
		}
		cv, ok := vaults[elem.VaultId]
		if !ok {
			return fmt.Errorf("vaultProposal %d references unknown vault %d", elem.ProposalId, elem.VaultId)
		}
		if err := elem.ValidateGenesis(cv); err != nil {
			return fmt.Errorf("vaultProposal %d: %w", elem.ProposalId, err)
		}
	}

	return gs.Params.Validate()
}

// ValidateGenesis checks an imported vault: a positive id, the address
// derived from that id, and the config rules of MsgCreateVault /
// MsgUpdateVault. Personal-address and tombstone checks need x/rep state,
// which may not be imported yet, so they are not re-run at import.
func (v Vault) ValidateGenesis() error {
	if v.VaultId == 0 {
		return fmt.Errorf("vault id must be positive")
	}
	if v.ConfigVersion == 0 {
		return fmt.Errorf("vault %d: config_version must be positive", v.VaultId)
	}
	_, bz, err := bech32.DecodeAndConvert(v.Address)
	if err != nil {
		return fmt.Errorf("vault %d: invalid address %q: %w", v.VaultId, v.Address, err)
	}
	if !bytes.Equal(bz, VaultAddress(v.VaultId)) {
		return fmt.Errorf("vault %d: address %s is not the vault's derived address", v.VaultId, v.Address)
	}
	if err := ValidateConfig(v.Signers, v.Threshold, v.ProposalTtl, v.Label); err != nil {
		return fmt.Errorf("vault %d: %w", v.VaultId, err)
	}
	for _, s := range v.Signers {
		if _, _, err := bech32.DecodeAndConvert(s.Address); err != nil {
			return fmt.Errorf("vault %d: invalid signer %q: %w", v.VaultId, s.Address, err)
		}
	}
	return nil
}

// ValidateGenesis checks an imported proposal against the rules Propose,
// Approve and resolveProposal enforce on chain: a known non-UNSPECIFIED
// status, 1..MaxMessagesPerProposal non-empty messages whose MessagesHash is
// the stored messages_hash (Approve compares a client's hash with the stored
// one, so a mismatch would let signers approve one batch and execute
// another), a valid proposer, valid and distinct approvals, a config_version
// in [1, vaultConfigVersion], a memo within MaxMemoLength, a positive
// expires_at and a resolved_at set exactly when the proposal is resolved.
// Unpacking, the inner-message denylist and the vault-signer rule need the
// codec and are checked by InitGenesis for PENDING proposals; the expiry
// horizon needs the genesis time and is checked there as well.
func (p VaultProposal) ValidateGenesis(vaultConfigVersion uint64) error {
	if _, ok := ProposalStatus_name[int32(p.Status)]; !ok || p.Status == ProposalStatus_PROPOSAL_STATUS_UNSPECIFIED {
		return fmt.Errorf("invalid status %d", p.Status)
	}
	if len(p.Messages) == 0 || len(p.Messages) > MaxMessagesPerProposal {
		return fmt.Errorf("must carry 1..%d messages, got %d", MaxMessagesPerProposal, len(p.Messages))
	}
	for i, m := range p.Messages {
		if m == nil || m.TypeUrl == "" {
			return fmt.Errorf("message %d is empty", i)
		}
	}
	if !bytes.Equal(p.MessagesHash, MessagesHash(p.Messages)) {
		return fmt.Errorf("messages_hash does not match messages")
	}
	if _, _, err := bech32.DecodeAndConvert(p.Proposer); err != nil {
		return fmt.Errorf("invalid proposer %q: %w", p.Proposer, err)
	}
	seen := make(map[string]struct{}, len(p.Approvals))
	for _, a := range p.Approvals {
		if _, _, err := bech32.DecodeAndConvert(a); err != nil {
			return fmt.Errorf("invalid approval %q: %w", a, err)
		}
		if _, dup := seen[a]; dup {
			return fmt.Errorf("duplicate approval %s", a)
		}
		seen[a] = struct{}{}
	}
	if len(p.Approvals) > MaxSigners {
		return fmt.Errorf("at most %d approvals, got %d", MaxSigners, len(p.Approvals))
	}
	if p.ConfigVersion == 0 || p.ConfigVersion > vaultConfigVersion {
		return fmt.Errorf("config_version %d outside [1, %d]", p.ConfigVersion, vaultConfigVersion)
	}
	if len(p.Memo) > MaxMemoLength {
		return fmt.Errorf("memo longer than %d chars", MaxMemoLength)
	}
	if p.ExpiresAt <= 0 {
		return fmt.Errorf("expires_at must be positive")
	}
	if p.IsResolved() {
		if p.ResolvedAt <= 0 {
			return fmt.Errorf("resolved proposal needs a positive resolved_at")
		}
	} else if p.ResolvedAt != 0 {
		return fmt.Errorf("pending proposal has resolved_at %d", p.ResolvedAt)
	}
	return nil
}

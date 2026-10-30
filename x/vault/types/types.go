package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	commontypes "sparkdream/x/common/types"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"
)

// Compiled bounds. They bound gas (every pass over a vault's signers,
// approvals, pending proposals or messages is capped by them), so they are
// constants rather than params. MinProposalTTL and ProposalRetention are
// build-tagged (bounds_vals_*.go): the network builds use an hour and seven
// days, the test build seconds so the e2e suite can watch expiry and pruning.
const (
	MaxSigners                                  = 10
	MaxSignerWeight                      uint64 = 1000
	MaxMessagesPerProposal                      = 16
	MaxPendingProposalsPerProposer              = 8
	MaxPendingUpdateProposalsPerProposer        = 2
	MaxProposalTTL                              = int64(30 * 24 * time.Hour / time.Second)
	MaxLabelLength                              = 64
	MaxMemoLength                               = 256
	// MaxVaultsPerSigner caps how many distinct vaults one member (any of
	// its x/rep addresses) may be an accepted signer of. Only accepted entries
	// count: an unaccepted listing (made without the member's consent) never
	// consumes the cap, so nobody can fill it to keep a member out of real
	// vaults. The cap bounds the vaults-by-signer index of an address and the
	// migration-hook walk.
	MaxVaultsPerSigner = 20
	// MaxEndBlockerPassItems caps each EndBlocker pass (expiry, retention).
	MaxEndBlockerPassItems = 50
)

// VaultAddress derives the keyless address of vault id:
// Module("vault", "vault/" || bigEndian(id)).
func VaultAddress(id uint64) sdk.AccAddress {
	key := make([]byte, 6+8)
	copy(key, "vault/")
	binary.BigEndian.PutUint64(key[6:], id)
	return sdkaddress.Module(ModuleName, key)
}

// MessagesHash is sha256 over a deterministic encoding of the inner messages:
// for each Any, the big-endian length of the type URL, the URL, the length of
// the value and the value. Wallets recompute it to show what an approval
// covers; MsgApprove must carry it.
func MessagesHash(msgs []*codectypes.Any) []byte {
	h := sha256.New()
	var n [4]byte
	for _, a := range msgs {
		url, val := "", []byte(nil)
		if a != nil {
			url, val = a.TypeUrl, a.Value
		}
		binary.BigEndian.PutUint32(n[:], commontypes.Saturate[uint32](len(url)))
		h.Write(n[:])
		h.Write([]byte(url))
		binary.BigEndian.PutUint32(n[:], commontypes.Saturate[uint32](len(val)))
		h.Write(n[:])
		h.Write(val)
	}
	return h.Sum(nil)
}

// ValidateConfig checks the rules shared by MsgCreateVault and MsgUpdateVault:
// 1 <= len(signers) <= MaxSigners, no duplicate signer, every weight in
// [1, MaxSignerWeight], 1 <= threshold <= sum(weights), the TTL within bounds
// and the label within length. Address validity is checked by the caller with
// its address codec. There is deliberately no way to write an empty signer
// set or an unreachable threshold, so no create or rotation can brick a vault.
// The sum counts unaccepted entries too: a vault may be created (or rotated)
// with a threshold only its invitees can complete, and proposals then cannot
// execute until enough weight has accepted (MsgAcceptVaultSigner). The
// accepted flag is set by the keeper, never by these rules.
// One case stays outside these rules: a RECOVERY or COUNCIL migration that
// merges two entries into one member (Tally counts that member once) can
// lower the reachable weight below the threshold afterwards; the owner should
// rotate one entry out before migrating (docs/x-vault-spec.md §7).
func ValidateConfig(signers []VaultSigner, threshold uint64, proposalTTL int64, label string) error {
	if len(signers) == 0 {
		return fmt.Errorf("%w: at least one signer", ErrInvalidSignerSet)
	}
	if len(signers) > MaxSigners {
		return fmt.Errorf("%w: at most %d signers, got %d", ErrInvalidSignerSet, MaxSigners, len(signers))
	}
	seen := make(map[string]bool, len(signers))
	var sum uint64
	for _, s := range signers {
		if s.Address == "" {
			return fmt.Errorf("%w: empty signer address", ErrInvalidSignerSet)
		}
		if seen[s.Address] {
			return fmt.Errorf("%w: duplicate signer %s", ErrInvalidSignerSet, s.Address)
		}
		seen[s.Address] = true
		if s.Weight == 0 || s.Weight > MaxSignerWeight {
			return fmt.Errorf("%w: weight of %s must be in [1, %d], got %d", ErrInvalidSignerSet, s.Address, MaxSignerWeight, s.Weight)
		}
		sum += s.Weight
	}
	if threshold == 0 || threshold > sum {
		return fmt.Errorf("%w: must be in [1, %d], got %d", ErrInvalidThreshold, sum, threshold)
	}
	if proposalTTL < MinProposalTTL || proposalTTL > MaxProposalTTL {
		return fmt.Errorf("%w: must be in [%d, %d] seconds, got %d", ErrInvalidProposalTTL, MinProposalTTL, MaxProposalTTL, proposalTTL)
	}
	if len(label) > MaxLabelLength {
		return fmt.Errorf("%w: at most %d chars", ErrInvalidLabel, MaxLabelLength)
	}
	return nil
}

// SignerEntry returns addr's entry in the signer set and whether it exists.
func (v Vault) SignerEntry(addr string) (VaultSigner, bool) {
	for _, s := range v.Signers {
		if s.Address == addr {
			return s, true
		}
	}
	return VaultSigner{}, false
}

// SignerWeight returns the weight of addr in the signer set, 0 when absent.
func (v Vault) SignerWeight(addr string) uint64 {
	for _, s := range v.Signers {
		if s.Address == addr {
			return s.Weight
		}
	}
	return 0
}

// IsResolved reports whether the proposal has left PENDING.
func (p VaultProposal) IsResolved() bool {
	return p.Status != ProposalStatus_PROPOSAL_STATUS_PENDING
}

// NonDelegableMsgs lists every x/vault message: a session hot key must never
// cast a co-signer's approval, so x/session folds NonDelegableMsgTypeURLs into
// its ForbiddenAllowlistMsgs (guard: types/allowlist_guard_test.go walks the
// Msg service).
var NonDelegableMsgs = []sdk.Msg{
	&MsgCreateVault{},
	&MsgPropose{},
	&MsgApprove{},
	&MsgRevokeApproval{},
	&MsgExecute{},
	&MsgCancelProposal{},
	&MsgUpdateVault{},
	&MsgUpdateParams{},
	&MsgAcceptVaultSigner{},
	&MsgLeaveVault{},
}

// NonDelegableMsgTypeURLs returns the type URLs of NonDelegableMsgs (a
// function: message names are registered by the generated init()).
func NonDelegableMsgTypeURLs() []string {
	out := make([]string, 0, len(NonDelegableMsgs))
	for _, m := range NonDelegableMsgs {
		out = append(out, sdk.MsgTypeURL(m))
	}
	return out
}

// InnerAllowedVaultMsgs are the only x/vault messages a vault may execute as
// inner messages of its own proposal: rotating itself and cancelling one of
// its proposals. Everything else in x/vault is refused (no vault-in-vault
// recursion).
var InnerAllowedVaultMsgs = []sdk.Msg{&MsgUpdateVault{}, &MsgCancelProposal{}}

// IsInnerAllowedVaultMsg reports whether url names one of InnerAllowedVaultMsgs.
func IsInnerAllowedVaultMsg(url string) bool {
	for _, m := range InnerAllowedVaultMsgs {
		if sdk.MsgTypeURL(m) == url {
			return true
		}
	}
	return false
}

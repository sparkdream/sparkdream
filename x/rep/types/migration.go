package types

import (
	"bytes"
	"crypto/sha256"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Compiled constants of the member key migration (docs/x-rep-spec.md,
// "Member key migration"). They bound gas and state and are deliberately not
// parameters: lowering the horizon reopens dedup, raising the alias ceilings
// raises the per-check gas inside other modules' transactions.
const (
	// MaxRecentAliases is the hard ceiling on aliases finalized within
	// AliasDedupHorizonEpochs: the gas bound of the in-tx dedup set. Eight
	// aliases is about nine store reads per check, which fits the ~50k gas
	// headroom of a council vote.
	MaxRecentAliases = 8
	// AliasRecoveryReserve is the number of recent-alias slots only RECOVERY
	// and COUNCIL migrations may use. The KEY path is refused once
	// MaxRecentAliases - AliasRecoveryReserve recent aliases exist, so a
	// recovery is never blocked by alias exhaustion.
	AliasRecoveryReserve = 2
	// MaxLifetimeAliases caps every alias a member ever accumulates. It bounds
	// path compression at finalization and full-history reads; at two
	// rotations a year it lasts three decades. At the ceiling every path is
	// refused.
	MaxLifetimeAliases = 64
	// MaxRecoveryCommitments is how many recovery commitments a member may
	// hold at once (active plus pending). A leaked or burned preimage voids
	// only its own commitment.
	MaxRecoveryCommitments = 4
	// RecoveryCommitmentSize is the byte length of a commitment, a sealed
	// commit and the hashes they are compared with (SHA-256).
	RecoveryCommitmentSize = sha256.Size
	// MaxRecoveryPreimageSize bounds the preimage carried by a reveal.
	MaxRecoveryPreimageSize = 256

	// RecoveryCommitmentDomain prefixes the preimage in a commitment hash.
	RecoveryCommitmentDomain = "sparkdream/recovery/commit/v1"
	// RecoverySealDomain prefixes (preimage || from || to) in a sealed commit.
	RecoverySealDomain = "sparkdream/recovery/seal/v1"

	// MigrationObjectionInitiativeType is the ChallengerClaim tag of the jury
	// review opened by MsgObjectMigration. TallyJuryVotes dispatches on it.
	MigrationObjectionInitiativeType = "member_migration_objection"
	// MaxMigrationFinalizationsPerBlock caps the EndBlocker finalization pass;
	// the backlog drains over the following blocks.
	MaxMigrationFinalizationsPerBlock = 10
	// MaxMigrationExpiriesPerBlock caps the accept-window and seal expiry
	// passes.
	MaxMigrationExpiriesPerBlock = 50
	// MaxZkKeyActivationsPerBlock caps the pending ZK key activation pass.
	MaxZkKeyActivationsPerBlock = 50
	// MaxTombstoneUndelegations caps how many of a tombstone's delegations
	// one finalization or MsgSweepTombstone starts unbonding; a later sweep
	// picks up the rest.
	MaxTombstoneUndelegations = 50

	// AssumedBlockTimeSeconds converts epoch-denominated windows to the
	// seconds other modules measure voting periods in. It is the same
	// assumption behind epoch_blocks = 14400 being "about a day".
	AssumedBlockTimeSeconds = 6
)

// AliasDedupHorizonEpochs is how far back (in rep epochs) RecentAddressesOf
// reaches. Every bounded dedup scope (voting periods, challenge and appeal
// windows, review rounds, season-scoped records) must be shorter than it, so
// the x/season params validator keeps a season with its transition and
// extensions inside it. Production value 240; the test build widens it so the
// e2e season pin (max_extension_epochs 100000) stays valid.
var AliasDedupHorizonEpochs = getAliasDedupHorizonEpochs()

// RecoveryCommitmentHash is SHA-256(RecoveryCommitmentDomain || preimage).
func RecoveryCommitmentHash(preimage []byte) []byte {
	h := sha256.New()
	h.Write([]byte(RecoveryCommitmentDomain))
	h.Write(preimage)
	return h.Sum(nil)
}

// RecoverySeal is SHA-256(RecoverySealDomain || preimage || from || to), the
// value MsgCommitRecovery carries and MsgRevealRecovery is checked against.
// Binding the target address into the seal is what stops a mempool observer
// from redirecting a reveal to their own address.
func RecoverySeal(preimage []byte, from, to string) []byte {
	h := sha256.New()
	h.Write([]byte(RecoverySealDomain))
	h.Write(preimage)
	h.Write([]byte(from))
	h.Write([]byte(to))
	return h.Sum(nil)
}

// MigrationPathRank orders the paths by precedence: RECOVERY > COUNCIL > KEY.
// Opening a higher-ranked migration cancels a pending lower-ranked one; a
// lower-ranked request is refused while a higher-ranked one is pending.
func MigrationPathRank(p MigrationPath) int {
	switch p {
	case MigrationPath_MIGRATION_PATH_KEY:
		return 1
	case MigrationPath_MIGRATION_PATH_COUNCIL:
		return 2
	case MigrationPath_MIGRATION_PATH_RECOVERY:
		return 3
	default:
		return 0
	}
}

// IsPendingMigrationStatus reports whether a migration still occupies the
// member's single pending slot.
func IsPendingMigrationStatus(s MigrationStatus) bool {
	switch s {
	case MigrationStatus_MIGRATION_STATUS_REQUESTED,
		MigrationStatus_MIGRATION_STATUS_ACCEPTED,
		MigrationStatus_MIGRATION_STATUS_OBJECTED:
		return true
	default:
		return false
	}
}

// ActiveCommitment reports whether c is usable for a reveal at blockTime: it
// has reached effective_at and is not scheduled for removal at or before
// blockTime.
func (s RecoveryCommitmentSet) ActiveCommitment(c RecoveryCommitment, blockTime int64) bool {
	if c.EffectiveAt > blockTime {
		return false
	}
	for _, r := range s.PendingRemovals {
		if bytes.Equal(r.Commitment, c.Commitment) && r.RemovalAt <= blockTime {
			return false
		}
	}
	return true
}

// FindCommitment returns the index of commitment in s, or -1.
func (s RecoveryCommitmentSet) FindCommitment(commitment []byte) int {
	for i, c := range s.Commitments {
		if bytes.Equal(c.Commitment, commitment) {
			return i
		}
	}
	return -1
}

// MigrationMsgs lists every x/rep migration message. They must never be
// delegable through x/session nor executable through x/shield: a session key
// or an anonymous proof must never be able to move an identity. x/session
// folds MigrationMsgTypeURLs into its forbidden list and the guard test in
// types/migration_guard_test.go walks the Msg service to make sure no new
// migration message escapes this list.
var MigrationMsgs = []sdk.Msg{
	&MsgRequestMigration{},
	&MsgAcceptMigration{},
	&MsgCancelMigration{},
	&MsgSetRecoveryCommitment{},
	&MsgRemoveRecoveryCommitment{},
	&MsgCommitRecovery{},
	&MsgRevealRecovery{},
	&MsgForceMigration{},
	&MsgObjectMigration{},
	&MsgSweepTombstone{},
}

// MigrationMsgTypeURLs returns the type URLs of MigrationMsgs. A function
// rather than a package variable: message names are registered by the
// generated code's init(), which runs after package-level variables are
// initialised, so a variable would hold empty names.
func MigrationMsgTypeURLs() []string {
	urls := make([]string, 0, len(MigrationMsgs))
	for _, m := range MigrationMsgs {
		urls = append(urls, sdk.MsgTypeURL(m))
	}
	return urls
}

// IdentityMsgs is the rep identity surface: the migration messages plus the
// ZK key messages. A session hot key that could register or clear a ZK key
// would bypass the account's signing threshold on the member's anonymous
// identity (multisig-accounts plan §3.2), so none of these may ever appear in
// an x/session allowlist, be shield-compatible, or be executable by a shared
// vault. x/session's ForbiddenAllowlistMsgs builds from IdentityMsgTypeURLs.
var IdentityMsgs = append(append([]sdk.Msg{}, MigrationMsgs...),
	&MsgRegisterZkPublicKey{},
	&MsgClearZkPublicKey{},
)

// IdentityMsgTypeURLs returns the type URLs of IdentityMsgs (a function for
// the same init-order reason as MigrationMsgTypeURLs).
func IdentityMsgTypeURLs() []string {
	urls := make([]string, 0, len(IdentityMsgs))
	for _, m := range IdentityMsgs {
		urls = append(urls, sdk.MsgTypeURL(m))
	}
	return urls
}

// IsMigrationMsgTypeURL reports whether url names a migration message.
func IsMigrationMsgTypeURL(url string) bool {
	for _, u := range MigrationMsgTypeURLs() {
		if u == url {
			return true
		}
	}
	return false
}

// AllRoleTypes lists every bonded role type (never UNSPECIFIED), ascending.
// Finalization re-keys BondedRoles and RoleActivities for each of them.
func AllRoleTypes() []RoleType {
	out := make([]RoleType, 0, len(RoleType_name))
	for v := range RoleType_name {
		if v == 0 {
			continue
		}
		out = append(out, RoleType(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// MigrationObjectionEscalationWindow is how long (seconds) x/gov has to
// decide an objection that an inconclusive jury escalated. Past it the
// objection is rejected and the council migration proceeds, so a stalled
// escalation cannot park a migration forever. 30 days on network builds (a
// conservative bound that leaves room for a full gov proposal cycle); 90
// seconds in the test build so the e2e suites can reach the expiry path
// (build-tagged through getMigrationTimings).
var MigrationObjectionEscalationWindow = getMigrationTimings().objectionEscalationWindow

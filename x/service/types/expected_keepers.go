package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
}

// BankKeeper defines the expected interface for the Bank module. x/service
// uses bank to escrow/return operator bonds, report deposits, controller-
// transfer case deposits, and tier-1 escrow (see §3.4.7 four-pool model).
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// CommonsKeeper defines the slice of x/commons that x/service depends on.
// All references to Groups go through x/commons (see x-service-spec.md
// §3.3 — controller MUST be a registered x/commons Group address).
//
// Note: the x-service-spec.md text uses "Group" terminology generically;
// the actual x/commons API uses "GroupPolicy" — a Group's policy address
// is what gets stored as the operator's controller.
type CommonsKeeper interface {
	// IsGroupPolicyAddress reports whether `addr` is a registered
	// x/commons group-policy address (used by MsgRegisterOperator
	// validation, §5.1, and by MsgFinalizeControllerTransfer apply-time
	// re-check, §5.4).
	IsGroupPolicyAddress(ctx context.Context, addr string) bool

	// IsGroupPolicyMember reports whether `memberAddr` is a member of
	// the group whose policy address is `policyAddr`. Used by
	// MsgReportOperator to enforce the reporter-not-controller-member
	// rule (§3.4.6 + §5.2).
	IsGroupPolicyMember(ctx context.Context, policyAddr string, memberAddr string) (bool, error)

	// GroupPolicyMemberCount returns the number of active members in the
	// group whose policy address is `policyAddr`. Used by
	// MsgFinalizeControllerTransfer to re-check Group eligibility at
	// apply time (§5.4) — a Group that has been emptied during the jury
	// case must not become a valid controller. Returns 0 if the policy
	// address is unknown.
	GroupPolicyMemberCount(ctx context.Context, policyAddr string) (uint64, error)

	// IsCouncilAuthorized reports whether `addr` is authorized via the
	// given (council, committee) tuple. x/service uses
	// ("commons", "operations") to gate `MsgResolveReportByJury` and
	// `MsgFinalizeControllerTransfer` resolvers — matching the
	// project-wide pattern (see x/rep's `MsgResolveGovActionAppeal`,
	// forum's appeal-resolve handlers, etc.).
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}

// RepKeeper defines the slice of x/rep that x/service depends on for
// reputation accrual (§6.6), trust-level gating (§3.4.6 / §5.4), and
// jury-case management (§6.2).
type RepKeeper interface {
	// AddReputation grants `amount` to `address` under the given tag —
	// used for positive accrual at successful unbond claim (§6.6).
	AddReputation(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error

	// DeductReputation deducts `amount` from `address` under the given
	// tag — used on tier-1 / tier-2 slashes (§6.6).
	DeductReputation(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error

	// CreateAppealInitiative opens a generic jury appeal in x/rep with
	// the given type-tag and opaque payload. Used by
	// `MsgResolveReport(ESCALATE_TO_JURY)`, `MsgContestSlash`, and
	// `MsgOpenControllerTransferCase` (§6.2). Returns the new JuryReview
	// ID, which x/service stores as `jury_case_id` on the originating
	// report or controller-transfer case.
	//
	// `initiativeType` is one of `"service.slash"` /
	// `"service.controller_transfer"` (free-form string, x/rep stores
	// it on the JuryReview's `ChallengerClaim` field).
	//
	// `deadline` is the block height by which the jury must reach a
	// verdict; x/service sets it to `current_height + max_escalated_blocks`.
	//
	// Mirrors `x/forum/types.RepKeeper.CreateAppealInitiative`.
	CreateAppealInitiative(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error)

	// GetJuryVerdictName returns the canonical verdict-enum name for a
	// JuryReview (e.g. "VERDICT_UPHOLD_CHALLENGE",
	// "VERDICT_REJECT_CHALLENGE", "VERDICT_INCONCLUSIVE",
	// "VERDICT_PENDING"). Returns an error if the JuryReview doesn't
	// exist. The string-keyed API keeps the x/rep type enum out of
	// x/service's import surface.
	//
	// Used by MsgResolveReportByJury and MsgFinalizeControllerTransfer
	// to cross-check the resolver's submitted verdict against the
	// jury's actual tally (§5.2 + §6.2 — see the verdict-mapping table).
	GetJuryVerdictName(ctx context.Context, juryReviewID uint64) (string, error)

	// MeetsTrustLevel reports whether `address` currently has a trust
	// level >= the named minimum (e.g. "TRUST_LEVEL_ESTABLISHED").
	// Used by MsgReportOperator (§3.4.6 reporter gate) and
	// MsgOpenControllerTransferCase (§5.4 opener gate). Returns false
	// (no error) if the address isn't a known member — non-members
	// have no trust level. The string-keyed API avoids importing
	// x/rep's TrustLevel enum into x/service.
	MeetsTrustLevel(ctx context.Context, address sdk.AccAddress, minLevelName string) (bool, error)
}

// Canonical x/rep verdict names that x/service compares against (see
// x/rep/types.Verdict_name). Centralised here so spec drift on either
// side surfaces as a constant rename in this file.
const (
	JuryVerdictPending          = "VERDICT_PENDING"
	JuryVerdictUpholdChallenge  = "VERDICT_UPHOLD_CHALLENGE"
	JuryVerdictRejectChallenge  = "VERDICT_REJECT_CHALLENGE"
	JuryVerdictInconclusive     = "VERDICT_INCONCLUSIVE"
)

// DistributionKeeper defines the slice of x/distribution that x/service
// depends on. Slashed SPARK and forfeited deposits always go to the
// community pool via FundCommunityPool (see §3.4.8).
type DistributionKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}

// ServiceHooks is the hook interface external modules (notably
// x/commons) implement to react to operator-terminal transitions
// (see §6.1). Registered with the service Keeper via SetHooks.
type ServiceHooks interface {
	// AfterOperatorDissolved fires when an operator transitions to
	// SLASHED. The x/commons implementation cancels any active
	// RecurringSpend schedules targeting the operator address — without
	// this, slashed operators would silently keep receiving scheduled
	// SPARK.
	AfterOperatorDissolved(ctx context.Context, operator sdk.AccAddress, serviceType string)

	// AfterOperatorRetired fires when an operator completes voluntary
	// unbonding. Present for parity / future use; x/commons does NOT
	// auto-cancel recurring spends on RETIRED (operator may simply be
	// rotating keys; controller decides whether to keep paying).
	AfterOperatorRetired(ctx context.Context, operator sdk.AccAddress, serviceType string)
}

// MultiServiceHooks aggregates multiple ServiceHooks implementations
// behind the single setter expected by the keeper. Modelled on the
// SDK's pattern (e.g. staking's MultiStakingHooks).
type MultiServiceHooks []ServiceHooks

// NewMultiServiceHooks returns a MultiServiceHooks wrapping the given
// implementations.
func NewMultiServiceHooks(hooks ...ServiceHooks) MultiServiceHooks {
	return hooks
}

var _ ServiceHooks = MultiServiceHooks{}

func (h MultiServiceHooks) AfterOperatorDissolved(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	for _, hook := range h {
		hook.AfterOperatorDissolved(ctx, operator, serviceType)
	}
}

func (h MultiServiceHooks) AfterOperatorRetired(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	for _, hook := range h {
		hook.AfterOperatorRetired(ctx, operator, serviceType)
	}
}

// ParamSubspace defines the expected Subspace interface for parameters.
// Retained from the ignite scaffold; not currently used by x/service
// (params are accessed via the keeper's collections.Item).
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

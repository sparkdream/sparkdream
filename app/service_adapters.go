package app

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"

	commonskeeper "sparkdream/x/commons/keeper"
	federationtypes "sparkdream/x/federation/types"
	repkeeper "sparkdream/x/rep/keeper"
	reptypes "sparkdream/x/rep/types"
	servicekeeper "sparkdream/x/service/keeper"
	servicetypes "sparkdream/x/service/types"
)

// service_adapters.go bridges the concrete x/commons, x/rep, and
// x/distribution keepers to the slimmer interfaces x/service expects
// (see x/service/types/expected_keepers.go). The adapters shrink the
// cross-module surface to just what x/service needs and translate
// between x/rep's typed enums (TrustLevel, Verdict) and the
// string-keyed API x/service uses internally.

// ---------------------------------------------------------------------------
// Commons → service.CommonsKeeper adapter
// ---------------------------------------------------------------------------

// ServiceCommonsAdapter implements service.types.CommonsKeeper from a
// concrete x/commons keeper.
type ServiceCommonsAdapter struct {
	keeper commonskeeper.Keeper
}

// NewServiceCommonsAdapter wraps the concrete commons keeper.
func NewServiceCommonsAdapter(k commonskeeper.Keeper) *ServiceCommonsAdapter {
	return &ServiceCommonsAdapter{keeper: k}
}

var _ servicetypes.CommonsKeeper = (*ServiceCommonsAdapter)(nil)

func (a *ServiceCommonsAdapter) IsGroupPolicyAddress(ctx context.Context, addr string) bool {
	return a.keeper.IsGroupPolicyAddress(ctx, addr)
}

func (a *ServiceCommonsAdapter) IsGroupPolicyMember(ctx context.Context, policyAddr string, memberAddr string) (bool, error) {
	return a.keeper.IsGroupPolicyMember(ctx, policyAddr, memberAddr)
}

// GroupPolicyMemberCount delegates to x/commons's helper of the same
// name. Returns 0 (no error) if the policy address is unknown.
func (a *ServiceCommonsAdapter) GroupPolicyMemberCount(ctx context.Context, policyAddr string) (uint64, error) {
	return a.keeper.GroupPolicyMemberCount(ctx, policyAddr)
}

// IsCouncilAuthorized delegates to x/commons. Used by x/service to gate
// jury-resolver msgs to the Commons Council Operations Committee (see
// expected_keepers.go).
func (a *ServiceCommonsAdapter) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	return a.keeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// IsCouncilOrCommitteePolicy delegates to x/commons.
func (a *ServiceCommonsAdapter) IsCouncilOrCommitteePolicy(ctx context.Context, addr string, council string, committee string) bool {
	return a.keeper.IsCouncilOrCommitteePolicy(ctx, addr, council, committee)
}

// GroupPolicyMembers lists the members of the group behind policyAddr;
// nil when the address is not a group policy.
func (a *ServiceCommonsAdapter) GroupPolicyMembers(ctx context.Context, policyAddr string) ([]string, error) {
	name, _, ok := a.keeper.GetGroupByPolicy(ctx, policyAddr)
	if !ok {
		return nil, nil
	}
	members, err := a.keeper.GetCouncilMembers(ctx, name)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		out = append(out, m.Address)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Rep → service.RepKeeper adapter
// ---------------------------------------------------------------------------

// ServiceRepAdapter implements service.types.RepKeeper from a concrete
// x/rep keeper. All methods delegate directly to x/rep's existing API
// — there are no stubs; the underlying jury system already exists and
// is used by forum / rep's gov_action_appeal flow.
type ServiceRepAdapter struct {
	keeper repkeeper.Keeper
}

// NewServiceRepAdapter wraps the concrete rep keeper.
func NewServiceRepAdapter(k repkeeper.Keeper) *ServiceRepAdapter {
	return &ServiceRepAdapter{keeper: k}
}

var _ servicetypes.RepKeeper = (*ServiceRepAdapter)(nil)

func (a *ServiceRepAdapter) AddReputation(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error {
	return a.keeper.AddReputation(ctx, address, tag, amount)
}

func (a *ServiceRepAdapter) DeductReputation(ctx context.Context, address sdk.AccAddress, tag string, amount math.LegacyDec) error {
	return a.keeper.DeductReputation(ctx, address, tag, amount)
}

// CreateAppealInitiative delegates to x/rep's generic-jury entry point
// — the same call forum uses for moderation appeals and rep uses for
// gov_action_appeal. Returns the JuryReview ID.
func (a *ServiceRepAdapter) CreateAppealInitiative(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error) {
	return a.keeper.CreateAppealInitiative(ctx, initiativeType, payload, deadline)
}

// CreateSeatedAppeal opens an x/rep jury case with its jury seated at once
// (excluding the parties) and indexed for tallying. x/service uses it for
// service.slash and service.controller_transfer cases.
func (a *ServiceRepAdapter) CreateSeatedAppeal(ctx context.Context, initiativeType string, payload []byte, deadline int64, excludes []string) (uint64, error) {
	return a.keeper.CreateSeatedAppeal(ctx, initiativeType, payload, deadline, excludes)
}

// GetJuryVerdictName returns the canonical enum name for the given
// JuryReview's verdict, e.g. "VERDICT_UPHOLD_CHALLENGE". x/service
// callers compare this against the resolver's submitted verdict to
// detect mismatches (see MsgResolveReportByJury / MsgFinalizeControllerTransfer).
func (a *ServiceRepAdapter) GetJuryVerdictName(ctx context.Context, juryReviewID uint64) (string, error) {
	review, err := a.keeper.GetJuryReview(ctx, juryReviewID)
	if err != nil {
		return "", err
	}
	name, ok := reptypes.Verdict_name[int32(review.Verdict)]
	if !ok {
		return servicetypes.JuryVerdictPending, nil
	}
	return name, nil
}

// MeetsTrustLevel checks the address's current trust level against
// the named minimum. Returns false (no error) if the address isn't a
// known member (non-members have no trust level — they can't pass
// any gate).
//
// Trust levels are ordered ints in x/rep (NEW=0, PROVISIONAL=1,
// ESTABLISHED=2, ..., CORE=4). The named minimum is the enum name
// string (e.g. "TRUST_LEVEL_ESTABLISHED"); empty string means "no
// gate" and always returns true.
func (a *ServiceRepAdapter) MeetsTrustLevel(ctx context.Context, address sdk.AccAddress, minLevelName string) (bool, error) {
	if minLevelName == "" {
		return true, nil
	}
	minVal, ok := reptypes.TrustLevel_value[minLevelName]
	if !ok {
		// Params.Validate rejects unknown names; a stored one (from before
		// that check) must not read as "no gate", so fail closed.
		return false, nil
	}
	current, err := a.keeper.GetTrustLevel(ctx, address)
	if err != nil {
		// Non-member or fetch error — fail closed.
		return false, nil
	}
	return int32(current) >= minVal, nil
}

// ---------------------------------------------------------------------------
// Commons → service.ServiceHooks adapter
// ---------------------------------------------------------------------------

// CommonsServiceHooks implements x/service's ServiceHooks interface by
// driving x/commons-side cleanup on operator-terminal transitions.
// Currently the only load-bearing hook is AfterOperatorDissolved →
// cancel matching ACTIVE RecurringSpend schedules (§6.1).
type CommonsServiceHooks struct {
	commons commonskeeper.Keeper
}

// NewCommonsServiceHooks wraps the commons keeper as a ServiceHooks
// implementation. Registered with the service keeper via SetHooks in
// app.go.
func NewCommonsServiceHooks(k commonskeeper.Keeper) *CommonsServiceHooks {
	return &CommonsServiceHooks{commons: k}
}

var _ servicetypes.ServiceHooks = (*CommonsServiceHooks)(nil)

// AfterOperatorDissolved cancels every ACTIVE RecurringSpend whose
// `recipient == operator.address`. Without this, slashed operators
// would continue receiving scheduled payments until a human notices
// (§6.1).
//
// The sweep runs in a cache context behind a recover, like federation's
// runIsolated: an error or a panic in commons discards only the sweep and
// never rolls back the service slash or jury verdict that fired the hook.
// Out-of-gas is re-panicked so gas accounting stays intact.
func (h *CommonsServiceHooks) AfterOperatorDissolved(ctx context.Context, operator sdk.AccAddress, serviceType string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	reason := "operator_dissolved:" + serviceType
	ok := func() (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				if _, oog := r.(storetypes.ErrorOutOfGas); oog {
					panic(r)
				}
				sdkCtx.Logger().Error("commons service hook panic recovered",
					"hook", "AfterOperatorDissolved", "operator", operator.String(),
					"service_type", serviceType, "panic", fmt.Sprintf("%v", r))
				ok = false
			}
		}()
		if _, err := h.commons.CancelActiveSchedulesForRecipient(cacheCtx, operator.String(), reason); err != nil {
			sdkCtx.Logger().Error("commons service hook failed",
				"hook", "AfterOperatorDissolved", "operator", operator.String(),
				"service_type", serviceType, "err", err)
			return false
		}
		return true
	}()
	if ok {
		write()
	}
}

// AfterOperatorRetired is a no-op for x/commons: a voluntarily-exiting
// operator may be rotating keys or paying out from accumulated balance,
// so the controller decides whether to keep paying (§6.1).
func (h *CommonsServiceHooks) AfterOperatorRetired(_ context.Context, _ sdk.AccAddress, _ string) {}

// AfterOperatorUnderfunded is a no-op for x/commons: an underfunded
// operator is in temporary distress and may recover via top-up; the
// controller decides whether to keep paying. Consumer modules that need
// to suspend operator activity on underfunding (e.g. x/federation) hook
// this on their own side.
func (h *CommonsServiceHooks) AfterOperatorUnderfunded(_ context.Context, _ sdk.AccAddress, _ string) {
}

// AfterOperatorReFunded is a no-op for x/commons: symmetric counterpart
// to AfterOperatorUnderfunded; commons doesn't track operator activity
// state.
func (h *CommonsServiceHooks) AfterOperatorReFunded(_ context.Context, _ sdk.AccAddress, _ string) {}

// ---------------------------------------------------------------------------
// Distribution → service.DistributionKeeper adapter
// ---------------------------------------------------------------------------

// ServiceDistributionAdapter implements service.types.DistributionKeeper
// from the SDK's concrete distribution keeper.
type ServiceDistributionAdapter struct {
	keeper distrkeeper.Keeper
}

// NewServiceDistributionAdapter wraps the concrete distribution keeper.
func NewServiceDistributionAdapter(k distrkeeper.Keeper) *ServiceDistributionAdapter {
	return &ServiceDistributionAdapter{keeper: k}
}

var _ servicetypes.DistributionKeeper = (*ServiceDistributionAdapter)(nil)

func (a *ServiceDistributionAdapter) FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error {
	return a.keeper.FundCommunityPool(ctx, amount, sender)
}

// ---------------------------------------------------------------------------
// Service → federation.ServiceKeeper adapter
// ---------------------------------------------------------------------------

// FederationServiceAdapter wraps the concrete x/service Keeper to
// satisfy x/federation's slimmer ServiceKeeper interface (Phase 2 of
// the federation→service migration). Federation's interface keeps the
// `source` parameter of RegisterOperator as a plain `int` so federation
// doesn't have to import servicekeeper for the SlashSource enum; the
// adapter does the type conversion here.
type FederationServiceAdapter struct {
	keeper servicekeeper.Keeper
}

// NewFederationServiceAdapter wraps the concrete service keeper.
func NewFederationServiceAdapter(k servicekeeper.Keeper) *FederationServiceAdapter {
	return &FederationServiceAdapter{keeper: k}
}

var _ federationtypes.ServiceKeeper = (*FederationServiceAdapter)(nil)

func (a *FederationServiceAdapter) RegisterOperator(ctx context.Context, creator, serviceType, controller string, bondAmount math.Int, metadata []byte, source int) (servicetypes.Operator, error) {
	return a.keeper.RegisterOperator(ctx, creator, serviceType, controller, bondAmount, metadata, servicekeeper.SlashSource(source))
}

func (a *FederationServiceAdapter) TopUpBond(ctx context.Context, opBytes []byte, serviceType string, additionalBondAmount math.Int) error {
	return a.keeper.TopUpBond(ctx, opBytes, serviceType, additionalBondAmount)
}

func (a *FederationServiceAdapter) OpenSystemReport(ctx context.Context, callerModuleAddr sdk.AccAddress, operatorAddr sdk.AccAddress, serviceType string, slashBps uint32, evidenceURI string, dedupeKey []byte) (uint64, bool, error) {
	return a.keeper.OpenSystemReport(ctx, callerModuleAddr, operatorAddr, serviceType, slashBps, evidenceURI, dedupeKey)
}

func (a *FederationServiceAdapter) GetOperator(ctx context.Context, addrBytes []byte, serviceType string) (servicetypes.Operator, bool) {
	return a.keeper.GetOperator(ctx, addrBytes, serviceType)
}

func (a *FederationServiceAdapter) HasSlashedRecord(ctx context.Context, addrBytes []byte, serviceType string) (bool, error) {
	return a.keeper.HasSlashedRecord(ctx, addrBytes, serviceType)
}

func (a *FederationServiceAdapter) GetServiceTypeConfig(ctx context.Context, serviceType string) (servicetypes.ServiceTypeConfig, bool) {
	return a.keeper.GetServiceTypeConfig(ctx, serviceType)
}

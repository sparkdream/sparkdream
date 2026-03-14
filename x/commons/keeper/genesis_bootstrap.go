package keeper

import (
	"context"
	"fmt"
	"sparkdream/x/commons/types"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// 5 Months in Seconds (assuming 30-day months)
const TermDuration5Months = 12960000

// 1 Year in Seconds
const TermDuration1Year = 31536000

// --- PRODUCTION VOTING WINDOWS ---
const WindowCouncil = 120 * time.Hour     // 5 Days
const WindowCommittee = 120 * time.Hour   // 5 Days
const WindowGovernance = 120 * time.Hour  // 5 Days
const WindowSupervisory = 360 * time.Hour // 15 Days
const WindowVeto = 48 * time.Hour         // 2 Days

// MemberRequest is a local replacement for group.MemberRequest
type MemberRequest struct {
	Address  string
	Weight   string
	Metadata string
}

func (k Keeper) BootstrapGovernance(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("module", "x/commons")
	logger.Info("Bootstrapping 'Three Pillars' Governance...")

	// 1. Gather Founding Members
	// Look up each GenesisNames address directly via GetAccount.
	// IterateAccounts may return empty results during InitGenesis in SDK v0.53
	// because the account iterator index is not yet flushed at that point.
	var foundingMembers []MemberRequest
	var founderMembers []MemberRequest

	for addr, name := range GenesisNames {
		accAddr, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			logger.Error("Invalid address in GenesisNames", "address", addr, "error", err)
			continue
		}
		acc := k.authKeeper.GetAccount(ctx, accAddr)
		if acc == nil {
			logger.Warn("Account not found in auth store", "address", addr, "name", name)
			continue
		}

		foundingMembers = append(foundingMembers, MemberRequest{Address: addr, Weight: "1", Metadata: name})

		if name == FounderName {
			founderMembers = append(founderMembers, MemberRequest{Address: addr, Weight: "1", Metadata: "Founder"})
		}
	}

	if len(foundingMembers) == 0 {
		logger.Error("No founding members found in GenesisNames!")
		return
	}
	if len(founderMembers) == 0 {
		panic("Bootstrap Failed: Founder not found in genesis accounts")
	}

	govAddr := k.authKeeper.GetModuleAddress(types.GovModuleName).String()

	// =========================================================================
	// STEP 1: CREATE THE COMMONS COUNCIL
	// =========================================================================

	commonsConfig := GroupConfig{
		Name:          "Commons Council",
		Description:   "Culture, Arts, and Events (Top Level)",
		FundingWeight: 50,
		ParentPolicy:  govAddr,

		PolicyType:           "percentage",
		StandardValue:        "0.51",
		StandardWindow:       WindowCouncil,
		StandardMinExecution: CommonsCouncilStandardMinExecution,

		VetoValue:        "0.49",
		VetoWindow:       WindowVeto,
		VetoMinExecution: 0,

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgRegisterGroup",
			"/sparkdream.commons.v1.MsgRenewGroup",
			"/sparkdream.commons.v1.MsgSpendFromCommons",
			"/sparkdream.commons.v1.MsgUpdateGroupConfig",
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
			"/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
			"/sparkdream.name.v1.MsgResolveDispute",
			"/sparkdream.commons.v1.MsgVoteProposal",
		},
		VetoPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
			"/sparkdream.commons.v1.MsgVetoGroupProposals",
		},

		MaxSpendPerEpoch: math.NewInt(500000000000),
		UpdateCooldown:   int64(CouncilUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       3,
		MaxMembers:       1000,
		TermDuration:     TermDuration1Year * 1000,
	}
	commonsPolicy := k.createGroup(ctx, commonsConfig, foundingMembers)

	// =========================================================================
	// STEP 2: CREATE TECH & ECO COUNCILS
	// =========================================================================

	// --- A. TECHNICAL COUNCIL ---
	techMembers := []MemberRequest{
		{Address: founderMembers[0].Address, Weight: "3", Metadata: "Lead Core Dev"},
		{Address: commonsPolicy, Weight: "3", Metadata: "Guardian Veto (Commons Council)"},
	}

	techConfig := GroupConfig{
		Name:          "Technical Council",
		Description:   "Chain Upgrades & Security (Top Level)",
		FundingWeight: 30,
		ParentPolicy:  govAddr,

		PolicyType:           "percentage",
		StandardValue:        "0.66",
		StandardWindow:       WindowCouncil,
		StandardMinExecution: TechCouncilStandardMinExecution,

		VetoValue:        "0.49",
		VetoWindow:       WindowVeto,
		VetoMinExecution: 0,

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgRegisterGroup",
			"/sparkdream.commons.v1.MsgRenewGroup",
			"/sparkdream.commons.v1.MsgSpendFromCommons",
			"/sparkdream.commons.v1.MsgUpdateGroupConfig",
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
			"/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
			"/cosmos.gov.v1.MsgUpdateParams",
			"/sparkdream.commons.v1.MsgForceUpgrade",
		},
		VetoPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
			"/sparkdream.commons.v1.MsgVetoGroupProposals",
		},
		MaxSpendPerEpoch: math.NewInt(500000000000),
		UpdateCooldown:   int64(CouncilUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       2,
		MaxMembers:       100,
		TermDuration:     TermDuration1Year * 1000,
	}
	techPolicy := k.createGroup(ctx, techConfig, techMembers)

	// Tech Committees
	k.createGroup(ctx, GroupConfig{
		Name:                 "Technical Operations Committee",
		Description:          "Operational arm for Tech",
		FundingWeight:        0,
		ParentPolicy:         techPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowCommittee,
		StandardMinExecution: TechOpsMinExecution,
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgSpendFromCommons", "/sparkdream.commons.v1.MsgVoteProposal", "/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(10000000000),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2,
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers)

	techGovPolicy := k.createGroup(ctx, GroupConfig{
		Name:                 "Technical Governance Committee",
		Description:          "Membership management for Tech",
		FundingWeight:        0,
		ParentPolicy:         techPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowGovernance,
		StandardMinExecution: TechMembershipMinExecution,
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(0),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2,
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers)

	if err := k.SetElectoralDelegation(ctx, "Technical Council", techGovPolicy); err != nil {
		panic(err)
	}

	// --- B. ECOSYSTEM COUNCIL ---
	ecoMembers := []MemberRequest{
		{Address: founderMembers[0].Address, Weight: "3", Metadata: "Founder"},
		{Address: commonsPolicy, Weight: "3", Metadata: "Guardian Veto (Commons Council)"},
	}

	ecoConfig := GroupConfig{
		Name:          "Ecosystem Council",
		Description:   "Treasury & Growth (Top Level)",
		FundingWeight: 20,
		ParentPolicy:  govAddr,

		PolicyType:           "percentage",
		StandardValue:        "0.66",
		StandardWindow:       WindowCouncil,
		StandardMinExecution: EcoCouncilStandardMinExecution,

		VetoValue:        "0.49",
		VetoWindow:       WindowVeto,
		VetoMinExecution: 0,

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgRegisterGroup",
			"/sparkdream.commons.v1.MsgRenewGroup",
			"/sparkdream.commons.v1.MsgSpendFromCommons",
			"/sparkdream.commons.v1.MsgUpdateGroupConfig",
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
			"/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
		},
		VetoPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgEmergencyCancelGovProposal",
			"/sparkdream.commons.v1.MsgVetoGroupProposals",
		},
		MaxSpendPerEpoch: math.NewInt(500000000000),
		UpdateCooldown:   int64(CouncilUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       2,
		MaxMembers:       1000,
		TermDuration:     TermDuration1Year * 1000,
	}
	ecoPolicy := k.createGroup(ctx, ecoConfig, ecoMembers)

	// Eco Committees
	k.createGroup(ctx, GroupConfig{
		Name:                 "Ecosystem Operations Committee",
		Description:          "Operational arm for Ecosystem",
		FundingWeight:        0,
		ParentPolicy:         ecoPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowCommittee,
		StandardMinExecution: EcoOpsMinExecution,
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgSpendFromCommons", "/sparkdream.commons.v1.MsgUpdateGroupConfig", "/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(10000000000),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2,
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers)

	ecoGovPolicy := k.createGroup(ctx, GroupConfig{
		Name:                 "Ecosystem Governance Committee",
		Description:          "Membership management for Ecosystem",
		FundingWeight:        0,
		ParentPolicy:         ecoPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowGovernance,
		StandardMinExecution: EcoMembershipMinExecution,
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(0),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2,
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers)

	if err := k.SetElectoralDelegation(ctx, "Ecosystem Council", ecoGovPolicy); err != nil {
		panic(err)
	}

	// =========================================================================
	// STEP 3: CREATE THE COMMONS SUPERVISORY BOARD
	// =========================================================================

	supervisorMembers := []MemberRequest{
		{Address: techPolicy, Weight: "1", Metadata: "Tech Guardian"},
		{Address: ecoPolicy, Weight: "1", Metadata: "Eco Guardian"},
	}

	supervisorPolicy := k.createGroup(ctx, GroupConfig{
		Name:          "Commons Supervisory Board",
		Description:   "Oversees the Commons Governance Committee",
		FundingWeight: 0,
		ParentPolicy:  govAddr,

		PolicyType:           "threshold",
		StandardValue:        "2",
		StandardWindow:       WindowSupervisory,
		StandardMinExecution: SupervisoryMinExecution,

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
			"/sparkdream.commons.v1.MsgVetoGroupProposals",
		},
		MaxSpendPerEpoch: math.NewInt(0),
		UpdateCooldown:   int64(SupervisoryUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       2,
		MaxMembers:       2,
		TermDuration:     TermDuration1Year * 1000,
	}, supervisorMembers)

	// =========================================================================
	// STEP 4: CREATE & WIRE COMMONS COMMITTEES
	// =========================================================================

	commOpsPolicy := k.createGroup(ctx, GroupConfig{
		Name:          "Commons Operations Committee",
		Description:   "Day-to-day spending for Commons",
		FundingWeight: 0,
		ParentPolicy:  commonsPolicy,

		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowCommittee,
		StandardMinExecution: CommonsOpsMinExecution,

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgSpendFromCommons",
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
			"/sparkdream.blog.v1.MsgUpdateOperationalParams",
			"/sparkdream.collect.v1.MsgUpdateOperationalParams",
			"/sparkdream.forum.v1.MsgCreateTagBudget",
			"/sparkdream.forum.v1.MsgToggleTagBudget",
			"/sparkdream.forum.v1.MsgUpdateOperationalParams",
			"/sparkdream.forum.v1.MsgWithdrawTagBudget",
			"/sparkdream.futarchy.v1.MsgUpdateOperationalParams",
			"/sparkdream.name.v1.MsgUpdateOperationalParams",
			"/sparkdream.rep.v1.MsgUpdateOperationalParams",
			"/sparkdream.season.v1.MsgUpdateOperationalParams",
		},
		MaxSpendPerEpoch: math.NewInt(10000000000),
		UpdateCooldown:   int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       2,
		MaxMembers:       5,
		TermDuration:     TermDuration5Months,
	}, founderMembers)

	if err := k.SetElectoralDelegation(ctx, "Commons Operations Committee", commOpsPolicy); err != nil {
		panic(err)
	}

	commonsGovPolicy := k.createGroup(ctx, GroupConfig{
		Name:          "Commons Governance Committee",
		Description:   "Membership management for Commons (HR)",
		FundingWeight: 0,
		ParentPolicy:  supervisorPolicy,

		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowGovernance,
		StandardMinExecution: CommonsMembershipMinExecution,

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
		},
		MaxSpendPerEpoch: math.NewInt(0),
		UpdateCooldown:   int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       2,
		MaxMembers:       5,
		TermDuration:     TermDuration5Months,
	}, founderMembers)

	if err := k.SetElectoralDelegation(ctx, "Commons Council", commonsGovPolicy); err != nil {
		panic(err)
	}

	_ = supervisorPolicy
	logger.Info("Bootstrap Complete. Supervisory Structure Active.")
}

// SetElectoralDelegation wires up electoral delegations
func (k Keeper) SetElectoralDelegation(ctx context.Context, parentName string, childPolicy string) error {
	group, err := k.Groups.Get(ctx, parentName)
	if err != nil {
		return err
	}
	group.ElectoralPolicyAddress = childPolicy
	return k.Groups.Set(ctx, parentName, group)
}

// GroupConfig struct
type GroupConfig struct {
	Name          string
	Description   string
	FundingWeight uint64
	ParentPolicy  string
	PolicyType    string

	// Standard Policy
	StandardValue        string
	StandardWindow       time.Duration
	StandardMinExecution time.Duration
	StandardPermissions  []string

	// Veto Policy
	VetoValue        string
	VetoWindow       time.Duration
	VetoMinExecution time.Duration
	VetoPermissions  []string

	// Custom Constraints
	MaxSpendPerEpoch math.Int
	UpdateCooldown   int64
	FutarchyEnabled  bool
	MinMembers       uint64
	MaxMembers       uint64
	TermDuration     int64
}

// createGroup creates a council/committee with native state (no x/group).
func (k Keeper) createGroup(ctx context.Context, cfg GroupConfig, members []MemberRequest) string {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 1. Get next council ID
	councilID, err := k.CouncilSeq.Next(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to get next council ID for %s: %v", cfg.Name, err))
	}

	// 2. Derive deterministic policy address
	policyAddr := DeriveCouncilAddress(councilID, "standard")
	mainPolicyAddr := policyAddr.String()

	// 3. Store members in native collection
	for _, m := range members {
		member := types.Member{
			Address:  m.Address,
			Weight:   m.Weight,
			Metadata: m.Metadata,
			AddedAt:  sdkCtx.BlockTime().Unix(),
		}
		if err := k.AddMember(ctx, cfg.Name, member); err != nil {
			panic(fmt.Sprintf("Failed to add member %s to %s: %v", m.Address, cfg.Name, err))
		}
	}

	// 4. Store decision policy
	stdPolicy := types.DecisionPolicy{
		PolicyType:         cfg.PolicyType,
		Threshold:          cfg.StandardValue,
		VotingPeriod:       int64(cfg.StandardWindow.Seconds()),
		MinExecutionPeriod: int64(cfg.StandardMinExecution.Seconds()),
	}
	if err := k.DecisionPolicies.Set(ctx, mainPolicyAddr, stdPolicy); err != nil {
		panic(err)
	}

	// 5. Initialize policy version
	if err := k.PolicyVersion.Set(ctx, mainPolicyAddr, 0); err != nil {
		panic(err)
	}

	// 6. Register Standard Permissions
	if len(cfg.StandardPermissions) > 0 {
		if err := k.PolicyPermissions.Set(ctx, mainPolicyAddr, types.PolicyPermissions{
			PolicyAddress:   mainPolicyAddr,
			AllowedMessages: cfg.StandardPermissions,
		}); err != nil {
			panic(err)
		}
	}

	// 7. Create Veto Policy if needed
	if len(cfg.VetoPermissions) > 0 {
		vetoPolicyAddr := DeriveCouncilAddress(councilID, "veto")
		vetoAddr := vetoPolicyAddr.String()

		vetoPolicy := types.DecisionPolicy{
			PolicyType:         cfg.PolicyType,
			Threshold:          cfg.VetoValue,
			VotingPeriod:       int64(cfg.VetoWindow.Seconds()),
			MinExecutionPeriod: int64(cfg.VetoMinExecution.Seconds()),
		}
		if err := k.DecisionPolicies.Set(ctx, vetoAddr, vetoPolicy); err != nil {
			panic(err)
		}

		if err := k.PolicyVersion.Set(ctx, vetoAddr, 0); err != nil {
			panic(err)
		}

		if err := k.PolicyPermissions.Set(ctx, vetoAddr, types.PolicyPermissions{
			PolicyAddress:   vetoAddr,
			AllowedMessages: cfg.VetoPermissions,
		}); err != nil {
			panic(err)
		}

		// Map this council's veto policy for sibling lookups
		if err := k.VetoPolicies.Set(ctx, cfg.Name, vetoAddr); err != nil {
			panic(err)
		}

		// Also index the veto policy -> council name
		if err := k.PolicyToName.Set(ctx, vetoAddr, cfg.Name); err != nil {
			panic(err)
		}
	}

	// 8. Funding
	if cfg.FundingWeight > 0 {
		k.splitKeeper.SetShareByAddress(ctx, mainPolicyAddr, cfg.FundingWeight)
	}

	// 9. Store Group
	var vetoPolicyAddr string
	if len(cfg.VetoPermissions) > 0 {
		vetoPolicyAddr = DeriveCouncilAddress(councilID, "veto").String()
	}
	group := types.Group{
		GroupId:               councilID,
		PolicyAddress:         mainPolicyAddr,
		ParentPolicyAddress:   cfg.ParentPolicy,
		VetoPolicyAddress:     vetoPolicyAddr,
		FundingWeight:         cfg.FundingWeight,
		MaxSpendPerEpoch:      &cfg.MaxSpendPerEpoch,
		UpdateCooldown:        cfg.UpdateCooldown,
		FutarchyEnabled:       cfg.FutarchyEnabled,
		MinMembers:            cfg.MinMembers,
		MaxMembers:            cfg.MaxMembers,
		TermDuration:          cfg.TermDuration,
		CurrentTermExpiration: sdkCtx.BlockTime().Unix() + cfg.TermDuration,
		ActivationTime:        0,
	}

	if err := k.Groups.Set(ctx, cfg.Name, group); err != nil {
		panic(err)
	}

	// 10. Index: Policy Address -> Council Name
	if err := k.PolicyToName.Set(ctx, mainPolicyAddr, cfg.Name); err != nil {
		panic(err)
	}

	return mainPolicyAddr
}

package keeper

import (
	"context"
	"fmt"
	"sparkdream/x/commons/types"
	"time"

	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/cosmos-sdk/x/group"
)

// 5 Months in Seconds (assuming 30-day months)
// 5 * 30 * 24 * 60 * 60 = 12,960,000
const TermDuration5Months = 12960000

// 1 Year in Seconds
const TermDuration1Year = 31536000

// --- PRODUCTION VOTING WINDOWS ---
// How long the poll is open for voting.
const WindowCouncil = 120 * time.Hour     // 5 Days
const WindowCommittee = 120 * time.Hour   // 5 Days
const WindowGovernance = 120 * time.Hour  // 5 Days (HR decisions should be slow)
const WindowSupervisory = 360 * time.Hour // 15 Days
const WindowVeto = 48 * time.Hour         // 2 Days (Fast enough to block, slow enough to wake up)

func (k Keeper) BootstrapGovernance(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("module", "x/commons")
	logger.Info("Bootstrapping 'Three Pillars' Governance...")

	// 1. Gather Founding Members (Broad Community)
	var foundingMembers []group.MemberRequest
	var founderMembers []group.MemberRequest // Just the Founder

	k.authKeeper.IterateAccounts(ctx, func(acc sdk.AccountI) bool {
		if _, ok := acc.(sdk.ModuleAccountI); ok {
			return false
		}

		addr := acc.GetAddress().String()
		name, exists := GenesisNames[addr]
		if !exists {
			return false // Skip random accounts (validators, etc.)
		}

		// Broad list for Commons Council
		foundingMembers = append(foundingMembers, group.MemberRequest{Address: addr, Weight: "1", Metadata: name})

		// Trusted Founder for Committees
		if name == FounderName {
			founderMembers = append(founderMembers, group.MemberRequest{Address: addr, Weight: "1", Metadata: "Founder"})
		}
		return false
	})

	if len(foundingMembers) == 0 {
		// return // SAFEGUARD: In production, maybe panic here if no members found?
		logger.Error("No founding members found in GenesisNames!")
		return
	}
	if len(founderMembers) == 0 {
		panic("Bootstrap Failed: Founder not found in genesis accounts")
	}

	govAddr := k.authKeeper.GetModuleAddress(govtypes.ModuleName).String()
	moduleAddr := k.GetModuleAddress().String()

	// =========================================================================
	// STEP 1: CREATE THE COMMONS COUNCIL (HEAD ONLY)
	// =========================================================================

	commonsConfig := GroupConfig{
		Name:          "Commons Council",
		Description:   "Culture, Arts, and Events (Top Level)",
		FundingWeight: 50,
		ParentPolicy:  govAddr,

		PolicyType:           "percentage",
		StandardValue:        "0.51",
		StandardWindow:       WindowCouncil,                      // 5 Days
		StandardMinExecution: CommonsCouncilStandardMinExecution, // 3 Days

		VetoValue:        "0.49",     // Protects minority vote. Can be lowered for larger groups.
		VetoWindow:       WindowVeto, // 2 Days
		VetoMinExecution: 0,          // Vetoes execute immediately

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgDeleteGroup",
			"/sparkdream.commons.v1.MsgRegisterGroup",
			"/sparkdream.commons.v1.MsgRenewGroup",
			"/sparkdream.commons.v1.MsgSpendFromCommons",
			"/sparkdream.commons.v1.MsgUpdateGroupConfig",
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
			"/sparkdream.commons.v1.MsgUpdatePolicyPermissions",
			"/sparkdream.name.v1.MsgResolveDispute",
			"/cosmos.group.v1.MsgVote",
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
	commonsPolicy := k.createExtendedGroup(ctx, commonsConfig, foundingMembers, moduleAddr)

	// =========================================================================
	// STEP 2: CREATE TECH & ECO COUNCILS (THE GUARDIANS)
	// =========================================================================

	// --- A. TECHNICAL COUNCIL ---
	techMembers := []group.MemberRequest{}
	techMembers = append(techMembers, group.MemberRequest{
		Address:  founderMembers[0].Address,
		Weight:   "3",
		Metadata: "Lead Core Dev",
	})
	// Golden Share: Commons Council
	techMembers = append(techMembers, group.MemberRequest{
		Address:  commonsPolicy,
		Weight:   "3",
		Metadata: "Guardian Veto (Commons Council)",
	})

	techConfig := GroupConfig{
		Name:          "Technical Council",
		Description:   "Chain Upgrades & Security (Top Level)",
		FundingWeight: 30,
		ParentPolicy:  govAddr,

		PolicyType:           "percentage",
		StandardValue:        "0.66",                          // Consensus Required (Both must agree to PASS upgrades)
		StandardWindow:       WindowCouncil,                   // 5 Days
		StandardMinExecution: TechCouncilStandardMinExecution, // 3 Days

		VetoValue:        "0.49",     // UNILATERAL DEFENSE: 0.49 allows EITHER member (Weight 3/6) to Trigger Veto
		VetoWindow:       WindowVeto, // 2 Days
		VetoMinExecution: 0,          // Immediate

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
		// RESTRICTED: Veto Policy can ONLY Cancel or Delete a rogue child group. It cannot Create.
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
	techPolicy := k.createExtendedGroup(ctx, techConfig, techMembers, moduleAddr)

	// Tech Committees
	k.createExtendedGroup(ctx, GroupConfig{
		Name:                 "Technical Operations Committee",
		Description:          "Operational arm for Tech",
		FundingWeight:        0,
		ParentPolicy:         techPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowCommittee,     // 3 Days
		StandardMinExecution: TechOpsMinExecution, // 1 Day
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgSpendFromCommons", "/cosmos.gov.v1.MsgVote", "/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(10000000000),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2, // Golden share: founder + parent council oversight
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers, moduleAddr)

	techGovPolicy := k.createExtendedGroup(ctx, GroupConfig{
		Name:                 "Technical Governance Committee",
		Description:          "Membership management for Tech",
		FundingWeight:        0,
		ParentPolicy:         techPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowGovernance,           // 5 Days
		StandardMinExecution: TechMembershipMinExecution, // 7 Days
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(0),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2, // Golden share: founder + parent council oversight
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers, moduleAddr)

	if err := k.SetElectoralDelegation(ctx, "Technical Council", techGovPolicy); err != nil {
		panic(err)
	}

	// --- B. ECOSYSTEM COUNCIL ---
	ecoMembers := []group.MemberRequest{}
	ecoMembers = append(ecoMembers, group.MemberRequest{
		Address:  founderMembers[0].Address,
		Weight:   "3",
		Metadata: "Founder",
	})
	// Golden Share: Commons Council
	ecoMembers = append(ecoMembers, group.MemberRequest{
		Address:  commonsPolicy,
		Weight:   "3",
		Metadata: "Guardian Veto (Commons Council)",
	})

	ecoConfig := GroupConfig{
		Name:          "Ecosystem Council",
		Description:   "Treasury & Growth (Top Level)",
		FundingWeight: 20,
		ParentPolicy:  govAddr,

		PolicyType:           "percentage",
		StandardValue:        "0.66",                         // Consensus Required
		StandardWindow:       WindowCouncil,                  // 5 Days
		StandardMinExecution: EcoCouncilStandardMinExecution, // 3 Days

		VetoValue:        "0.49", // UNILATERAL DEFENSE: 0.49 allows EITHER member (Weight 3/6) to Trigger Veto
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
		// RESTRICTED: Same as Tech Council, Veto can ONLY Cancel or Delete a rogue child group.
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
	ecoPolicy := k.createExtendedGroup(ctx, ecoConfig, ecoMembers, moduleAddr)

	// Eco Committees
	k.createExtendedGroup(ctx, GroupConfig{
		Name:                 "Ecosystem Operations Committee",
		Description:          "Operational arm for Ecosystem",
		FundingWeight:        0,
		ParentPolicy:         ecoPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowCommittee,    // 3 Days
		StandardMinExecution: EcoOpsMinExecution, // 1 Day
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgSpendFromCommons", "/sparkdream.commons.v1.MsgUpdateGroupConfig", "/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(10000000000),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2, // Golden share: founder + parent council oversight
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers, moduleAddr)

	ecoGovPolicy := k.createExtendedGroup(ctx, GroupConfig{
		Name:                 "Ecosystem Governance Committee",
		Description:          "Membership management for Ecosystem",
		FundingWeight:        0,
		ParentPolicy:         ecoPolicy,
		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowGovernance,          // 5 Days
		StandardMinExecution: EcoMembershipMinExecution, // 7 Days
		StandardPermissions:  []string{"/sparkdream.commons.v1.MsgUpdateGroupMembers"},
		MaxSpendPerEpoch:     math.NewInt(0),
		UpdateCooldown:       int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:      false,
		MinMembers:           2, // Golden share: founder + parent council oversight
		MaxMembers:           5,
		TermDuration:         TermDuration5Months,
	}, founderMembers, moduleAddr)

	if err := k.SetElectoralDelegation(ctx, "Ecosystem Council", ecoGovPolicy); err != nil {
		panic(err)
	}

	// =========================================================================
	// STEP 3: CREATE THE COMMONS SUPERVISORY BOARD
	// =========================================================================

	var supervisorMembers []group.MemberRequest
	supervisorMembers = append(supervisorMembers, group.MemberRequest{Address: techPolicy, Weight: "1", Metadata: "Tech Guardian"})
	supervisorMembers = append(supervisorMembers, group.MemberRequest{Address: ecoPolicy, Weight: "1", Metadata: "Eco Guardian"})

	supervisorPolicy := k.createExtendedGroup(ctx, GroupConfig{
		Name:          "Commons Supervisory Board",
		Description:   "Oversees the Commons Governance Committee",
		FundingWeight: 0,
		ParentPolicy:  govAddr, // Ultimate fallback to x/gov

		PolicyType:           "threshold",
		StandardValue:        "2",               // Both Tech and Eco must agree to fire the Recruiters
		StandardWindow:       WindowSupervisory, // 15 Days
		StandardMinExecution: 24 * time.Hour,    // 1 Day (Fast execution once consensus reached)

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
	}, supervisorMembers, moduleAddr)

	// =========================================================================
	// STEP 4: CREATE & WIRE COMMONS COMMITTEES
	// =========================================================================

	// -- A. Commons Operations (Spenders) --
	commOpsPolicy := k.createExtendedGroup(ctx, GroupConfig{
		Name:          "Commons Operations Committee",
		Description:   "Day-to-day spending for Commons",
		FundingWeight: 0,
		ParentPolicy:  commonsPolicy,

		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowCommittee,        // 3 Days
		StandardMinExecution: CommonsOpsMinExecution, // 1 Day

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgSpendFromCommons",
			"/sparkdream.commons.v1.MsgUpdateGroupMembers", // Self-Manage
		},
		MaxSpendPerEpoch: math.NewInt(10000000000),
		UpdateCooldown:   int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       2, // Golden share: founder + parent council oversight
		MaxMembers:       5,
		TermDuration:     TermDuration5Months,
	}, founderMembers, moduleAddr)

	if err := k.SetElectoralDelegation(ctx, "Commons Operations Committee", commOpsPolicy); err != nil {
		panic(err)
	}

	// -- B. Commons Governance (HR/Gatekeepers) --
	commonsGovPolicy := k.createExtendedGroup(ctx, GroupConfig{
		Name:          "Commons Governance Committee",
		Description:   "Membership management for Commons (HR)",
		FundingWeight: 0,
		ParentPolicy:  supervisorPolicy,

		PolicyType:           "threshold",
		StandardValue:        "1",
		StandardWindow:       WindowGovernance,              // 5 Days
		StandardMinExecution: CommonsMembershipMinExecution, // 21 Days

		StandardPermissions: []string{
			"/sparkdream.commons.v1.MsgUpdateGroupMembers",
		},
		MaxSpendPerEpoch: math.NewInt(0),
		UpdateCooldown:   int64(CommitteeUpdateCooldown.Seconds()),
		FutarchyEnabled:  false,
		MinMembers:       2, // Golden share: founder + parent council oversight
		MaxMembers:       5,
		TermDuration:     TermDuration5Months,
	}, founderMembers, moduleAddr)

	// WIRING: The HR Committee controls the Council
	if err := k.SetElectoralDelegation(ctx, "Commons Council", commonsGovPolicy); err != nil {
		panic(err)
	}

	logger.Info("Bootstrap Complete. Supervisory Structure Active.")
}

// Helper to wire up delegations safely
func (k Keeper) SetElectoralDelegation(ctx context.Context, parentName string, childPolicy string) error {
	group, err := k.ExtendedGroup.Get(ctx, parentName)
	if err != nil {
		return err
	}
	group.ElectoralPolicyAddress = childPolicy
	return k.ExtendedGroup.Set(ctx, parentName, group)
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

func (k Keeper) createExtendedGroup(ctx context.Context, cfg GroupConfig, members []group.MemberRequest, adminAddr string) string {

	// 1. Create Group
	groupRes, err := k.groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    adminAddr,
		Members:  members,
		Metadata: cfg.Description,
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to create group %s: %v", cfg.Name, err))
	}

	// 2. Create Standard Policy
	var stdPolicy group.DecisionPolicy
	if cfg.PolicyType == "percentage" {
		stdPolicy = group.NewPercentageDecisionPolicy(cfg.StandardValue, cfg.StandardWindow, cfg.StandardMinExecution)
	} else {
		stdPolicy = &group.ThresholdDecisionPolicy{
			Threshold: cfg.StandardValue,
			Windows: &group.DecisionPolicyWindows{
				VotingPeriod:       cfg.StandardWindow,
				MinExecutionPeriod: cfg.StandardMinExecution,
			},
		}
	}

	stdAny, _ := codectypes.NewAnyWithValue(stdPolicy)
	stdRes, err := k.groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          adminAddr,
		GroupId:        groupRes.GroupId,
		Metadata:       "standard",
		DecisionPolicy: stdAny,
	})
	if err != nil {
		panic(err)
	}
	mainPolicyAddr := stdRes.Address

	// 3. Register Standard Permissions
	if err := k.PolicyPermissions.Set(ctx, mainPolicyAddr, types.PolicyPermissions{
		PolicyAddress:   mainPolicyAddr,
		AllowedMessages: cfg.StandardPermissions,
	}); err != nil {
		panic(err)
	}

	// 4. Create Veto Policy
	if len(cfg.VetoPermissions) > 0 {
		var vetoPolicy group.DecisionPolicy
		if cfg.PolicyType == "percentage" {
			vetoPolicy = group.NewPercentageDecisionPolicy(cfg.VetoValue, cfg.VetoWindow, cfg.VetoMinExecution)
		} else {
			vetoPolicy = &group.ThresholdDecisionPolicy{
				Threshold: cfg.VetoValue,
				Windows: &group.DecisionPolicyWindows{
					VotingPeriod:       cfg.VetoWindow,
					MinExecutionPeriod: cfg.VetoMinExecution,
				},
			}
		}

		vetoAny, _ := codectypes.NewAnyWithValue(vetoPolicy)
		vetoRes, err := k.groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
			Admin:          adminAddr,
			GroupId:        groupRes.GroupId,
			Metadata:       "veto",
			DecisionPolicy: vetoAny,
		})
		if err != nil {
			panic(err)
		}

		if err := k.PolicyPermissions.Set(ctx, vetoRes.Address, types.PolicyPermissions{
			PolicyAddress:   vetoRes.Address,
			AllowedMessages: cfg.VetoPermissions,
		}); err != nil {
			panic(err)
		}
	}

	// 5. Funding & Metadata
	if cfg.FundingWeight > 0 {
		k.splitKeeper.SetShareByAddress(ctx, mainPolicyAddr, cfg.FundingWeight)
	}

	extendedGroup := types.ExtendedGroup{
		GroupId:             groupRes.GroupId,
		PolicyAddress:       mainPolicyAddr,
		ParentPolicyAddress: cfg.ParentPolicy,
		FundingWeight:       cfg.FundingWeight,

		// Use Config values
		MaxSpendPerEpoch:      &cfg.MaxSpendPerEpoch,
		UpdateCooldown:        cfg.UpdateCooldown,
		FutarchyEnabled:       cfg.FutarchyEnabled,
		MinMembers:            cfg.MinMembers,
		MaxMembers:            cfg.MaxMembers,
		TermDuration:          cfg.TermDuration,
		CurrentTermExpiration: sdk.UnwrapSDKContext(ctx).BlockTime().Unix() + cfg.TermDuration,
		ActivationTime:        0,
	}

	if err := k.ExtendedGroup.Set(ctx, cfg.Name, extendedGroup); err != nil {
		panic(err)
	}

	// Populate the Index so these groups can be found by Policy Address
	if err := k.PolicyToName.Set(ctx, mainPolicyAddr, cfg.Name); err != nil {
		panic(err)
	}

	return mainPolicyAddr
}

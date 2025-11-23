package keeper

import (
	"context"
	"fmt"
	"time"

	"sparkdream/x/commons/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/group"
)

// GenesisNames maps genesis addresses (from config.yml) to human-readable names.
var GenesisNames = map[string]string{
	"sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan": "Alice",
	"sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y": "Bob",
	"sprkdrm1a0gkdyzcnsjrl2s5vlywkancparhp53fucz3zz": "Carol",
}

// BootstrapCommonsCouncil automatically creates the Council Group and Policies at Genesis.
func (k Keeper) BootstrapCommonsCouncil(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("module", "x/commons")
	logger.Info("Bootstrapping Commons Council from Genesis Accounts...")

	var members []group.MemberRequest
	var tempAdmin string

	// 1. Iterate over ALL accounts to find Genesis Users
	k.authKeeper.IterateAccounts(ctx, func(acc sdk.AccountI) bool {
		// Filter: Skip Module Accounts (We only want users)
		if _, ok := acc.(sdk.ModuleAccountI); ok {
			return false
		}

		address := acc.GetAddress().String()

		// Determine Metadata Name from our map, or fallback
		name, isKnown := GenesisNames[address]
		if !isKnown {
			name = "Genesis Member"
		}

		// Add to Group
		members = append(members, group.MemberRequest{
			Address:  address,
			Weight:   "1",
			Metadata: name,
		})

		// Pick the first user found as the temporary admin for creation
		if tempAdmin == "" {
			tempAdmin = address
		}

		return false // Continue iteration
	})

	if len(members) == 0 {
		logger.Error("No user accounts found in Genesis! Skipping Commons Council bootstrap.")
		return
	}

	logger.Info(fmt.Sprintf("Found %d genesis users. Creating Group...", len(members)))

	// 2. Create Group
	groupID, err := k.groupKeeper.CreateGroup(ctx, &group.MsgCreateGroup{
		Admin:    tempAdmin,
		Members:  members,
		Metadata: "Commons Council",
	})
	if err != nil {
		// If group 1 already exists (e.g. node restart without reset), log and skip
		logger.Info("Commons Council Group creation skipped (likely exists)", "err", err)
		return
	}

	// 3. Create Standard Policy (25%, 30s Voting Period)
	stdPolicy := group.NewPercentageDecisionPolicy("0.25", time.Second*30, 0)
	stdPolicyAny, _ := codectypes.NewAnyWithValue(stdPolicy)

	policyRes, err := k.groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          tempAdmin,
		GroupId:        groupID.GroupId,
		Metadata:       "standard",
		DecisionPolicy: stdPolicyAny,
	})
	if err != nil {
		panic(err)
	}
	standardAddr := policyRes.Address

	// 4. Create Veto Policy (50%, 10s Voting Period)
	vetoPolicy := group.NewPercentageDecisionPolicy("0.50", time.Second*10, 0)
	vetoPolicyAny, _ := codectypes.NewAnyWithValue(vetoPolicy)

	_, err = k.groupKeeper.CreateGroupPolicy(ctx, &group.MsgCreateGroupPolicy{
		Admin:          tempAdmin,
		GroupId:        groupID.GroupId,
		Metadata:       "veto",
		DecisionPolicy: vetoPolicyAny,
	})
	if err != nil {
		panic(err)
	}

	// 5. Set Commons Module Params (The Handover)
	params := types.DefaultParams()
	params.CommonsCouncilAddress = standardAddr
	if err := k.SetParams(ctx, params); err != nil {
		panic(err)
	}

	// 6. Secure the Group (Update Admin to Standard Policy)
	// This prevents the tempAdmin (Alice) from adding rogue policies later.
	_, err = k.groupKeeper.UpdateGroupAdmin(ctx, &group.MsgUpdateGroupAdmin{
		Admin:    tempAdmin,
		GroupId:  groupID.GroupId,
		NewAdmin: standardAddr,
	})
	if err != nil {
		panic(err)
	}

	logger.Info("BOOTSTRAP COMPLETE: Commons Council Initialized", "Address", standardAddr)
}

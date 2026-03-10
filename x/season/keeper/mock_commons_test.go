package keeper_test

import (
	"context"

	commonstypes "sparkdream/x/commons/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// mockCommonsKeeper mocks the commons keeper for testing gamification authorization
type mockCommonsKeeper struct {
	// IsCommitteeMemberFn can be set to control committee membership checks
	IsCommitteeMemberFn func(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)
	// GetGroupFn can be set to control group lookups (e.g., Commons Council)
	GetGroupFn func(ctx context.Context, name string) (commonstypes.Group, error)
	// IsCouncilAuthorizedFn can be set to control council authorization checks
	IsCouncilAuthorizedFn func(ctx context.Context, addr string, council string, committee string) bool
}

func (m *mockCommonsKeeper) IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
	if m.IsCommitteeMemberFn != nil {
		return m.IsCommitteeMemberFn(ctx, address, council, committee)
	}
	return false, nil
}

func (m *mockCommonsKeeper) GetGroup(ctx context.Context, name string) (commonstypes.Group, error) {
	if m.GetGroupFn != nil {
		return m.GetGroupFn(ctx, name)
	}
	return commonstypes.Group{}, nil
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if m.IsCouncilAuthorizedFn != nil {
		return m.IsCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	return false
}

// newMockCommonsKeeper creates a mock that allows the specified addresses to manage gamification
func newMockCommonsKeeper(authorizedAddresses ...string) *mockCommonsKeeper {
	authorizedSet := make(map[string]bool)
	for _, addr := range authorizedAddresses {
		authorizedSet[addr] = true
	}

	return &mockCommonsKeeper{
		IsCommitteeMemberFn: func(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
			// Check if the address is in our authorized set
			return authorizedSet[address.String()], nil
		},
		GetGroupFn: func(ctx context.Context, name string) (commonstypes.Group, error) {
			// Return a mock Commons Council with no special policy address
			return commonstypes.Group{
				Index:         name,
				PolicyAddress: "", // No special policy address unless set
			}, nil
		},
		IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
			return authorizedSet[addr]
		},
	}
}

// newMockCommonsKeeperWithCouncil creates a mock with a specific Commons Council policy address
func newMockCommonsKeeperWithCouncil(councilPolicyAddr string, authorizedAddresses ...string) *mockCommonsKeeper {
	authorizedSet := make(map[string]bool)
	for _, addr := range authorizedAddresses {
		authorizedSet[addr] = true
	}

	return &mockCommonsKeeper{
		IsCommitteeMemberFn: func(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
			return authorizedSet[address.String()], nil
		},
		GetGroupFn: func(ctx context.Context, name string) (commonstypes.Group, error) {
			if name == "Commons Council" {
				return commonstypes.Group{
					Index:         name,
					PolicyAddress: councilPolicyAddr,
				}, nil
			}
			return commonstypes.Group{Index: name}, nil
		},
		IsCouncilAuthorizedFn: func(ctx context.Context, addr string, council string, committee string) bool {
			if addr == councilPolicyAddr {
				return true
			}
			return authorizedSet[addr]
		},
	}
}

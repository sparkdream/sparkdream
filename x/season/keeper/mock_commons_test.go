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
	// GetExtendedGroupFn can be set to control group lookups (e.g., Commons Council)
	GetExtendedGroupFn func(ctx context.Context, name string) (commonstypes.ExtendedGroup, error)
}

func (m *mockCommonsKeeper) IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
	if m.IsCommitteeMemberFn != nil {
		return m.IsCommitteeMemberFn(ctx, address, council, committee)
	}
	return false, nil
}

func (m *mockCommonsKeeper) GetExtendedGroup(ctx context.Context, name string) (commonstypes.ExtendedGroup, error) {
	if m.GetExtendedGroupFn != nil {
		return m.GetExtendedGroupFn(ctx, name)
	}
	return commonstypes.ExtendedGroup{}, nil
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
		GetExtendedGroupFn: func(ctx context.Context, name string) (commonstypes.ExtendedGroup, error) {
			// Return a mock Commons Council with no special policy address
			return commonstypes.ExtendedGroup{
				Index:         name,
				PolicyAddress: "", // No special policy address unless set
			}, nil
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
		GetExtendedGroupFn: func(ctx context.Context, name string) (commonstypes.ExtendedGroup, error) {
			if name == "Commons Council" {
				return commonstypes.ExtendedGroup{
					Index:         name,
					PolicyAddress: councilPolicyAddr,
				}, nil
			}
			return commonstypes.ExtendedGroup{Index: name}, nil
		},
	}
}

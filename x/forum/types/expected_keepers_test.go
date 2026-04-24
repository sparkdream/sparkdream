package types_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// stubAuthKeeper / stubBankKeeper / stubCommonsKeeper / stubRepKeeper exist
// only to prove that the expected-keeper interfaces are implementable. If the
// interfaces ever change shape, this file stops compiling and the test fails.

type stubAuthKeeper struct{}

func (stubAuthKeeper) AddressCodec() address.Codec                               { return nil }
func (stubAuthKeeper) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI    { return nil }

type stubBankKeeper struct{}

func (stubBankKeeper) SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (stubBankKeeper) SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (stubBankKeeper) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}
func (stubBankKeeper) SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (stubBankKeeper) SendCoinsFromModuleToModule(context.Context, string, string, sdk.Coins) error {
	return nil
}
func (stubBankKeeper) BurnCoins(context.Context, string, sdk.Coins) error { return nil }
func (stubBankKeeper) MintCoins(context.Context, string, sdk.Coins) error { return nil }

type stubCommonsKeeper struct{}

func (stubCommonsKeeper) IsGroupPolicyMember(context.Context, string, string) (bool, error) {
	return false, nil
}
func (stubCommonsKeeper) IsGroupPolicyAddress(context.Context, string) bool { return false }
func (stubCommonsKeeper) IsCouncilAuthorized(context.Context, string, string, string) bool {
	return false
}
func (stubCommonsKeeper) GetCategory(context.Context, uint64) (commonstypes.Category, bool) {
	return commonstypes.Category{}, false
}
func (stubCommonsKeeper) HasCategory(context.Context, uint64) bool { return false }

type stubRepKeeper struct{}

func (stubRepKeeper) MintDREAM(context.Context, sdk.AccAddress, math.Int) error   { return nil }
func (stubRepKeeper) BurnDREAM(context.Context, sdk.AccAddress, math.Int) error   { return nil }
func (stubRepKeeper) LockDREAM(context.Context, sdk.AccAddress, math.Int) error   { return nil }
func (stubRepKeeper) UnlockDREAM(context.Context, sdk.AccAddress, math.Int) error { return nil }
func (stubRepKeeper) GetBalance(context.Context, sdk.AccAddress) (math.Int, error) {
	return math.ZeroInt(), nil
}
func (stubRepKeeper) TransferDREAM(context.Context, sdk.AccAddress, sdk.AccAddress, math.Int, reptypes.TransferPurpose) error {
	return nil
}
func (stubRepKeeper) IsMember(context.Context, sdk.AccAddress) bool       { return false }
func (stubRepKeeper) IsActiveMember(context.Context, sdk.AccAddress) bool { return false }
func (stubRepKeeper) GetMember(context.Context, sdk.AccAddress) (reptypes.Member, error) {
	return reptypes.Member{}, nil
}
func (stubRepKeeper) GetTrustLevel(context.Context, sdk.AccAddress) (reptypes.TrustLevel, error) {
	return 0, nil
}
func (stubRepKeeper) GetReputationTier(context.Context, sdk.AccAddress) (uint64, error) {
	return 0, nil
}
func (stubRepKeeper) ZeroMember(context.Context, sdk.AccAddress, string) error   { return nil }
func (stubRepKeeper) DemoteMember(context.Context, sdk.AccAddress, string) error { return nil }
func (stubRepKeeper) CreateAppealInitiative(context.Context, string, []byte, int64) (uint64, error) {
	return 0, nil
}
func (stubRepKeeper) GetContentConviction(context.Context, reptypes.StakeTargetType, uint64) (math.LegacyDec, error) {
	return math.LegacyZeroDec(), nil
}
func (stubRepKeeper) CreateAuthorBond(context.Context, sdk.AccAddress, reptypes.StakeTargetType, uint64, math.Int) (uint64, error) {
	return 0, nil
}
func (stubRepKeeper) SlashAuthorBond(context.Context, reptypes.StakeTargetType, uint64) error {
	return nil
}
func (stubRepKeeper) ValidateInitiativeReference(context.Context, uint64) error { return nil }
func (stubRepKeeper) RegisterContentInitiativeLink(context.Context, uint64, int32, uint64) error {
	return nil
}
func (stubRepKeeper) RemoveContentInitiativeLink(context.Context, uint64, int32, uint64) error {
	return nil
}
func (stubRepKeeper) TagExists(context.Context, string) (bool, error)      { return false, nil }
func (stubRepKeeper) IsReservedTag(context.Context, string) (bool, error)  { return false, nil }
func (stubRepKeeper) GetTag(context.Context, string) (reptypes.Tag, error) { return reptypes.Tag{}, nil }
func (stubRepKeeper) IncrementTagUsage(context.Context, string, int64) error { return nil }
func (stubRepKeeper) SetReservedTag(context.Context, reptypes.ReservedTag) error {
	return nil
}
func (stubRepKeeper) GetSalvationCounters(context.Context, string) (uint32, int64, error) {
	return 0, 0, nil
}
func (stubRepKeeper) UpdateSalvationCounters(context.Context, string, uint32, int64) error {
	return nil
}
func (stubRepKeeper) GetBondedRole(context.Context, reptypes.RoleType, string) (reptypes.BondedRole, error) {
	return reptypes.BondedRole{}, nil
}
func (stubRepKeeper) ReserveBond(context.Context, reptypes.RoleType, string, math.Int) error { return nil }
func (stubRepKeeper) RecordActivity(context.Context, reptypes.RoleType, string) error        { return nil }
func (stubRepKeeper) SetBondStatus(context.Context, reptypes.RoleType, string, reptypes.BondedRoleStatus, int64) error {
	return nil
}
func (stubRepKeeper) SetBondedRoleConfig(context.Context, reptypes.BondedRoleConfig) error { return nil }

func TestExpectedKeepersImplementable(t *testing.T) {
	var (
		_ types.AuthKeeper    = stubAuthKeeper{}
		_ types.BankKeeper    = stubBankKeeper{}
		_ types.CommonsKeeper = stubCommonsKeeper{}
		_ types.RepKeeper     = stubRepKeeper{}
	)
}

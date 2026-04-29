package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"

	"sparkdream/x/rep/types"
)

// Sub-address keys partition uspark escrows held by x/rep into independent
// pools so distribution / burn / refund paths never see funds belonging to
// another pool. Mirrors x/commons' DeriveCouncilAddress pattern: each address
// is `sdkaddress.Module("rep", []byte(key))`, lives in bank like a regular
// account, and the partition is enforced by bank itself.
const (
	subAddrKeySentinelRewards = "sentinel_rewards"
	subAddrKeyTagBudgets      = "tag_budgets"
	subAddrKeyAppealBonds     = "appeal_bonds"
)

// SentinelRewardPoolAddress returns the deterministic address that holds the
// sentinel reward pool's uspark balance.
func SentinelRewardPoolAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeySentinelRewards))
}

// TagBudgetEscrowAddress returns the deterministic address that holds all
// tag-budget uspark escrows. Per-budget accounting stays on the TagBudget
// record's PoolBalance field.
func TagBudgetEscrowAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeyTagBudgets))
}

// AppealBondEscrowAddress returns the deterministic address that holds all
// gov-action appeal bond escrows. Per-appeal accounting stays on the appeal
// record's AppealBond field.
func AppealBondEscrowAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeyAppealBonds))
}

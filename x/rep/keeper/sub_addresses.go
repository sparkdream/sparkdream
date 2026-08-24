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
	subAddrKeySentinelRewards  = "sentinel_rewards"
	subAddrKeyTagBudgets       = "tag_budgets"
	subAddrKeyAppealBonds      = "appeal_bonds"
	subAddrKeyReviewerRewards  = "reviewer_rewards"
	subAddrKeyCuratorRewards   = "curator_rewards"
	subAddrKeyRoleRewardIntake = "role_reward_intake"
)

// SentinelRewardPoolAddress returns the deterministic address that holds the
// sentinel reward pool's uspark balance.
func SentinelRewardPoolAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeySentinelRewards))
}

// ReviewerRewardPoolAddress returns the deterministic address holding the
// initiative-reviewer reward pool's uspark balance.
//
// Kept separate from the sentinel pool for the same reason the roles are
// separate: a wrong approval mints DREAM where a wrong hide costs a post some
// visibility, so the two must never draw on each other's funds. Filled
// automatically by FundRoleRewardPools; like the other sub-addresses it is also
// an ordinary bank account, so a council can top it up with a plain send —
// no bespoke funding message is required either way.
func ReviewerRewardPoolAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeyReviewerRewards))
}

// CuratorRewardPoolAddress returns the deterministic address holding the
// collect-curator reward pool's uspark balance.
//
// Sized to match the sentinel pool rather than the curator's smaller bond:
// rating a collection and hiding a post are comparable judgment calls. It is
// still a separate pool, so the two cannot cross-subsidise and each keeps its
// own accuracy bar.
func CuratorRewardPoolAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeyCuratorRewards))
}

// RoleRewardIntakeAddress returns the deterministic address that receives
// x/rep's automatic community-pool allocation before it is divided among the
// bonded-role reward pools.
//
// One intake rather than a skim per role: the community pool should see a
// single, capped claim from x/rep, and adding a fourth bonded role should not
// mean adding a fourth funding line. Division happens internally, by headroom.
func RoleRewardIntakeAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeyRoleRewardIntake))
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

package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
)

// Cross-module integration with x/rep
// These methods delegate to the RepKeeper for DREAM token operations and member management

// IsGovAuthority checks if the given address is the governance authority.
func (k Keeper) IsGovAuthority(ctx context.Context, addr string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.authority, addrBytes)
}

// IsMember checks if the given address is a registered member via x/rep.
func (k Keeper) IsMember(ctx context.Context, addr string) bool {
	if k.repKeeper == nil {
		return true // Fallback: permissive mode when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return k.repKeeper.IsMember(ctx, sdk.AccAddress(addrBytes))
}

// IsActiveMember checks if the given address is an active member (not zeroed).
func (k Keeper) IsActiveMember(ctx context.Context, addr string) bool {
	if k.repKeeper == nil {
		return true // Fallback: permissive mode when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return k.repKeeper.IsActiveMember(ctx, sdk.AccAddress(addrBytes))
}

// GetRepTier returns the reputation tier (0-5) for a user via x/rep.
func (k Keeper) GetRepTier(ctx context.Context, addr string) uint64 {
	if k.repKeeper == nil {
		return 5 // Fallback: high tier when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return 0
	}
	tier, err := k.repKeeper.GetReputationTier(ctx, sdk.AccAddress(addrBytes))
	if err != nil {
		return 0
	}
	return tier
}

// GetMemberSince returns the timestamp when a user became a member via x/rep.
func (k Keeper) GetMemberSince(ctx context.Context, addr string) int64 {
	if k.repKeeper == nil {
		return 0 // Fallback: member since genesis
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return 0
	}
	member, err := k.repKeeper.GetMember(ctx, sdk.AccAddress(addrBytes))
	if err != nil {
		return 0
	}
	return member.JoinedAt
}

// GetSentinelBond returns the DREAM bond amount for a sentinel from their staked balance.
func (k Keeper) GetSentinelBond(ctx context.Context, addr string) math.Int {
	if k.repKeeper == nil {
		return math.NewInt(2000) // Fallback: high value when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return math.NewInt(0)
	}
	member, err := k.repKeeper.GetMember(ctx, sdk.AccAddress(addrBytes))
	if err != nil {
		return math.NewInt(0)
	}
	if member.StakedDream == nil {
		return math.NewInt(0)
	}
	return *member.StakedDream
}

// GetSentinelBacking returns the DREAM backing amount (total balance) for a sentinel.
func (k Keeper) GetSentinelBacking(ctx context.Context, addr string) math.Int {
	if k.repKeeper == nil {
		return math.NewInt(20000) // Fallback: high value when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return math.NewInt(0)
	}
	balance, err := k.repKeeper.GetBalance(ctx, sdk.AccAddress(addrBytes))
	if err != nil {
		return math.NewInt(0)
	}
	return balance
}

// IsGroupMember checks if addr is a member of the group identified by groupAddr (policy address).
// Integrates with x/commons to verify group membership via policy address.
func (k Keeper) IsGroupMember(ctx context.Context, groupAddr, addr string) bool {
	if k.commonsKeeper == nil {
		return true // Fallback: permissive mode when x/commons not wired
	}
	isMember, err := k.commonsKeeper.IsGroupPolicyMember(ctx, groupAddr, addr)
	if err != nil {
		return false
	}
	return isMember
}

// IsGroupAccount checks if the given address is a valid group policy account.
// Integrates with x/commons to verify the address is a known group policy.
func (k Keeper) IsGroupAccount(ctx context.Context, addr string) bool {
	if k.commonsKeeper == nil {
		return true // Fallback: permissive mode when x/commons not wired
	}
	return k.commonsKeeper.IsGroupPolicyAddress(ctx, addr)
}

// IsCouncilAuthorized checks if the address is authorized via governance authority,
// council policy address, or committee membership.
// Delegates to x/commons IsCouncilAuthorized when available.
// Falls back to IsGovAuthority when x/commons is not wired.
func (k Keeper) IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if k.commonsKeeper == nil {
		return k.IsGovAuthority(ctx, addr)
	}
	return k.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// CreateAppealInitiative creates an x/rep initiative for jury-based appeal resolution.
// initiativeType: type of appeal ("moderation_appeal", "sentinel_appeal", etc.)
// payload: JSON-encoded appeal data containing case details
// deadline: block height by which the appeal must be resolved
// Returns the initiative ID or error.
func (k Keeper) CreateAppealInitiative(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error) {
	if k.repKeeper == nil {
		return 0, fmt.Errorf("x/rep keeper not available for appeal creation")
	}
	return k.repKeeper.CreateAppealInitiative(ctx, initiativeType, payload, deadline)
}

// SlashSentinelBond slashes DREAM from a sentinel's staked balance via x/rep.
func (k Keeper) SlashSentinelBond(ctx context.Context, addr string, amount math.Int) error {
	if k.repKeeper == nil {
		return nil // No-op when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	// First unlock the staked DREAM, then burn it
	if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(addrBytes), amount); err != nil {
		return err
	}
	return k.repKeeper.BurnDREAM(ctx, sdk.AccAddress(addrBytes), amount)
}

// MintDREAM mints DREAM tokens to a member via x/rep.
func (k Keeper) MintDREAM(ctx context.Context, addr string, amount math.Int) error {
	if k.repKeeper == nil {
		return nil // No-op when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return err
	}
	return k.repKeeper.MintDREAM(ctx, sdk.AccAddress(addrBytes), amount)
}

// TransferDREAM transfers DREAM tokens between addresses via x/rep.
// For forum operations (bonds, escrow), we use LockDREAM/UnlockDREAM pattern.
func (k Keeper) TransferDREAM(ctx context.Context, from, to string, amount math.Int) error {
	if k.repKeeper == nil {
		return nil // No-op when x/rep not wired
	}
	fromBytes, err := k.addressCodec.StringToBytes(from)
	if err != nil {
		return err
	}

	// For forum bonds: lock DREAM on the sender (moves to staked balance)
	// The "to" address is typically the module address for escrow purposes
	// We lock on sender rather than transferring to module account
	moduleAddr := k.GetModuleAddress()
	if to == moduleAddr {
		// Locking for escrow (bond/stake)
		return k.repKeeper.LockDREAM(ctx, sdk.AccAddress(fromBytes), amount)
	}

	// Unlocking from escrow (refund)
	if from == moduleAddr {
		toBytes, err := k.addressCodec.StringToBytes(to)
		if err != nil {
			return err
		}
		return k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(toBytes), amount)
	}

	// Direct transfer between members (for rewards/tips)
	toBytes, err := k.addressCodec.StringToBytes(to)
	if err != nil {
		return err
	}
	return k.repKeeper.TransferDREAM(ctx, sdk.AccAddress(fromBytes), sdk.AccAddress(toBytes), amount, reptypes.TransferPurpose_TRANSFER_PURPOSE_TIP)
}

// GetBackerMembershipDuration returns how long a backer has been a member.
func (k Keeper) GetBackerMembershipDuration(ctx context.Context, backerAddr string) int64 {
	if k.repKeeper == nil {
		return 31536000 // Fallback: 1 year when x/rep not wired
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	joinedAt := k.GetMemberSince(ctx, backerAddr)
	if joinedAt == 0 {
		return 31536000 // Default if not found
	}
	return sdkCtx.BlockTime().Unix() - joinedAt
}

// DemoteMember demotes a member's reputation via x/rep.
// Note: Per spec, trust levels never decrease. "Demotion" is a reputation slash.
func (k Keeper) DemoteMember(ctx context.Context, member string, reason string) error {
	if k.repKeeper == nil {
		return nil // No-op when x/rep not wired
	}
	memberBytes, err := k.addressCodec.StringToBytes(member)
	if err != nil {
		return err
	}
	return k.repKeeper.DemoteMember(ctx, sdk.AccAddress(memberBytes), reason)
}

// ZeroMember zeros out a member's reputation and DREAM via x/rep.
// This is the harshest penalty - burns all DREAM and zeros all reputation.
func (k Keeper) ZeroMember(ctx context.Context, member string, reason string) error {
	if k.repKeeper == nil {
		return nil // No-op when x/rep not wired
	}
	memberBytes, err := k.addressCodec.StringToBytes(member)
	if err != nil {
		return err
	}
	return k.repKeeper.ZeroMember(ctx, sdk.AccAddress(memberBytes), reason)
}

// RefundBonds refunds DREAM bonds to a list of addresses by unlocking their staked DREAM.
func (k Keeper) RefundBonds(ctx context.Context, recipients []string, totalAmount math.Int) error {
	if k.repKeeper == nil {
		return nil // No-op when x/rep not wired
	}
	if len(recipients) == 0 || totalAmount.IsZero() {
		return nil
	}

	// Distribute evenly among recipients
	amountPerRecipient := totalAmount.Quo(math.NewInt(int64(len(recipients))))
	if amountPerRecipient.IsZero() {
		return nil
	}

	for _, recipient := range recipients {
		recipientBytes, err := k.addressCodec.StringToBytes(recipient)
		if err != nil {
			continue // Skip invalid addresses
		}
		// Unlock staked DREAM back to available balance
		if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(recipientBytes), amountPerRecipient); err != nil {
			// Log but don't fail - some recipients may not have enough staked
			continue
		}
	}

	return nil
}

// GetModuleAddress returns the forum module account address (authority).
func (k Keeper) GetModuleAddress() string {
	addr, _ := k.addressCodec.BytesToString(k.authority)
	return addr
}

// Helper functions for time-related operations

// GetBlockTime returns the current block time as Unix timestamp.
func (k Keeper) GetBlockTime(ctx context.Context) int64 {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.BlockTime().Unix()
}

// GetBlockHeight returns the current block height.
func (k Keeper) GetBlockHeight(ctx context.Context) int64 {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.BlockHeight()
}

// Helper functions for address validation

// ValidateAddress validates an address string and returns the bytes.
func (k Keeper) ValidateAddress(addr string) ([]byte, error) {
	return k.addressCodec.StringToBytes(addr)
}

// AddressToString converts address bytes to string.
func (k Keeper) AddressToString(addr []byte) (string, error) {
	return k.addressCodec.BytesToString(addr)
}

// GetAuthorityString returns the module authority as a string.
func (k Keeper) GetAuthorityString() string {
	addr, err := k.addressCodec.BytesToString(k.authority)
	if err != nil {
		return ""
	}
	return addr
}

// GetTrustLevel returns the trust level for a member via x/rep.
func (k Keeper) GetTrustLevel(ctx context.Context, addr string) uint64 {
	if k.repKeeper == nil {
		return uint64(reptypes.TrustLevel_TRUST_LEVEL_TRUSTED) // Fallback when x/rep not wired
	}
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return 0
	}
	trustLevel, err := k.repKeeper.GetTrustLevel(ctx, sdk.AccAddress(addrBytes))
	if err != nil {
		return 0
	}
	return uint64(trustLevel)
}

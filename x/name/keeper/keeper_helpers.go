package keeper

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// --- Params Helper ---

func (k Keeper) GetParams(ctx context.Context) types.Params {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.DefaultParams()
	}
	return params
}

// --- Dispute Helpers ---

func (k Keeper) SetDispute(ctx context.Context, dispute types.Dispute) error {
	return k.Disputes.Set(ctx, dispute.Name, dispute)
}

func (k Keeper) GetDispute(ctx context.Context, name string) (types.Dispute, bool) {
	dispute, err := k.Disputes.Get(ctx, name)
	if err != nil {
		return types.Dispute{}, false
	}
	return dispute, true
}

func (k Keeper) RemoveDispute(ctx context.Context, name string) error {
	return k.Disputes.Remove(ctx, name)
}

// --- Name Helpers ---

func (k Keeper) GetNameOwner(ctx context.Context, name string) (sdk.AccAddress, bool) {
	record, err := k.Names.Get(ctx, name)
	if err != nil {
		return nil, false
	}
	addr, err := sdk.AccAddressFromBech32(record.Owner)
	if err != nil {
		return nil, false
	}
	return addr, true
}

func (k Keeper) GetName(ctx context.Context, name string) (types.NameRecord, bool) {
	record, err := k.Names.Get(ctx, name)
	if err != nil {
		return types.NameRecord{}, false
	}
	return record, true
}

// IsNameAvailable returns true if the given name is not currently registered.
// Only collections.ErrNotFound means the name is free; other errors (e.g.
// storage faults) must not be treated as availability.
func (k Keeper) IsNameAvailable(ctx context.Context, name string) bool {
	_, err := k.Names.Get(ctx, name)
	return errors.Is(err, collections.ErrNotFound)
}

// ClaimName atomically checks availability and registers a name, preventing
// TOCTOU races. Unlike the MsgRegisterName handler, this skips fee collection,
// council membership checks, and scavenge logic — it is intended for
// cross-module programmatic registration (e.g., guild name reservation).
func (k Keeper) ClaimName(ctx context.Context, name string, owner string, data string) error {
	// Normalize and validate the name to match MsgRegisterName's rules.
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return types.ErrInvalidName
	}

	params := k.GetParams(ctx)
	if uint64(len(name)) < params.MinNameLength {
		return errorsmod.Wrapf(types.ErrInvalidName, "name too short (min %d)", params.MinNameLength)
	}
	if uint64(len(name)) > params.MaxNameLength {
		return errorsmod.Wrapf(types.ErrInvalidName, "name too long (max %d)", params.MaxNameLength)
	}
	if !validNameRegex.MatchString(name) {
		return errorsmod.Wrap(types.ErrInvalidName, "name contains invalid characters (allowed: a-z, 0-9, -; cannot start/end with -)")
	}

	if !k.IsNameAvailable(ctx, name) {
		return types.ErrNameTaken
	}

	// Check blocked names
	for _, blocked := range params.BlockedNames {
		if name == blocked {
			return types.ErrNameReserved
		}
	}

	ownerAddr, err := sdk.AccAddressFromBech32(owner)
	if err != nil {
		return err
	}

	// Check per-address limit
	count, err := k.GetOwnedNamesCount(ctx, ownerAddr)
	if err != nil {
		return err
	}
	if count >= params.MaxNamesPerAddress {
		return types.ErrTooManyNames
	}

	// Register atomically — check + write in one call
	record := types.NameRecord{
		Name:  name,
		Owner: owner,
		Data:  data,
	}
	if err := k.SetName(ctx, record); err != nil {
		return err
	}

	return k.AddNameToOwner(ctx, ownerAddr, name)
}

func (k Keeper) SetName(ctx context.Context, record types.NameRecord) error {
	return k.Names.Set(ctx, record.Name, record)
}

func (k Keeper) RemoveNameFromOwner(ctx context.Context, owner sdk.AccAddress, name string) error {
	// Remove from Secondary Index
	return k.OwnerNames.Remove(ctx, collections.Join(owner.String(), name))
}

func (k Keeper) AddNameToOwner(ctx context.Context, owner sdk.AccAddress, name string) error {
	// 1. Add to Secondary Index (Efficient Lookup)
	if err := k.OwnerNames.Set(ctx, collections.Join(owner.String(), name)); err != nil {
		return err
	}

	// 2. Ensure OwnerInfo exists for metadata (LastActiveTime, PrimaryName).
	// Acquiring or being assigned a name counts as owner activity, so seed
	// LastActiveTime on creation; otherwise refresh it on the existing record.
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	info, err := k.Owners.Get(ctx, owner.String())
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		info = types.OwnerInfo{Address: owner.String(), LastActiveTime: now}
		return k.Owners.Set(ctx, owner.String(), info)
	}
	info.LastActiveTime = now
	return k.Owners.Set(ctx, owner.String(), info)
}

// RecordOwnerActivity bumps the LastActiveTime on the OwnerInfo record for the
// given address. Called from every msg handler that represents the owner
// taking a public action; lets the scavenge logic (IsOwnerExpired) identify
// owners who have been silent past the expiration window.
func (k Keeper) RecordOwnerActivity(ctx context.Context, addr string) error {
	info, err := k.Owners.Get(ctx, addr)
	if err != nil {
		// No OwnerInfo yet (e.g. caller does not own a name): nothing to bump.
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	info.LastActiveTime = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	return k.Owners.Set(ctx, addr, info)
}

// --- Helper Implementations ---

// GetOwnedNamesCount counts how many names an address owns using the secondary index.
// Optimization: Uses O(M) prefix iteration where M is user's name count (max ~5).
func (k Keeper) GetOwnedNamesCount(ctx context.Context, owner sdk.AccAddress) (uint64, error) {
	var count uint64

	// Create a range that matches all pairs starting with owner address
	rng := collections.NewPrefixedPairRange[string, string](owner.String())

	// Walk only the relevant keys
	err := k.OwnerNames.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		count++
		return false, nil
	})

	return count, err
}

// SetPrimaryName sets the primary name in the OwnerInfo struct.
func (k Keeper) SetPrimaryName(ctx context.Context, owner sdk.AccAddress, name string) error {
	info, err := k.Owners.Get(ctx, owner.String())
	if err != nil {
		info = types.OwnerInfo{Address: owner.String()}
	}
	info.PrimaryName = name
	return k.Owners.Set(ctx, owner.String(), info)
}

// GetLastActiveTime retrieves the last active timestamp for an address.
func (k Keeper) GetLastActiveTime(ctx context.Context, owner sdk.AccAddress) int64 {
	info, err := k.Owners.Get(ctx, owner.String())
	if err != nil {
		return 0
	}
	return info.LastActiveTime
}

// --- Authority Helpers ---

// IsGovAuthority checks if the given address is the governance authority.
func (k Keeper) IsGovAuthority(addr string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	return bytes.Equal(k.authority, addrBytes)
}

// IsCouncilAuthorized checks if the address is authorized via governance authority,
// council policy address, or committee membership.
// Delegates to x/commons IsCouncilAuthorized when available.
// Falls back to IsGovAuthority when x/commons is not wired.
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	if k.commonsKeeper == nil {
		return k.IsGovAuthority(addr)
	}
	return k.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// IsCommonsCouncilMember checks if the provided address is a member of the "Commons Council".
// Uses the native x/commons membership check instead of x/group queries.
func (k Keeper) IsCommonsCouncilMember(ctx context.Context, memberAddr string) (bool, error) {
	if k.commonsKeeper == nil {
		return false, errors.New("commons keeper not configured")
	}
	return k.commonsKeeper.HasMember(ctx, "Commons Council", memberAddr)
}

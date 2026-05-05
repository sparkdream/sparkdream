package keeper

import (
	"context"
	"regexp"
	"strings"
	"time"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// validNameRegex permits lowercase alphanumerics, hyphens, and underscores.
// Names must start and end with an alphanumeric or underscore (matching X /
// Twitter handle rules where leading/trailing underscores are valid).
// Hyphens, by contrast, remain restricted to the interior — leading/trailing
// hyphens are rejected to avoid DNS-like ambiguity and URL-parsing edge
// cases (X also disallows hyphens entirely).
var validNameRegex = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?$`)

func (k msgServer) RegisterName(goCtx context.Context, msg *types.MsgRegisterName) (*types.MsgRegisterNameResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)

	// 1. Normalize Name (Lower case, trim spaces)
	name := strings.ToLower(strings.TrimSpace(msg.Name))
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Authority)
	if err != nil {
		return nil, err
	}

	// 2. Validate Name (Length, Characters & Reserved)
	if len(name) < int(params.MinNameLength) {
		return nil, errorsmod.Wrapf(types.ErrInvalidName, "name too short (min %d)", params.MinNameLength)
	}
	if len(name) > int(params.MaxNameLength) {
		return nil, errorsmod.Wrapf(types.ErrInvalidName, "name too long (max %d)", params.MaxNameLength)
	}
	if !validNameRegex.MatchString(name) {
		return nil, errorsmod.Wrapf(types.ErrInvalidName, "name contains invalid characters (allowed: a-z, 0-9, -, _; cannot start/end with -)")
	}
	for _, blocked := range params.BlockedNames {
		if name == blocked {
			return nil, errorsmod.Wrapf(types.ErrNameReserved, "name '%s' is reserved", name)
		}
	}

	// 3. Membership Gate: any active x/rep member may register a name.
	// Invitation acceptance creates the Member record with
	// MEMBER_STATUS_ACTIVE, so newly invited members (including AI agents)
	// can claim a handle immediately. Anti-squatting / anti-impersonation
	// continues to be enforced by the registration_fee, max_names_per_address,
	// blocked_names list, and the dispute mechanism.
	isMember, err := k.IsActiveRepMember(ctx, msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "authorization check failed: %s", err.Error())
	}
	if !isMember {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only active x/rep members can register names")
	}

	// 4. Check Fees
	if !params.RegistrationFee.IsZero() {
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, sdk.NewCoins(params.RegistrationFee)); err != nil {
			return nil, errorsmod.Wrapf(err, "insufficient funds for registration fee")
		}
	}

	// 5. Check Availability & Scavenge Logic
	currentOwner, found := k.GetNameOwner(ctx, name)
	if found {
		// Scavenge Logic: Check if current owner is expired
		if k.IsOwnerExpired(ctx, currentOwner) {
			// Expired! We can steal it.
			// Emit event to track the scavenge
			ctx.EventManager().EmitEvent(
				sdk.NewEvent("name_scavenged",
					sdk.NewAttribute("name", name),
					sdk.NewAttribute("old_owner", currentOwner.String()),
					sdk.NewAttribute("new_owner", msg.Authority),
				),
			)

			// Remove the name from the old owner's index
			if err := k.RemoveNameFromOwner(ctx, currentOwner, name); err != nil {
				return nil, err
			}

			// Clear old owner's PrimaryName if it matches the scavenged name
			oldOwnerInfo, err := k.Owners.Get(ctx, currentOwner.String())
			if err == nil && oldOwnerInfo.PrimaryName == name {
				oldOwnerInfo.PrimaryName = ""
				if err := k.Owners.Set(ctx, currentOwner.String(), oldOwnerInfo); err != nil {
					return nil, err
				}
			}

		} else {
			return nil, errorsmod.Wrapf(types.ErrNameTaken, "name already taken and active")
		}
	}

	// 6. Limit Check
	// We check how many names the creator currently owns
	count, err := k.GetOwnedNamesCount(ctx, creatorAddr)
	if err != nil {
		return nil, err
	}
	if count >= params.MaxNamesPerAddress {
		return nil, errorsmod.Wrapf(types.ErrTooManyNames, "limit of %d names reached", params.MaxNamesPerAddress)
	}

	// 7. Store Data
	record := types.NameRecord{
		Name:  name,
		Owner: msg.Authority,
		Data:  msg.Data,
	}
	if err := k.SetName(ctx, record); err != nil {
		return nil, err
	}

	// Update Owner Info & Secondary Index
	if err := k.AddNameToOwner(ctx, creatorAddr, name); err != nil {
		return nil, err
	}

	// Auto-set Primary if this is their first name
	if count == 0 {
		if err := k.SetPrimaryName(ctx, creatorAddr, name); err != nil {
			return nil, err
		}
	}

	return &types.MsgRegisterNameResponse{}, nil
}

// Helper to check expiration
func (k Keeper) IsOwnerExpired(ctx sdk.Context, ownerAddr sdk.AccAddress) bool {
	// LastActiveTime is seeded by AddNameToOwner and refreshed by
	// RecordOwnerActivity on every owner-driven message; a zero value here
	// means we have no recent activity record and the owner is treated as
	// expired (scavengeable). Pre-mainnet there are no legacy zero records.
	lastActive := k.GetLastActiveTime(ctx, ownerAddr)

	params := k.GetParams(ctx)
	expirationDuration := params.ExpirationDuration.Seconds() // int64

	// Expiration Time = LastActive + Duration
	expiryTime := time.Unix(lastActive, 0).Add(time.Duration(expirationDuration) * time.Second)

	return ctx.BlockTime().After(expiryTime)
}

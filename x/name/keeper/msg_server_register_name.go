package keeper

import (
	"context"
	"regexp"
	commontypes "sparkdream/x/common/types"
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
	if commontypes.Saturate[uint64](len(name)) < params.MinNameLength {
		return nil, errorsmod.Wrapf(types.ErrInvalidName, "name too short (min %d)", params.MinNameLength)
	}
	if commontypes.Saturate[uint64](len(name)) > params.MaxNameLength {
		return nil, errorsmod.Wrapf(types.ErrInvalidName, "name too long (max %d)", params.MaxNameLength)
	}
	if !validNameRegex.MatchString(name) {
		return nil, errorsmod.Wrapf(types.ErrInvalidName, "name contains invalid characters (allowed: a-z, 0-9, -, _; cannot start/end with -)")
	}
	if isBlockedName(name, params.BlockedNames) {
		return nil, errorsmod.Wrapf(types.ErrNameReserved, "name '%s' is reserved", name)
	}
	if len(msg.Data) > MaxNameDataLen {
		return nil, errorsmod.Wrapf(types.ErrInvalidName, "data is at most %d bytes", MaxNameDataLen)
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

	// 4. Check Fees. The denom comes from the chain's identity at runtime.
	// The fee is burned, like every other creation fee on the chain.
	if !params.RegistrationFeeAmount.IsZero() {
		fee := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), params.RegistrationFeeAmount))
		if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, fee); err != nil {
			return nil, errorsmod.Wrapf(err, "insufficient funds for registration fee")
		}
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, fee); err != nil {
			return nil, errorsmod.Wrapf(err, "failed to burn registration fee")
		}
	}

	// 5. Check Availability & Scavenge Logic
	scavenged := false
	currentOwner, found := k.GetNameOwner(ctx, name)
	if found {
		// Scavenge Logic: Check if current owner is expired
		if k.IsOwnerExpired(ctx, currentOwner) {
			// A name under an active dispute is not scavengeable: the
			// dispute would otherwise resolve against the scavenger, who
			// never had a contest window (NAM-09). It becomes available
			// again once the dispute resolves.
			if d, found := k.GetDispute(ctx, name); found && d.Active {
				return nil, errorsmod.Wrapf(types.ErrNameTaken, "name %q is under an active dispute", name)
			}
			// Expired! We can steal it.
			scavenged = true
			// Emit event to track the scavenge
			ctx.EventManager().EmitEvent(
				sdk.NewEvent("name_scavenged",
					sdk.NewAttribute("name", name),
					sdk.NewAttribute("old_owner", currentOwner.String()),
					sdk.NewAttribute("new_owner", msg.Authority),
				),
			)

			// End the old owner's resolver-target relationship. The fresh
			// record below carries no target, so an accepted target left in
			// place would keep reverse-resolving to a name the scavenger
			// now controls.
			if oldRecord, ok := k.GetName(ctx, name); ok {
				if err := k.clearTargetConsent(ctx, oldRecord); err != nil {
					return nil, err
				}
			}

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

	// A scavenge already emitted name_scavenged above; a fresh registration
	// emits name_registered (spec §2 step 8).
	if !scavenged {
		ctx.EventManager().EmitEvent(
			sdk.NewEvent("name_registered",
				sdk.NewAttribute("name", name),
				sdk.NewAttribute("owner", msg.Authority),
			),
		)
	}

	return &types.MsgRegisterNameResponse{}, nil
}

// IsOwnerExpired reports whether ownerAddr's names can be scavenged: the
// owner has been silent for expiration_duration, or x/rep zeroed the owner
// (the zeroing hook sets OwnerInfo.scavengeable, NAM-202).
func (k Keeper) IsOwnerExpired(ctx sdk.Context, ownerAddr sdk.AccAddress) bool {
	info, err := k.Owners.Get(ctx, ownerAddr.String())
	if err == nil && info.Scavengeable {
		return true
	}
	// LastActiveTime is seeded by AddNameToOwner and refreshed by
	// RecordOwnerActivity on every owner-driven message; a zero value here
	// means we have no recent activity record and the owner is treated as
	// expired (scavengeable). Pre-mainnet there are no legacy zero records.
	lastActive := int64(0)
	if err == nil {
		lastActive = info.LastActiveTime
	}

	params := k.GetParams(ctx)
	expirationDuration := params.ExpirationDuration.Seconds() // int64

	// Expiration Time = LastActive + Duration
	expiryTime := time.Unix(lastActive, 0).Add(time.Duration(expirationDuration) * time.Second)

	return ctx.BlockTime().After(expiryTime)
}

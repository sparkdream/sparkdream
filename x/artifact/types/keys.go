package types

import (
	"strconv"

	"cosmossdk.io/collections"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "artifact"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"

	// ContentLicense is the only license value MsgCreateClass and MsgMint
	// accept in accepted_content_license (docs/content-license.md).
	ContentLicense = "CC0-1.0"

	// TokenStandard identifies local tokens in x/collect NftReference.
	TokenStandard = "sparkdream-artifact-v1"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_artifact")

// Collection prefixes. One byte each, never reused.
var (
	ClassSeqKey            = collections.NewPrefix(1)
	ClassesKey             = collections.NewPrefix(2)
	ClassesByOwnerKey      = collections.NewPrefix(3)
	ClassCountByCreatorKey = collections.NewPrefix(4)
	TokensKey              = collections.NewPrefix(5)
	TokensByOwnerKey       = collections.NewPrefix(6)
	PublicMintCountKey     = collections.NewPrefix(7)
	ReceivePoliciesKey     = collections.NewPrefix(8)
	PendingTransfersKey    = collections.NewPrefix(9)
	PendingMintsKey        = collections.NewPrefix(10)
	InboxByRecipientKey    = collections.NewPrefix(11)
	OutboxBySenderKey      = collections.NewPrefix(12)
	PendingPairCountKey    = collections.NewPrefix(13)
	InboxCountKey          = collections.NewPrefix(14)
	PendingExpiryKey       = collections.NewPrefix(15)
	ListingsKey            = collections.NewPrefix(16)
	ListingsBySellerKey    = collections.NewPrefix(17)
	ListingExpiryKey       = collections.NewPrefix(18)
	ClassCancelQueueKey    = collections.NewPrefix(19)
	ClassScrubQueueKey     = collections.NewPrefix(20)
	PendingClassOwnersKey  = collections.NewPrefix(21)
	PendingOwnerExpiryKey  = collections.NewPrefix(22)
	HideSeqKey             = collections.NewPrefix(23)
	HideRecordsKey         = collections.NewPrefix(24)
	HideByTargetKey        = collections.NewPrefix(25)
	HideExpiryKey          = collections.NewPrefix(26)
	SentinelDailyHidesKey  = collections.NewPrefix(27)
	HidesByTargetAllKey    = collections.NewPrefix(28)
	FailedExpiryKey        = collections.NewPrefix(29)
)

// MintOrigin is the inbox-cap origin for pending mints of a class
// (docs/x-artifact-spec.md §3.7): keyed by class, not by minter, so a class
// owner cannot rotate minters to get around the per-pair cap.
func MintOrigin(classID uint64) string {
	return "class/" + strconv.FormatUint(classID, 10)
}

// ModuleAddressString returns the module account address (deposit escrow).
func ModuleAddressString() string {
	return authtypes.NewModuleAddress(ModuleName).String()
}

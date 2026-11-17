package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "storage"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"

	// ContentScannerServiceType is the x/service type of content scanners.
	ContentScannerServiceType = "content-scanner"

	// ArweaveBackend and ArweaveServiceType name the launch backend.
	ArweaveBackend     = "arweave"
	ArweaveServiceType = "storage-arweave"
)

// Deadline kinds: one timer queue, keyed (height, request id, kind).
const (
	DeadlineExpiry  uint32 = 1 // PENDING past its deadline
	DeadlineClaim   uint32 = 2 // CLAIMED past claim_expires_at
	DeadlineSettle  uint32 = 3 // COMPLETED at settle_at
	DeadlineForfeit uint32 = 4 // flagged fee held until forfeit_at
	DeadlinePrune   uint32 = 5 // closed request past its retention
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_storage")

var (
	BackendsKey          = collections.NewPrefix("backend/")
	ApprovedOperatorsKey = collections.NewPrefix("approved_operator/")
	ApprovedScannersKey  = collections.NewPrefix("approved_scanner/")
	RequestsKey          = collections.NewPrefix("request/value/")
	RequestSeqKey        = collections.NewPrefix("request/seq/")
	LiveByHashKey        = collections.NewPrefix("request/live_by_hash/")
	RemovedHashesKey     = collections.NewPrefix("removed_hash/")
	ByStatusKey          = collections.NewPrefix("request/by_status/")
	ByRequesterKey       = collections.NewPrefix("request/by_requester/")
	OpenByRequesterKey   = collections.NewPrefix("request/open_by_requester/")
	ByOperatorKey        = collections.NewPrefix("request/by_operator/")
	ClaimsByOperatorKey  = collections.NewPrefix("request/claims_by_operator/")
	DeadlinesKey         = collections.NewPrefix("deadline/")
	CountersKey          = collections.NewPrefix("counter/")
)

package types

import (
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
)

const (
	// ModuleName defines the module name
	ModuleName = "name"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	GovModuleName = "gov"
)

// Collections Prefixes
var (
	ParamsKey          = collections.NewPrefix("p_name")
	KeyNames           = collections.NewPrefix("names")
	KeyOwners          = collections.NewPrefix("owners")
	KeyDisputes        = collections.NewPrefix("disputes")
	KeyOwnerNames      = collections.NewPrefix("owner_names")
	KeyDisputeStakes   = collections.NewPrefix("dispute_stakes")
	KeyContestStakes   = collections.NewPrefix("contest_stakes")
	KeyAcceptedTargets = collections.NewPrefix("accepted_targets")
	// KeyTargetNames: (target_address, name) for every name whose resolver
	// target is set (accepted or not).
	KeyTargetNames = collections.NewPrefix("target_names")
	// KeyPendingDisputes: (filed_at_height, name) for every active,
	// uncontested dispute the BeginBlocker may auto-resolve.
	KeyPendingDisputes = collections.NewPrefix("pending_disputes")
	// KeyClaimantDisputes: (claimant, name) for every active dispute, so the
	// zeroing hook finds a member's disputes without walking the table.
	KeyClaimantDisputes = collections.NewPrefix("claimant_disputes")
	// KeyReviewDisputes: (filed_at_height, name) for every active dispute
	// that waits for a council decision (contested, or uncontested and
	// awaiting review); the BeginBlocker dismisses the ones the council
	// leaves past the review window.
	KeyReviewDisputes = collections.NewPrefix("review_disputes")
)

// Parameter keys
var (
	KeyBlockedNames       = []byte("BlockedNames")
	KeyMinNameLength      = []byte("MinNameLength")
	KeyMaxNameLength      = []byte("MaxNameLength")
	KeyMaxNamesPerAddress = []byte("MaxNamesPerAddress")
	KeyRegistrationFee    = []byte("RegistrationFee")
	KeyExpirationDuration = []byte("ExpirationDuration")
)

// Default parameter values
var (
	DefaultMinNameLength      = uint64(3)
	DefaultMaxNameLength      = uint64(30)
	DefaultMaxNamesPerAddress = uint64(5)
	// DefaultRegistrationFeeAmount is the default registration fee in bond-denom
	// micro-units. The denom comes from x/identity at runtime via k.BondDenom(ctx).
	DefaultRegistrationFeeAmount = math.NewInt(10000000)    // 10 SPARK
	DefaultExpirationDuration    = time.Hour * 24 * 365     // 1 Year
	DefaultDisputeStakeDream     = math.NewInt(50_000_000)  // 50 DREAM (aligned with x/season report stake)
	DefaultDisputeTimeoutBlocks  = uint64(100800)           // ~7 days in blocks (aligned with x/season appeal period)
	DefaultContestStakeDream     = math.NewInt(100_000_000) // 100 DREAM (aligned with x/season appeal stake)
)

// DefaultBlockedNames includes critical system names and project-specific reserved terms
var (
	DefaultBlockedNames = []string{
		// System & Roles
		"admin",
		"administrator",
		"root",
		"sysadmin",
		"mod",
		"moderator",
		"system",
		"net",
		"network",
		"bot",
		"support",
		"help",
		"helpdesk",
		"security",
		"verify",
		"verification",
		"kyc",

		// Actions (Scam Prevention)
		"airdrop",
		"claim",
		"mint",
		"wallet",
		"vault",
		"safe",
		"refund",

		// Protocol & Modules
		"validator",
		"faucet",
		"council",
		"treasury",
		"gov",
		"governance",
		"ecosystem",
		"community",
		"bank",
		"stake",
		"staking",
		"ibc",
		"transfer",

		// Major Crypto Brands & Figures
		"bitcoin",
		"btc",
		"ethereum",
		"eth",
		"cosmos",
		"atom",
		"tendermint",
		"ignite",
		"satoshi",
		"nakamoto",

		// Project Specific (Spark Dream)
		"sparkdream",
		"sparkdreamnft",
		"sparkdreamatelier",
		"sparkdreamofficial",
		"sparkdreamfoundation",
		"sparkdreamdao",
		"sparkdreamio",
		"atelier",
		"atelierdao",
		"official",
		"team",
		"founder",
		"ceo",
		"owner",
	}
)

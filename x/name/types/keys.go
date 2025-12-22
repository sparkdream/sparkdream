package types

import (
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
	ParamsKey     = collections.NewPrefix("p_name")
	KeyNames      = collections.NewPrefix("names")
	KeyOwners     = collections.NewPrefix("owners")
	KeyDisputes   = collections.NewPrefix("disputes")
	KeyOwnerNames = collections.NewPrefix("owner_names")
)

// Parameter keys
var (
	KeyBlockedNames       = []byte("BlockedNames")
	KeyMinNameLength      = []byte("MinNameLength")
	KeyMaxNameLength      = []byte("MaxNameLength")
	KeyMaxNamesPerAddress = []byte("MaxNamesPerAddress")
	KeyRegistrationFee    = []byte("RegistrationFee")
	KeyExpirationDuration = []byte("ExpirationDuration")
	KeyDisputeFee         = []byte("DisputeFee")
)

// Default parameter values
var (
	DefaultMinNameLength      = uint64(3)
	DefaultMaxNameLength      = uint64(30)
	DefaultMaxNamesPerAddress = uint64(5)
	DefaultRegistrationFee    = sdk.NewCoin("uspark", math.NewInt(10000000))  // 10 SPARK
	DefaultExpirationDuration = time.Hour * 24 * 365                          // 1 Year
	DefaultDisputeFee         = sdk.NewCoin("uspark", math.NewInt(500000000)) // 500 SPARK
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

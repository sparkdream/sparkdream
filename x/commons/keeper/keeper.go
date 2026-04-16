package keeper

import (
	"encoding/binary"
	"fmt"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"
)

// lateKeepers holds dependencies that are wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (e.g. in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	govKeeper types.GovKeeper
	router    baseapp.MessageRouter
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	authKeeper     types.AuthKeeper
	bankKeeper     types.BankKeeper
	futarchyKeeper types.FutarchyKeeper
	splitKeeper    types.SplitKeeper
	upgradeKeeper  types.UpgradeKeeper
	late           *lateKeepers

	// --- Existing collections (unchanged) ---
	PolicyPermissions  collections.Map[string, types.PolicyPermissions]
	Groups             collections.Map[string, types.Group]
	PolicyToName       collections.Map[string, string]
	MarketToGroup      collections.Map[uint64, string]
	MarketTriggerQueue collections.KeySet[collections.Pair[int64, string]]

	// --- New native governance collections (replacing x/group) ---

	// Members stores council/committee members: (council_name, member_address) -> Member
	Members collections.Map[collections.Pair[string, string], types.Member]
	// DecisionPolicies stores voting rules per policy address: policy_address -> DecisionPolicy
	DecisionPolicies collections.Map[string, types.DecisionPolicy]
	// Proposals stores all proposals: proposal_id -> Proposal
	Proposals collections.Map[uint64, types.Proposal]
	// ProposalSeq is the auto-incrementing proposal ID sequence
	ProposalSeq collections.Sequence
	// CouncilSeq is the auto-incrementing council ID sequence
	CouncilSeq collections.Sequence
	// PolicyVersion tracks the current policy version for veto invalidation
	PolicyVersion collections.Map[string, uint64]
	// Votes stores individual votes: (proposal_id, voter_address) -> Vote
	Votes collections.Map[collections.Pair[uint64, string], types.Vote]
	// ProposalsByCouncil is an index: (council_name, proposal_id) -> empty
	ProposalsByCouncil collections.KeySet[collections.Pair[string, uint64]]
	// VetoPolicies maps a council name to its veto policy address
	VetoPolicies collections.Map[string, string]
	// AnonVoteTallies stores anonymous vote counts per proposal: proposal_id -> AnonVoteTally
	AnonVoteTallies collections.Map[uint64, types.AnonVoteTally]
	// EpochSpending tracks cumulative spending per (policy_address, epoch_day).
	// Value is the cumulative uspark amount spent in the epoch (string-encoded).
	EpochSpending collections.Map[collections.Pair[string, int64], string]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	futarchyKeeper types.FutarchyKeeper,
	govKeeper types.GovKeeper,
	splitKeeper types.SplitKeeper,
	upgradeKeeper types.UpgradeKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		authKeeper:     authKeeper,
		bankKeeper:     bankKeeper,
		futarchyKeeper: futarchyKeeper,
		splitKeeper:    splitKeeper,
		upgradeKeeper:  upgradeKeeper,
		late:           &lateKeepers{govKeeper: govKeeper},

		// Existing collections
		Params:            collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		PolicyPermissions: collections.NewMap(sb, types.PolicyPermissionsKey, "policyPermissions", collections.StringKey, codec.CollValue[types.PolicyPermissions](cdc)),
		Groups:            collections.NewMap(sb, types.GroupKey, "group", collections.StringKey, codec.CollValue[types.Group](cdc)),
		PolicyToName:      collections.NewMap(sb, types.PolicyToNameKey, "policyToName", collections.StringKey, collections.StringValue),
		MarketToGroup:     collections.NewMap(sb, types.MarketToGroupKey, "market_to_group", collections.Uint64Key, collections.StringValue),
		MarketTriggerQueue: collections.NewKeySet(
			sb,
			types.MarketTriggerQueueKey,
			"market_trigger_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),

		// New native governance collections
		Members: collections.NewMap(
			sb, types.MembersKey, "members",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.Member](cdc),
		),
		DecisionPolicies: collections.NewMap(
			sb, types.DecisionPoliciesKey, "decisionPolicies",
			collections.StringKey,
			codec.CollValue[types.DecisionPolicy](cdc),
		),
		Proposals: collections.NewMap(
			sb, types.ProposalsKey, "proposals",
			collections.Uint64Key,
			codec.CollValue[types.Proposal](cdc),
		),
		ProposalSeq: collections.NewSequence(sb, types.ProposalSeqKey, "proposal_seq"),
		CouncilSeq:  collections.NewSequence(sb, types.CouncilSeqKey, "council_seq"),
		PolicyVersion: collections.NewMap(
			sb, types.PolicyVersionKey, "policyVersion",
			collections.StringKey,
			collections.Uint64Value,
		),
		Votes: collections.NewMap(
			sb, types.VotesKey, "votes",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			codec.CollValue[types.Vote](cdc),
		),
		ProposalsByCouncil: collections.NewKeySet(
			sb, types.ProposalsByCouncilKey, "proposalsByCouncil",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		VetoPolicies: collections.NewMap(
			sb, types.VetoPoliciesKey, "vetoPolicies",
			collections.StringKey,
			collections.StringValue,
		),
		AnonVoteTallies: collections.NewMap(
			sb, types.AnonVoteTalliesKey, "anonVoteTallies",
			collections.Uint64Key,
			codec.CollValue[types.AnonVoteTally](cdc),
		),
		EpochSpending: collections.NewMap(
			sb, types.EpochSpendingKey, "epochSpending",
			collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
			collections.StringValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// Codec returns the keeper's codec for use by ante handlers and other packages.
func (k Keeper) Codec() codec.Codec {
	return k.cdc
}

// SetGovKeeper wires the GovKeeper after depinject to break cyclic dependencies.
func (k *Keeper) SetGovKeeper(gk types.GovKeeper) {
	k.late.govKeeper = gk
}

// SetRouter wires the MsgServiceRouter after app build for proposal execution.
func (k *Keeper) SetRouter(router baseapp.MessageRouter) {
	k.late.router = router
}

// DeriveCouncilAddress generates a deterministic address for a council based on its ID.
// This replaces x/group's policy address generation.
func DeriveCouncilAddress(councilID uint64, policyType string) sdk.AccAddress {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, councilID)
	key := append([]byte("council/"+policyType+"/"), buf...)
	return sdkaddress.Module(types.ModuleName, key)
}

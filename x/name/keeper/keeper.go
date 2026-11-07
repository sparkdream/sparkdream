package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/internal/dreamutil"
	"sparkdream/x/name/types"
)

type Keeper struct {
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	// External Keepers
	bankKeeper    types.BankKeeper
	commonsKeeper types.CommonsKeeper
	repKeeper     types.RepKeeper

	// Shared DREAM token operations (delegates to repKeeper)
	dreamOps dreamutil.Ops

	// State Collections
	Schema   collections.Schema
	Params   collections.Item[types.Params]
	Names    collections.Map[string, types.NameRecord] // Key: Name
	Owners   collections.Map[string, types.OwnerInfo]  // Key: Address (string)
	Disputes collections.Map[string, types.Dispute]    // Key: Name

	// Secondary Index: (OwnerAddress, Name) -> Empty
	// This allows efficient iteration of names owned by a specific address.
	OwnerNames collections.KeySet[collections.Pair[string, string]]

	// Secondary Index: (TargetAddress, Name) -> Empty
	// Records names where TargetAddress has accepted being the resolver target.
	// Used by MsgSetPrimary to authorize an accepted target setting the name
	// as primary, and by the Targets query.
	AcceptedTargets collections.KeySet[collections.Pair[string, string]]

	// Dispute stake tracking
	DisputeStakes collections.Map[string, types.DisputeStake] // Key: challenge_id
	ContestStakes collections.Map[string, types.ContestStake] // Key: challenge_id

	// Secondary Index: (TargetAddress, Name) for every name with a target,
	// accepted or not. Maintained by SetName; lets the migration hook find
	// names pointing at an address without walking the registry.
	TargetNames collections.KeySet[collections.Pair[string, string]]

	// Secondary Index: (FiledAtHeight, Name) for active uncontested
	// disputes. Maintained by SetDispute; the BeginBlocker walks only the
	// entries whose timeout has passed.
	PendingDisputes collections.KeySet[collections.Pair[int64, string]]
	// Secondary Index: (Claimant, Name) for active disputes. Maintained by
	// SetDispute; read by the zeroing hook.
	ClaimantDisputes collections.KeySet[collections.Pair[string, string]]
	// Secondary Index: (FiledAtHeight, Name) for active disputes waiting
	// for a council decision: contested ones and uncontested ones awaiting
	// review. Maintained by SetDispute; the BeginBlocker dismisses the ones
	// past the review window (NAM-102, NAM-201).
	ReviewDisputes collections.KeySet[collections.Pair[int64, string]]
	// ParkedReviewDisputes: (RetryAt, FiledAtHeight, Name) for review
	// disputes whose dismissal failed, and its reverse ParkedReviewDisputeAt:
	// (FiledAtHeight, Name) -> RetryAt. Not exported: an imported dispute is
	// simply tried once more (review_dispute_parking.go).
	ParkedReviewDisputes  collections.KeySet[collections.Triple[int64, int64, string]]
	ParkedReviewDisputeAt collections.Map[collections.Pair[int64, string], int64]
	// late holds the keepers wired after depinject (app.go Set*Keeper).
	// It is a pointer so the AppModule's value copy of the keeper sees
	// them too (docs/development-conventions.md, AppModule value-copy bug).
	late *lateKeepers
}

// lateKeepers are the dependencies wired after construction; shared by
// every copy of the Keeper value.
type lateKeepers struct {
	identityKeeper types.IdentityKeeper
}

// GetCommonsKeeper returns the commons keeper for simulation use.
func (k Keeper) GetCommonsKeeper() types.CommonsKeeper {
	return k.commonsKeeper
}

// GetRepKeeper returns the rep keeper for simulation use.
func (k Keeper) GetRepKeeper() types.RepKeeper {
	return k.repKeeper
}

func NewKeeper(
	storeService storetypes.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	bankKeeper types.BankKeeper,
	commonsKeeper types.CommonsKeeper,
	repKeeper types.RepKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		late:          &lateKeepers{},
		cdc:           cdc,
		addressCodec:  addressCodec,
		authority:     authority,
		bankKeeper:    bankKeeper,
		commonsKeeper: commonsKeeper,
		repKeeper:     repKeeper,

		// Initialize Collections
		Params:   collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Names:    collections.NewMap(sb, types.KeyNames, "names", collections.StringKey, codec.CollValue[types.NameRecord](cdc)),
		Owners:   collections.NewMap(sb, types.KeyOwners, "owners", collections.StringKey, codec.CollValue[types.OwnerInfo](cdc)),
		Disputes: collections.NewMap(sb, types.KeyDisputes, "disputes", collections.StringKey, codec.CollValue[types.Dispute](cdc)),

		// Initialize Secondary Index
		OwnerNames: collections.NewKeySet(
			sb,
			types.KeyOwnerNames,
			"owner_names",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),

		// Index of (target_address, name) for accepted resolver targets.
		AcceptedTargets: collections.NewKeySet(
			sb,
			types.KeyAcceptedTargets,
			"accepted_targets",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),

		// Dispute stake tracking
		DisputeStakes: collections.NewMap(sb, types.KeyDisputeStakes, "dispute_stakes", collections.StringKey, codec.CollValue[types.DisputeStake](cdc)),
		ContestStakes: collections.NewMap(sb, types.KeyContestStakes, "contest_stakes", collections.StringKey, codec.CollValue[types.ContestStake](cdc)),

		TargetNames: collections.NewKeySet(sb, types.KeyTargetNames, "target_names",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey)),
		PendingDisputes: collections.NewKeySet(sb, types.KeyPendingDisputes, "pending_disputes",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey)),
		ClaimantDisputes: collections.NewKeySet(sb, types.KeyClaimantDisputes, "claimant_disputes",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey)),
		ReviewDisputes: collections.NewKeySet(sb, types.KeyReviewDisputes, "review_disputes",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey)),
		ParkedReviewDisputes: collections.NewKeySet(sb, types.KeyParkedReviewDisputes, "parked_review_disputes",
			collections.TripleKeyCodec(collections.Int64Key, collections.Int64Key, collections.StringKey)),
		ParkedReviewDisputeAt: collections.NewMap(sb, types.KeyParkedReviewDisputeAt, "parked_review_dispute_at",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey), collections.Int64Value),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	// Initialize shared DREAM operations (after schema build)
	k.dreamOps = dreamutil.NewOps(repKeeper, addressCodec)

	return k
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// SetIdentityKeeper wires the identity keeper post-depinject.
func (k *Keeper) SetIdentityKeeper(ik types.IdentityKeeper) {
	k.late.identityKeeper = ik
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity isn't wired; every call site needs a real denom.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("name keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

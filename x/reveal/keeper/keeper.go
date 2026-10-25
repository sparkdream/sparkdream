package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/reveal/types"
)

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	// External keepers
	authKeeper    types.AuthKeeper
	bankKeeper    types.BankKeeper
	repKeeper     types.RepKeeper
	commonsKeeper types.CommonsKeeper

	// Primary storage
	Contribution    collections.Map[uint64, types.Contribution]
	ContributionSeq collections.Sequence

	RevealStake collections.Map[uint64, types.RevealStake]
	StakeSeq    collections.Sequence

	// Votes keyed by composite string: "{contributionID}/{trancheID}/{voter}"
	Vote collections.Map[string, types.VerificationVote]

	// Secondary indexes for efficient queries
	ContributionsByStatus      collections.KeySet[collections.Pair[int32, uint64]]
	ContributionsByContributor collections.KeySet[collections.Pair[string, uint64]]
	StakesByTranche            collections.KeySet[collections.Pair[string, uint64]]
	StakesByStaker             collections.KeySet[collections.Pair[string, uint64]]
	VotesByTranche             collections.KeySet[collections.Pair[string, string]]
	VotesByVoter               collections.KeySet[collections.Pair[string, string]]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	repKeeper types.RepKeeper,
	commonsKeeper types.CommonsKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService:  storeService,
		cdc:           cdc,
		addressCodec:  addressCodec,
		authority:     authority,
		authKeeper:    authKeeper,
		bankKeeper:    bankKeeper,
		repKeeper:     repKeeper,
		commonsKeeper: commonsKeeper,

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		// Primary storage
		Contribution:    collections.NewMap(sb, types.ContributionKey, "contribution", collections.Uint64Key, codec.CollValue[types.Contribution](cdc)),
		ContributionSeq: collections.NewSequence(sb, types.ContributionSeqKey, "contributionSequence"),

		RevealStake: collections.NewMap(sb, types.RevealStakeKey, "revealStake", collections.Uint64Key, codec.CollValue[types.RevealStake](cdc)),
		StakeSeq:    collections.NewSequence(sb, types.RevealStakeSeqKey, "revealStakeSequence"),

		Vote: collections.NewMap(sb, types.VoteKey, "vote", collections.StringKey, codec.CollValue[types.VerificationVote](cdc)),

		// Secondary indexes
		ContributionsByStatus: collections.NewKeySet(
			sb, types.ContributionsByStatusKey, "contributionsByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		ContributionsByContributor: collections.NewKeySet(
			sb, types.ContributionsByContributorKey, "contributionsByContributor",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		StakesByTranche: collections.NewKeySet(
			sb, types.StakesByTrancheKey, "stakesByTranche",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		StakesByStaker: collections.NewKeySet(
			sb, types.StakesByStakerKey, "stakesByStaker",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		VotesByTranche: collections.NewKeySet(
			sb, types.VotesByTrancheKey, "votesByTranche",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		VotesByVoter: collections.NewKeySet(
			sb, types.VotesByVoterKey, "votesByVoter",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// GetAuthorityString returns the module's authority as a string.
func (k Keeper) GetAuthorityString() string {
	addr, _ := k.addressCodec.BytesToString(k.authority)
	return addr
}

// --- Member key migration alias helpers ---
//
// Records keep the address they were written with; a member who migrated to
// a fresh key is matched through x/rep's alias contract. The rep keeper is
// nil in some unit tests, where plain equality is the answer.

// canActAs reports whether signer may exercise rights recorded against stored.
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if k.repKeeper == nil {
		return signer == stored
	}
	return k.repKeeper.CanActAs(ctx, signer, stored)
}

// sameMember reports whether two stored addresses belong to the same member.
func (k Keeper) sameMember(ctx context.Context, a, b string) bool {
	if k.repKeeper == nil {
		return a == b
	}
	return k.repKeeper.SameMember(ctx, a, b)
}

// recentAddressesOf is the in-tx alias set (dedup checks).
func (k Keeper) recentAddressesOf(ctx context.Context, member string) []string {
	if k.repKeeper == nil {
		return []string{member}
	}
	return k.repKeeper.RecentAddressesOf(ctx, member)
}

// allAddressesOf is the full alias set (list queries only).
func (k Keeper) allAddressesOf(ctx context.Context, member string) []string {
	if k.repKeeper == nil {
		return []string{member}
	}
	return k.repKeeper.AllAddressesOf(ctx, member)
}

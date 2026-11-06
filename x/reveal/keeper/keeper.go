package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

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

	// Deadline index (REV-11): IN_PROGRESS contributions keyed by their
	// earliest pending deadline, plus the reverse map used to move an entry
	// when the deadline changes. Maintained by saveContribution.
	ContributionDeadlines    collections.KeySet[collections.Pair[int64, uint64]]
	ContributionNextDeadline collections.Map[uint64, int64]

	// Project transition retry (REV-07): (next_attempt_height, id) and the
	// failed-attempt count per contribution.
	ProjectTransitionQueue    collections.KeySet[collections.Pair[int64, uint64]]
	ProjectTransitionAttempts collections.Map[uint64, uint32]
	// DeadlineFailures counts consecutive real (non-transient) failed
	// deadline steps per contribution; at maxDeadlineFailures the entry is
	// retried once per x/rep epoch instead of every deadlineRetryBlocks
	// (poison counter, REV-104/REV-201). Cleared by every save of the
	// contribution (REV-202). Transient state, not exported.
	DeadlineFailures collections.Map[uint64, uint32]
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
		ContributionDeadlines: collections.NewKeySet(
			sb, types.ContributionDeadlinesKey, "contributionDeadlines",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		ContributionNextDeadline: collections.NewMap(
			sb, types.ContributionNextDeadlineKey, "contributionNextDeadline",
			collections.Uint64Key, collections.Int64Value,
		),
		ProjectTransitionQueue: collections.NewKeySet(
			sb, types.ProjectTransitionQueueKey, "projectTransitionQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		ProjectTransitionAttempts: collections.NewMap(
			sb, types.ProjectTransitionAttemptsKey, "projectTransitionAttempts",
			collections.Uint64Key, collections.Uint32Value,
		),
		DeadlineFailures: collections.NewMap(
			sb, types.DeadlineFailuresKey, "deadlineFailures",
			collections.Uint64Key, collections.Uint32Value,
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

// checkCouncilProposer validates the initiator recorded on a council-routed
// msg (Approve, Reject, ResolveDispute): an Operations Committee member, as
// the spec routes these. Under gov the proposer may be empty; the recorded
// initiator is then the authority itself.
func (k Keeper) checkCouncilProposer(ctx context.Context, authority, proposer string) (string, error) {
	if proposer == "" && authority == k.GetAuthorityString() {
		return authority, nil
	}
	addr, err := k.addressCodec.StringToBytes(proposer)
	if err != nil {
		return "", types.ErrInvalidProposer.Wrapf("invalid proposer address: %s", err)
	}
	ok, err := k.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress(addr), "commons", "operations")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", types.ErrInvalidProposer.Wrapf("%s", proposer)
	}
	return proposer, nil
}

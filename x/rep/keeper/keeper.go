package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/rep/types"
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

	authKeeper      types.AuthKeeper
	bankKeeper      types.BankKeeper
	commonsKeeper   types.CommonsKeeper
	Member          collections.Map[string, types.Member]
	InvitationSeq   collections.Sequence
	Invitation      collections.Map[uint64, types.Invitation]
	ProjectSeq      collections.Sequence
	Project         collections.Map[uint64, types.Project]
	InitiativeSeq   collections.Sequence
	Initiative      collections.Map[uint64, types.Initiative]
	StakeSeq        collections.Sequence
	Stake           collections.Map[uint64, types.Stake]
	ChallengeSeq    collections.Sequence
	Challenge       collections.Map[uint64, types.Challenge]
	JuryReviewSeq   collections.Sequence
	JuryReview      collections.Map[uint64, types.JuryReview]
	InterimSeq      collections.Sequence
	Interim         collections.Map[uint64, types.Interim]
	InterimTemplate collections.Map[string, types.InterimTemplate]
	UsedNullifiers  collections.KeySet[[]byte]
	GiftRecord      collections.Map[collections.Pair[string, string], types.GiftRecord]

	// Secondary indexes for efficient lookups (avoid full table scans in EndBlocker)
	// Key: (status, id) - allows iteration by status
	InitiativesByStatus  collections.KeySet[collections.Pair[int32, uint64]]
	InterimsByStatus     collections.KeySet[collections.Pair[int32, uint64]]
	JuryReviewsByVerdict collections.KeySet[collections.Pair[int32, uint64]]
	// Key: (targetType, targetID, stakeID) - allows lookup of stakes by target
	StakesByTarget collections.KeySet[collections.Triple[int32, uint64, uint64]]
	// Key: (status, id) - allows iteration of challenges by status
	ChallengesByStatus collections.KeySet[collections.Pair[int32, uint64]]

	// Extended staking pools (for O(1) reward distribution)
	MemberStakePool  collections.Map[string, types.MemberStakePool]  // member address -> pool
	TagStakePool     collections.Map[string, types.TagStakePool]     // tag name -> pool
	ProjectStakeInfo collections.Map[uint64, types.ProjectStakeInfo] // project ID -> info
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	commonsKeeper types.CommonsKeeper,
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

		authKeeper:      authKeeper,
		bankKeeper:      bankKeeper,
		commonsKeeper:   commonsKeeper,
		Params:          collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Member:          collections.NewMap(sb, types.MemberKey, "member", collections.StringKey, codec.CollValue[types.Member](cdc)),
		Invitation:      collections.NewMap(sb, types.InvitationKey, "invitation", collections.Uint64Key, codec.CollValue[types.Invitation](cdc)),
		InvitationSeq:   collections.NewSequence(sb, types.InvitationCountKey, "invitationSequence"),
		Project:         collections.NewMap(sb, types.ProjectKey, "project", collections.Uint64Key, codec.CollValue[types.Project](cdc)),
		ProjectSeq:      collections.NewSequence(sb, types.ProjectCountKey, "projectSequence"),
		Initiative:      collections.NewMap(sb, types.InitiativeKey, "initiative", collections.Uint64Key, codec.CollValue[types.Initiative](cdc)),
		InitiativeSeq:   collections.NewSequence(sb, types.InitiativeCountKey, "initiativeSequence"),
		Stake:           collections.NewMap(sb, types.StakeKey, "stake", collections.Uint64Key, codec.CollValue[types.Stake](cdc)),
		StakeSeq:        collections.NewSequence(sb, types.StakeCountKey, "stakeSequence"),
		Challenge:       collections.NewMap(sb, types.ChallengeKey, "challenge", collections.Uint64Key, codec.CollValue[types.Challenge](cdc)),
		ChallengeSeq:    collections.NewSequence(sb, types.ChallengeCountKey, "challengeSequence"),
		JuryReview:      collections.NewMap(sb, types.JuryReviewKey, "juryReview", collections.Uint64Key, codec.CollValue[types.JuryReview](cdc)),
		JuryReviewSeq:   collections.NewSequence(sb, types.JuryReviewCountKey, "juryReviewSequence"),
		Interim:         collections.NewMap(sb, types.InterimKey, "interim", collections.Uint64Key, codec.CollValue[types.Interim](cdc)),
		InterimSeq:      collections.NewSequence(sb, types.InterimCountKey, "interimSequence"),
		InterimTemplate: collections.NewMap(sb, types.InterimTemplateKey, "interimTemplate", collections.StringKey, codec.CollValue[types.InterimTemplate](cdc)),
		UsedNullifiers:  collections.NewKeySet(sb, types.UsedNullifierKey, "usedNullifier", collections.BytesKey),
		GiftRecord: collections.NewMap(sb, types.GiftRecordKey, "giftRecord",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.GiftRecord](cdc)),

		// Secondary indexes for efficient EndBlocker operations
		InitiativesByStatus: collections.NewKeySet(
			sb, types.InitiativesByStatusKey, "initiativesByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		InterimsByStatus: collections.NewKeySet(
			sb, types.InterimsByStatusKey, "interimsByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		JuryReviewsByVerdict: collections.NewKeySet(
			sb, types.JuryReviewsByVerdictKey, "juryReviewsByVerdict",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		StakesByTarget: collections.NewKeySet(
			sb, types.StakesByTargetKey, "stakesByTarget",
			collections.TripleKeyCodec(collections.Int32Key, collections.Uint64Key, collections.Uint64Key),
		),
		ChallengesByStatus: collections.NewKeySet(
			sb, types.ChallengesByStatusKey, "challengesByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),

		// Extended staking pools
		MemberStakePool:  collections.NewMap(sb, types.MemberStakePoolKey, "memberStakePool", collections.StringKey, codec.CollValue[types.MemberStakePool](cdc)),
		TagStakePool:     collections.NewMap(sb, types.TagStakePoolKey, "tagStakePool", collections.StringKey, codec.CollValue[types.TagStakePool](cdc)),
		ProjectStakeInfo: collections.NewMap(sb, types.ProjectStakeInfoKey, "projectStakeInfo", collections.Uint64Key, codec.CollValue[types.ProjectStakeInfo](cdc)),
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

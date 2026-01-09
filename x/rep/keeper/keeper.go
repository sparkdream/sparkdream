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
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
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

		authKeeper: authKeeper,
		bankKeeper: bankKeeper,
		Params:     collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Member:     collections.NewMap(sb, types.MemberKey, "member", collections.StringKey, codec.CollValue[types.Member](cdc)), Invitation: collections.NewMap(sb, types.InvitationKey, "invitation", collections.Uint64Key, codec.CollValue[types.Invitation](cdc)),
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
		InterimTemplate: collections.NewMap(sb, types.InterimTemplateKey, "interimTemplate", collections.StringKey, codec.CollValue[types.InterimTemplate](cdc))}
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

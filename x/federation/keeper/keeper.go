package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"

	"sparkdream/x/federation/types"
)

// lateKeepers holds dependencies wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	commonsKeeper types.CommonsKeeper
	repKeeper     types.RepKeeper
	nameKeeper    types.NameKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	authority    []byte

	authKeeper  types.AuthKeeper
	bankKeeper  types.BankKeeper
	ibcKeeperFn func() *ibckeeper.Keeper
	late        *lateKeepers

	Schema collections.Schema
	Params collections.Item[types.Params]
	Port   collections.Item[string]

	// --- Primary Collections ---

	Peers              collections.Map[string, types.Peer]
	PeerPolicies       collections.Map[string, types.PeerPolicy]
	BridgeOperators    collections.Map[collections.Pair[string, string], types.BridgeOperator]
	// VerifierActivity holds federation-specific per-verifier counters. The
	// generic bond/status record lives in x/rep as BondedRole
	// (ROLE_TYPE_FEDERATION_VERIFIER).
	VerifierActivity   collections.Map[string, types.VerifierActivity]
	VerificationRecords collections.Map[uint64, types.VerificationRecord]
	ArbiterSubmissions collections.Map[collections.Pair[uint64, string], types.ArbiterHashSubmission]
	Content            collections.Map[uint64, types.FederatedContent]
	IdentityLinks      collections.Map[collections.Pair[string, string], types.IdentityLink]
	PendingIdChallenges collections.Map[collections.Pair[string, string], types.PendingIdentityChallenge]
	RepAttestations    collections.Map[collections.Pair[string, string], types.ReputationAttestation]
	OutboundAttestations collections.Map[uint64, types.OutboundAttestation]
	PeerRemovalQueue   collections.Map[string, types.PeerRemovalState]

	// --- Sequences ---

	ContentSeq        collections.Sequence
	OutboundAttestSeq collections.Sequence

	// --- Secondary Indexes ---

	ContentByPeer    collections.KeySet[collections.Pair[string, uint64]]
	ContentByType    collections.KeySet[collections.Pair[string, uint64]]
	ContentByCreator collections.KeySet[collections.Pair[string, uint64]]
	ContentByHash    collections.Map[string, uint64]
	ContentExpiration collections.KeySet[collections.Pair[int64, uint64]]

	BridgesByPeer collections.KeySet[collections.Pair[string, string]]

	IdentityLinksByRemote collections.Map[collections.Pair[string, string], string]
	IdentityLinkCount     collections.Map[string, uint32]
	UnverifiedLinkExp     collections.KeySet[collections.Triple[int64, string, string]]

	AttestationExp collections.KeySet[collections.Triple[int64, string, string]]

	VerificationWindow collections.KeySet[collections.Pair[int64, uint64]]
	ChallengeWindow    collections.KeySet[collections.Pair[int64, uint64]]

	ArbiterHashCounts      collections.Map[collections.Pair[uint64, string], uint32]
	ArbiterResolutionQueue collections.KeySet[collections.Pair[int64, uint64]]
	ArbiterEscalationQueue collections.KeySet[collections.Pair[int64, uint64]]

	BridgeUnbondingQueue collections.KeySet[collections.Triple[int64, string, string]]

	InboundRateLimits  collections.Map[collections.Pair[string, int64], uint64]
	OutboundRateLimits collections.Map[collections.Pair[string, int64], uint64]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	ibcKeeperFn func() *ibckeeper.Keeper,
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
		authKeeper:   authKeeper,
		bankKeeper:   bankKeeper,
		ibcKeeperFn:  ibcKeeperFn,
		late:         &lateKeepers{},

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Port:   collections.NewItem(sb, types.PortKey, "port", collections.StringValue),

		// Primary collections
		Peers: collections.NewMap(sb, types.PeersKey, "peers",
			collections.StringKey, codec.CollValue[types.Peer](cdc)),
		PeerPolicies: collections.NewMap(sb, types.PeerPoliciesKey, "peerPolicies",
			collections.StringKey, codec.CollValue[types.PeerPolicy](cdc)),
		BridgeOperators: collections.NewMap(sb, types.BridgeOperatorsKey, "bridgeOperators",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.BridgeOperator](cdc)),
		VerifierActivity: collections.NewMap(sb, types.VerifierActivityKey, "verifierActivity",
			collections.StringKey, codec.CollValue[types.VerifierActivity](cdc)),
		VerificationRecords: collections.NewMap(sb, types.VerificationRecsKey, "verificationRecords",
			collections.Uint64Key, codec.CollValue[types.VerificationRecord](cdc)),
		ArbiterSubmissions: collections.NewMap(sb, types.ArbiterSubmissionsKey, "arbiterSubmissions",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			codec.CollValue[types.ArbiterHashSubmission](cdc)),
		Content: collections.NewMap(sb, types.ContentKey, "content",
			collections.Uint64Key, codec.CollValue[types.FederatedContent](cdc)),
		IdentityLinks: collections.NewMap(sb, types.IdentityLinksKey, "identityLinks",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.IdentityLink](cdc)),
		PendingIdChallenges: collections.NewMap(sb, types.PendingIdChallengesKey, "pendingIdChallenges",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.PendingIdentityChallenge](cdc)),
		RepAttestations: collections.NewMap(sb, types.RepAttestationsKey, "repAttestations",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.ReputationAttestation](cdc)),
		OutboundAttestations: collections.NewMap(sb, types.OutboundAttestationsKey, "outboundAttestations",
			collections.Uint64Key, codec.CollValue[types.OutboundAttestation](cdc)),
		PeerRemovalQueue: collections.NewMap(sb, types.PeerRemovalQueueKey, "peerRemovalQueue",
			collections.StringKey, codec.CollValue[types.PeerRemovalState](cdc)),

		// Sequences
		ContentSeq:        collections.NewSequence(sb, types.ContentSeqKey, "contentSequence"),
		OutboundAttestSeq: collections.NewSequence(sb, types.OutboundAttestationSeqKey, "outboundAttestSequence"),

		// Content indexes
		ContentByPeer: collections.NewKeySet(sb, types.ContentByPeerKey, "contentByPeer",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		ContentByType: collections.NewKeySet(sb, types.ContentByTypeKey, "contentByType",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		ContentByCreator: collections.NewKeySet(sb, types.ContentByCreatorKey, "contentByCreator",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		ContentByHash: collections.NewMap(sb, types.ContentByHashKey, "contentByHash",
			collections.StringKey, collections.Uint64Value),
		ContentExpiration: collections.NewKeySet(sb, types.ContentExpirationKey, "contentExpiration",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		// Bridge indexes
		BridgesByPeer: collections.NewKeySet(sb, types.BridgesByPeerKey, "bridgesByPeer",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey)),

		// Identity indexes
		IdentityLinksByRemote: collections.NewMap(sb, types.IdentityLinksByRemoteKey, "identityLinksByRemote",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collections.StringValue),
		IdentityLinkCount: collections.NewMap(sb, types.IdentityLinkCountKey, "identityLinkCount",
			collections.StringKey, collections.Uint32Value),
		UnverifiedLinkExp: collections.NewKeySet(sb, types.UnverifiedLinkExpKey, "unverifiedLinkExp",
			collections.TripleKeyCodec(collections.Int64Key, collections.StringKey, collections.StringKey)),

		// Attestation expiration
		AttestationExp: collections.NewKeySet(sb, types.AttestationExpKey, "attestationExp",
			collections.TripleKeyCodec(collections.Int64Key, collections.StringKey, collections.StringKey)),

		// Verification/challenge windows
		VerificationWindow: collections.NewKeySet(sb, types.VerificationWindowKey, "verificationWindow",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		ChallengeWindow: collections.NewKeySet(sb, types.ChallengeWindowKey, "challengeWindow",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		// Arbiter resolution
		ArbiterHashCounts: collections.NewMap(sb, types.ArbiterHashCountsKey, "arbiterHashCounts",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			collections.Uint32Value),
		ArbiterResolutionQueue: collections.NewKeySet(sb, types.ArbiterResolutionQueueKey, "arbiterResolutionQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		ArbiterEscalationQueue: collections.NewKeySet(sb, types.ArbiterEscalationQueueKey, "arbiterEscalationQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		// Bridge unbonding
		BridgeUnbondingQueue: collections.NewKeySet(sb, types.BridgeUnbondingQueueKey, "bridgeUnbondingQueue",
			collections.TripleKeyCodec(collections.Int64Key, collections.StringKey, collections.StringKey)),

		// Rate limiting
		InboundRateLimits: collections.NewMap(sb, types.InboundRateLimitKey, "inboundRateLimits",
			collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
			collections.Uint64Value),
		OutboundRateLimits: collections.NewMap(sb, types.OutboundRateLimitKey, "outboundRateLimits",
			collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
			collections.Uint64Value),
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

// --- Late Keeper Wiring ---

func (k Keeper) SetCommonsKeeper(ck types.CommonsKeeper) {
	k.late.commonsKeeper = ck
}

func (k Keeper) SetRepKeeper(rk types.RepKeeper) {
	k.late.repKeeper = rk
}

func (k Keeper) SetNameKeeper(nk types.NameKeeper) {
	k.late.nameKeeper = nk
}


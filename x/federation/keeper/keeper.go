package keeper

import (
	"context"
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
//
// ibcKeeperFn lives here (not on Keeper directly) because the IBC keeper is
// constructed in app.go AFTER depinject runs, so the wiring happens via
// SetIBCKeeperFn() post-NewAppModule. If ibcKeeperFn were on the Keeper
// struct, the AppModule's value-copy would never see the assignment and
// SendFederationPacket would silently no-op forever.
type lateKeepers struct {
	commonsKeeper  types.CommonsKeeper
	repKeeper      types.RepKeeper
	nameKeeper     types.NameKeeper
	identityKeeper types.IdentityKeeper
	ibcKeeperFn    func() *ibckeeper.Keeper

	// serviceKeeper is the slice of x/service that federation consumes
	// for the bridge-operator lifecycle (Phase 2 of the federation→
	// service migration). Set after construction via SetServiceKeeper
	// in app.go to avoid a depinject cycle. nil during early-dev /
	// standalone test runs; production paths that depend on it must
	// nil-check via the accessor and either fail loudly or fall back
	// to legacy behavior.
	serviceKeeper types.ServiceKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	authority    []byte

	authKeeper types.AuthKeeper
	bankKeeper types.BankKeeper
	late       *lateKeepers

	Schema collections.Schema
	Params collections.Item[types.Params]
	Port   collections.Item[string]

	// --- Primary Collections ---

	Peers        collections.Map[string, types.Peer]
	PeerPolicies collections.Map[string, types.PeerPolicy]

	// BridgeBindings holds the federation-side per-(operator, peer)
	// record. Economic state (bond, status, unbonding) lives on the
	// service.Operator keyed by (address, service_type). Per Decision 1a
	// of the federation→service migration, one operator address can
	// hold multiple bindings under the same protocol — one per peer —
	// backed by a single shared service.Operator and bond.
	BridgeBindings collections.Map[collections.Pair[string, string], types.BridgeBinding]
	// VerifierActivity holds federation-specific per-verifier counters. The
	// generic bond/status record lives in x/rep as BondedRole
	// (ROLE_TYPE_FEDERATION_VERIFIER).
	VerifierActivity     collections.Map[string, types.VerifierActivity]
	VerificationRecords  collections.Map[uint64, types.VerificationRecord]
	ArbiterSubmissions   collections.Map[collections.Pair[uint64, string], types.ArbiterHashSubmission]
	Content              collections.Map[uint64, types.FederatedContent]
	IdentityLinks        collections.Map[collections.Pair[string, string], types.IdentityLink]
	PendingIdChallenges  collections.Map[collections.Pair[string, string], types.PendingIdentityChallenge]
	RepAttestations      collections.Map[collections.Pair[string, string], types.ReputationAttestation]
	OutboundAttestations collections.Map[uint64, types.OutboundAttestation]
	PeerRemovalQueue     collections.Map[string, types.PeerRemovalState]

	// --- Sequences ---

	ContentSeq        collections.Sequence
	OutboundAttestSeq collections.Sequence
	// ArbiterAnonSubSeq generates monotonic IDs for ArbiterSubmissions keys on
	// the anonymous (shield-dispatched) path so each accepted call gets a
	// unique entry instead of overwriting the prior one (FEDERATION-S2-5).
	// Per-identity uniqueness is enforced upstream by shield's per-content
	// nullifier scope.
	ArbiterAnonSubSeq collections.Sequence

	// --- Secondary Indexes ---

	ContentByPeer     collections.KeySet[collections.Pair[string, uint64]]
	ContentByType     collections.KeySet[collections.Pair[string, uint64]]
	ContentByCreator  collections.KeySet[collections.Pair[string, uint64]]
	ContentByHash     collections.Map[string, uint64]
	ContentExpiration collections.KeySet[collections.Pair[int64, uint64]]

	// BridgesByPeer indexes bindings by peer for peer-removal sweeps and
	// max_bridges_per_peer enforcement.
	BridgesByPeer collections.KeySet[collections.Pair[string, string]]

	// BindingsByOperator is the reverse index for hook handlers. Keyed
	// on (service_type, address, peer_id) — multi-valued by design so
	// AfterOperatorDissolved / Underfunded etc. can iterate all bindings
	// an operator holds under one service_type in O(N) without scanning
	// BridgesByPeer (federation→service migration, Phase 1).
	BindingsByOperator collections.KeySet[collections.Triple[string, string, string]]

	IdentityLinksByRemote collections.Map[collections.Pair[string, string], string]
	IdentityLinkCount     collections.Map[string, uint32]
	UnverifiedLinkExp     collections.KeySet[collections.Triple[int64, string, string]]

	AttestationExp collections.KeySet[collections.Triple[int64, string, string]]

	VerificationWindow collections.KeySet[collections.Pair[int64, uint64]]
	ChallengeWindow    collections.KeySet[collections.Pair[int64, uint64]]

	ArbiterHashCounts      collections.Map[collections.Pair[uint64, string], uint32]
	ArbiterResolutionQueue collections.KeySet[collections.Pair[int64, uint64]]
	ArbiterEscalationQueue collections.KeySet[collections.Pair[int64, uint64]]

	// EscalatedChallenges tracks Phase 2 (jury) lifecycle for
	// challenges escalated past their auto-resolve window. Keyed by
	// content_id; populated by MsgEscalateChallenge, drained by
	// MsgResolveEscalatedChallenge or the jury-deadline timeout sweep.
	EscalatedChallenges        collections.Map[uint64, types.EscalatedChallenge]
	EscalatedChallengeDeadline collections.KeySet[collections.Pair[int64, uint64]]

	// BridgeUnbondingQueue removed in Phase 4 of the federation→service
	// migration: x/service owns operator unbonding now (the EndBlocker
	// sweepUnderfunded + per-type unbonding_period_blocks). Federation
	// reacts to AfterOperatorDissolved/Retired hooks instead.

	InboundRateLimits  collections.Map[collections.Pair[string, int64], uint64]
	OutboundRateLimits collections.Map[collections.Pair[string, int64], uint64]

	// Global per-block caps. Map[block_height, count]; at most one
	// entry per direction per block, pruned by EndBlocker phase 13.
	InboundPerBlock  collections.Map[int64, uint64]
	OutboundPerBlock collections.Map[int64, uint64]
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
		// ibcKeeperFn goes through `late` (shared pointer) so post-NewAppModule
		// SetIBCKeeperFn() updates are visible to msgServer's value-copy.
		// depinject may pass nil here (IBC keeper is constructed later in app.go);
		// app.go calls SetIBCKeeperFn after that.
		late: &lateKeepers{ibcKeeperFn: ibcKeeperFn},

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Port:   collections.NewItem(sb, types.PortKey, "port", collections.StringValue),

		// Primary collections
		Peers: collections.NewMap(sb, types.PeersKey, "peers",
			collections.StringKey, codec.CollValue[types.Peer](cdc)),
		PeerPolicies: collections.NewMap(sb, types.PeerPoliciesKey, "peerPolicies",
			collections.StringKey, codec.CollValue[types.PeerPolicy](cdc)),
		BridgeBindings: collections.NewMap(sb, types.BridgeBindingsKey, "bridgeBindings",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.BridgeBinding](cdc)),
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
		ArbiterAnonSubSeq: collections.NewSequence(sb, types.ArbiterAnonSubSeqKey, "arbiterAnonSubmissionSequence"),

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
		BindingsByOperator: collections.NewKeySet(sb, types.BindingsByOperatorKey, "bindingsByOperator",
			collections.TripleKeyCodec(collections.StringKey, collections.StringKey, collections.StringKey)),

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

		// Phase 2 (jury) escalation lifecycle.
		EscalatedChallenges: collections.NewMap(sb, types.EscalatedChallengesKey, "escalatedChallenges",
			collections.Uint64Key, codec.CollValue[types.EscalatedChallenge](cdc)),
		EscalatedChallengeDeadline: collections.NewKeySet(sb, types.EscalatedChallengeDeadlineKey, "escalatedChallengeDeadline",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		// (BridgeUnbondingQueue removed in Phase 4 of the federation→
		// service migration; x/service owns operator unbonding now.)

		// Rate limiting
		InboundRateLimits: collections.NewMap(sb, types.InboundRateLimitKey, "inboundRateLimits",
			collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
			collections.Uint64Value),
		OutboundRateLimits: collections.NewMap(sb, types.OutboundRateLimitKey, "outboundRateLimits",
			collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
			collections.Uint64Value),
		InboundPerBlock: collections.NewMap(sb, types.InboundPerBlockKey, "inboundPerBlock",
			collections.Int64Key, collections.Uint64Value),
		OutboundPerBlock: collections.NewMap(sb, types.OutboundPerBlockKey, "outboundPerBlock",
			collections.Int64Key, collections.Uint64Value),
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

// SetIdentityKeeper wires the x/identity keeper post-depinject so the keeper
// can resolve the chain's bond denom at runtime.
func (k Keeper) SetIdentityKeeper(ik types.IdentityKeeper) {
	k.late.identityKeeper = ik
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity is not wired — every call site needs a real denom and
// silently returning a hardcoded literal would re-introduce the legacy
// mixed-state bug we just removed.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late == nil || k.late.identityKeeper == nil {
		panic("federation keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// SetServiceKeeper wires the x/service keeper after construction. Used
// by Phase 2 of the federation→service migration. The concrete
// service.Keeper doesn't satisfy our local `int`-source interface
// directly (its RegisterOperator takes servicekeeper.SlashSource); an
// adapter in app/service_adapters.go bridges that gap.
//
// **App init order constraint**: x/commons must be constructed before
// x/federation in app.go so the Operations Committee policy address
// exists at MsgRegisterBridge time. x/service must be constructed
// before x/federation so this Set* call has something to bind to. The
// EndBlocker order must also place x/service BEFORE x/federation so
// hook-fired binding state mutations settle before federation's own
// per-block work runs.
func (k Keeper) SetServiceKeeper(sk types.ServiceKeeper) {
	k.late.serviceKeeper = sk
}

// serviceKeeper returns the wired ServiceKeeper, or nil if running in
// standalone mode (no service wired). Callers MUST nil-check.
func (k Keeper) serviceKeeper() types.ServiceKeeper {
	if k.late == nil {
		return nil
	}
	return k.late.serviceKeeper
}

// SetIBCKeeperFn wires the IBC keeper accessor late, after the IBC keeper is
// constructed in app.go. Pass a closure rather than the *ibckeeper.Keeper
// directly so callers don't need to worry about the IBC keeper being nil at
// the time of supply (it isn't created until later in app.New()).
//
// The wiring goes through `late` (shared pointer) so the AppModule's value-copy
// of Keeper sees the assignment too — see lateKeepers comment.
func (k Keeper) SetIBCKeeperFn(fn func() *ibckeeper.Keeper) {
	k.late.ibcKeeperFn = fn
}

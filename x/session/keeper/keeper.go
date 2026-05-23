package keeper

import (
	"bytes"
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// lateKeepers holds dependencies wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	router         baseapp.MessageRouter
	commonsKeeper  types.CommonsKeeper
	identityKeeper types.IdentityKeeper
	// claimHooks are invoked at MsgClaimRecurringPull / MsgPullAllowance /
	// scheduled-oneshot-transfer-fire time to let downstream modules gate
	// on-the-wire transfers (e.g. x/commons council activation / term /
	// rate limit). Empty by default; hooks are registered via
	// SetClaimHooks from app.go post-depinject.
	claimHooks types.GrantClaimMultiHook
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	authority    []byte

	bankKeeper types.BankKeeper
	authKeeper types.AuthKeeper
	late       *lateKeepers

	// State Collections
	Schema collections.Schema
	Params collections.Item[types.Params]

	// Primary: id -> Grant
	Grants   collections.Map[uint64, types.Grant]
	GrantSeq collections.Sequence

	// Secondary indexes.
	// (granter, id) -> empty
	GrantsByGranter collections.KeySet[collections.Pair[string, uint64]]
	// (grantee, id) -> empty
	GrantsByGrantee collections.KeySet[collections.Pair[string, uint64]]
	// (expires_at_unix, id) -> empty — pruning index
	GrantsByExpiration collections.KeySet[collections.Pair[int64, uint64]]
	// (type, granter, id) -> empty — per-type listings + per-(granter, type) caps
	GrantsByTypeAndGranter collections.KeySet[collections.Triple[int32, string, uint64]]
	// (granter, type) -> active count — O(1) caps and invariant target
	ActiveGrantCountByType collections.Map[collections.Pair[string, int32], uint32]

	// Back-compat lookup: (granter, grantee) -> grant_id for SESSION_KEY
	// grants only. Enforces the legacy invariant of at most one active
	// session key per (granter, grantee) pair, and keeps GetSession O(1).
	SessionKeyByPair collections.Map[collections.Pair[string, string], uint64]

	// Per-grant UTC-day spend buckets backing the RECURRING_PULL
	// max_per_epoch self-throttle. (grant_id, utc_day_index) ->
	// sdk.Int (string-encoded). Bounded to O(7) entries per active grant
	// in steady state via lazy pruning.
	EpochSpendByGrant collections.Map[collections.Pair[uint64, int64], string]

	// OneshotGasDeposit: grant_id -> Coin. SPARK deposit posted at
	// MsgCreateGrant time for SCHEDULED_ONESHOT grants. Refunded on
	// Revoke / Decline / paused-TTL auto-revoke; sent to fee collector
	// on FIRED.
	OneshotGasDeposit collections.Map[uint64, sdk.Coin]

	// PausedOneshotByPauseTime: (pause_unix, grant_id) -> empty.
	// Drives the EndBlocker paused-TTL auto-revoke pass.
	PausedOneshotByPauseTime collections.KeySet[collections.Pair[int64, uint64]]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	bankKeeper types.BankKeeper,
	authKeeper types.AuthKeeper,
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
		bankKeeper:   bankKeeper,
		authKeeper:   authKeeper,
		late:         &lateKeepers{},

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		Grants: collections.NewMap(
			sb, types.GrantsKey, "grants",
			collections.Uint64Key,
			codec.CollValue[types.Grant](cdc),
		),
		GrantSeq: collections.NewSequence(sb, types.GrantSeqKey, "grant_seq"),

		GrantsByGranter: collections.NewKeySet(
			sb, types.GrantsByGranterKey, "grants_by_granter",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		GrantsByGrantee: collections.NewKeySet(
			sb, types.GrantsByGranteeKey, "grants_by_grantee",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		GrantsByExpiration: collections.NewKeySet(
			sb, types.GrantsByExpirationKey, "grants_by_expiration",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		GrantsByTypeAndGranter: collections.NewKeySet(
			sb, types.GrantsByTypeAndGranterKey, "grants_by_type_and_granter",
			collections.TripleKeyCodec(collections.Int32Key, collections.StringKey, collections.Uint64Key),
		),
		ActiveGrantCountByType: collections.NewMap(
			sb, types.ActiveGrantCountByTypeKey, "active_grant_count_by_type",
			collections.PairKeyCodec(collections.StringKey, collections.Int32Key),
			collections.Uint32Value,
		),

		SessionKeyByPair: collections.NewMap(
			sb, types.SessionKeyByPairKey, "session_key_by_pair",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collections.Uint64Value,
		),

		EpochSpendByGrant: collections.NewMap(
			sb, types.EpochSpendByGrantKey, "epoch_spend_by_grant",
			collections.PairKeyCodec(collections.Uint64Key, collections.Int64Key),
			collections.StringValue,
		),

		OneshotGasDeposit: collections.NewMap(
			sb, types.OneshotGasDepositKey, "oneshot_gas_deposit",
			collections.Uint64Key,
			codec.CollValue[sdk.Coin](cdc),
		),

		PausedOneshotByPauseTime: collections.NewKeySet(
			sb, types.PausedOneshotByPauseTimeKey, "paused_oneshot_by_pause_time",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// SetRouter wires the MsgServiceRouter after app build for inner message dispatch.
func (k Keeper) SetRouter(router baseapp.MessageRouter) {
	k.late.router = router
}

// SetCommonsKeeper wires the optional CommonsKeeper used for council-gated
// operational parameter updates. Wired in app.go post-depinject.
func (k Keeper) SetCommonsKeeper(ck types.CommonsKeeper) {
	k.late.commonsKeeper = ck
}

// SetIdentityKeeper late-binds the identity keeper for federated-denom
// resolution. Called from app.go post-depinject.
func (k Keeper) SetIdentityKeeper(idk types.IdentityKeeper) {
	k.late.identityKeeper = idk
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity isn't wired: no silent fallback to a hardcoded literal.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("session keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// DreamDenom returns the chain's DREAM denom from the wired identity keeper.
// Panics if identity isn't wired: no silent fallback to a hardcoded literal.
func (k Keeper) DreamDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("session keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.DreamDenom(ctx)
}

// SetClaimHooks registers one or more GrantClaimHook implementations
// that will be invoked at every on-the-wire claim/pull/fire-transfer
// op. Order matters — hooks fire in registration order and the first
// veto short-circuits.
//
// Re-invoking SetClaimHooks replaces the entire list (the late-binding
// pattern doesn't currently support partial updates; modules wire
// themselves once at app build time). Pass `types.GrantClaimMultiHook{}`
// to clear.
//
// Safe to call before or after `NewAppModule`; the lateKeepers pointer
// is shared with value-copies of Keeper.
func (k Keeper) SetClaimHooks(hooks ...types.GrantClaimHook) {
	k.late.claimHooks = append(types.GrantClaimMultiHook(nil), hooks...)
}

// invokePreCheckHooks fans out PreCheck to every registered claim
// hook against (grant, amount). Returns the first hook error verbatim
// (no wrapping) so the CLI / indexer surface can attribute the
// failure cleanly. Returns nil immediately when no hooks are
// registered. A non-nil return vetoes the claim before any bank send.
func (k Keeper) invokePreCheckHooks(ctx context.Context, grant types.Grant, amount sdk.Coins) error {
	if len(k.late.claimHooks) == 0 {
		return nil
	}
	return k.late.claimHooks.PreCheck(ctx, grant, amount)
}

// invokePostCommitHooks fans out PostCommit to every registered claim
// hook against (grant, amount). Returns the first hook error verbatim
// so the CLI / indexer surface can attribute the failure cleanly.
// Returns nil immediately when no hooks are registered.
//
// PostCommit errors are tx-halting by contract: callers MUST return
// the error to the handler caller so the SDK rolls back the bank
// send + state writes done since PreCheck. See GrantClaimHook in
// types/hooks.go for the security rationale.
func (k Keeper) invokePostCommitHooks(ctx context.Context, grant types.Grant, amount sdk.Coins) error {
	if len(k.late.claimHooks) == 0 {
		return nil
	}
	return k.late.claimHooks.PostCommit(ctx, grant, amount)
}

// isCouncilAuthorized returns true when addr is either the governance authority
// or (when CommonsKeeper is wired) a member/policy of the given council/committee.
func (k Keeper) isCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(addr)
	if err != nil {
		return false
	}
	if bytes.Equal(k.authority, addrBytes) {
		return true
	}
	if k.late.commonsKeeper == nil {
		return false
	}
	return k.late.commonsKeeper.IsCouncilAuthorized(ctx, addr, council, committee)
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// GetSession is a legacy compatibility helper used by the ante handler and the
// Session/SessionsByGranter/SessionsByGrantee queries. Looks up the active
// SESSION_KEY-type Grant for the (granter, grantee) pair and projects it into
// the legacy Session shape. Returns types.ErrSessionNotFound if no active
// session-key grant exists.
func (k Keeper) GetSession(ctx context.Context, granter, grantee string) (types.Session, error) {
	id, err := k.SessionKeyByPair.Get(ctx, collections.Join(granter, grantee))
	if err != nil {
		return types.Session{}, types.ErrSessionNotFound
	}
	grant, err := k.Grants.Get(ctx, id)
	if err != nil {
		return types.Session{}, types.ErrSessionNotFound
	}
	return projectSession(grant)
}

// UpdateSessionSpent increments a session's spent counter. Used by the ante
// handler to debit the granter's spend-limit budget when fees are paid.
// Operates on the underlying SESSION_KEY grant.
func (k Keeper) UpdateSessionSpent(ctx context.Context, granter, grantee string, feeAmount sdk.Coin) error {
	id, err := k.SessionKeyByPair.Get(ctx, collections.Join(granter, grantee))
	if err != nil {
		return err
	}
	grant, err := k.Grants.Get(ctx, id)
	if err != nil {
		return err
	}
	sk := grant.GetSessionKey()
	if sk == nil {
		return fmt.Errorf("grant %d is not a session key", id)
	}
	sk.Spent = sk.Spent.Add(feeAmount)
	grant.Payload = &types.Grant_SessionKey{SessionKey: sk}
	return k.Grants.Set(ctx, id, grant)
}

// projectSession derives the legacy Session view from a SESSION_KEY-type
// Grant. Used by GetSession and the legacy query handlers.
func projectSession(g types.Grant) (types.Session, error) {
	sk := g.GetSessionKey()
	if sk == nil {
		return types.Session{}, fmt.Errorf("grant %d is not a session key (type=%s)", g.Id, g.Type)
	}
	return types.Session{
		Granter:         g.Granter,
		Grantee:         g.Grantee,
		AllowedMsgTypes: sk.AllowedMsgTypes,
		SpendLimit:      sk.SpendLimit,
		Spent:           sk.Spent,
		Expiration:      g.ExpiresAt,
		CreatedAt:       g.CreatedAt,
		LastUsedAt:      sk.LastUsedAt,
		ExecCount:       sk.ExecCount,
		MaxExecCount:    sk.MaxExecCount,
	}, nil
}

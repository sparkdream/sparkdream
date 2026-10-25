package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/vault/types"
)

// lateDeps are wired from app.go after depinject (never through depinject,
// to stay out of every dependency cycle).
type lateDeps struct {
	router   baseapp.MessageRouter
	rep      types.RepKeeper
	identity types.IdentityKeeper
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

	bankKeeper types.BankKeeper
	authKeeper types.AuthKeeper
	late       *lateDeps

	Vault         collections.Map[uint64, types.Vault]
	VaultProposal collections.Map[uint64, types.VaultProposal]
	VaultSeq      collections.Sequence
	ProposalSeq   collections.Sequence
	// VaultByAddress: address -> vault_id.
	VaultByAddress collections.Map[string, uint64]
	// VaultsBySigner: (signer, vault_id). Keyed index, never iterated in full.
	VaultsBySigner collections.KeySet[collections.Pair[string, uint64]]
	// PendingByVault: (vault_id, proposal_id) of PENDING proposals.
	PendingByVault collections.KeySet[collections.Pair[uint64, uint64]]
	// PendingByProposer: (vault_id, proposer, proposal_id) of PENDING proposals.
	PendingByProposer collections.KeySet[collections.Triple[uint64, string, uint64]]
	// ExpiryQueue: (expires_at, proposal_id) of PENDING proposals.
	ExpiryQueue collections.KeySet[collections.Pair[int64, uint64]]
	// RetentionQueue: (resolved_at, proposal_id) of resolved proposals.
	RetentionQueue collections.KeySet[collections.Pair[int64, uint64]]
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

		bankKeeper: bankKeeper,
		authKeeper: authKeeper,
		late:       &lateDeps{},
		Params:     collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Vault:      collections.NewMap(sb, types.VaultKey, "vault", collections.Uint64Key, codec.CollValue[types.Vault](cdc)), VaultProposal: collections.NewMap(sb, types.VaultProposalKey, "vaultProposal", collections.Uint64Key, codec.CollValue[types.VaultProposal](cdc)),
		VaultSeq:          collections.NewSequence(sb, types.VaultSeqKey, "vaultSeq"),
		ProposalSeq:       collections.NewSequence(sb, types.ProposalSeqKey, "proposalSeq"),
		VaultByAddress:    collections.NewMap(sb, types.VaultByAddressKey, "vaultByAddress", collections.StringKey, collections.Uint64Value),
		VaultsBySigner:    collections.NewKeySet(sb, types.VaultsBySignerKey, "vaultsBySigner", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		PendingByVault:    collections.NewKeySet(sb, types.PendingByVaultKey, "pendingByVault", collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		PendingByProposer: collections.NewKeySet(sb, types.PendingByProposerKey, "pendingByProposer", collections.TripleKeyCodec(collections.Uint64Key, collections.StringKey, collections.Uint64Key)),
		ExpiryQueue:       collections.NewKeySet(sb, types.ExpiryQueueKey, "expiryQueue", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		RetentionQueue:    collections.NewKeySet(sb, types.RetentionQueueKey, "retentionQueue", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
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

// SetRouter wires the MsgServiceRouter for proposal execution.
func (k Keeper) SetRouter(router baseapp.MessageRouter) { k.late.router = router }

// SetRepKeeper wires the x/rep alias contract and personal-address check.
func (k Keeper) SetRepKeeper(rk types.RepKeeper) { k.late.rep = rk }

// SetIdentityKeeper wires the chain identity (bond denom).
func (k Keeper) SetIdentityKeeper(ik types.IdentityKeeper) { k.late.identity = ik }

// BondDenom returns the chain's SPARK denom.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identity == nil {
		panic("vault keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identity.BondDenom(ctx)
}

// IsVault reports whether addr is a vault address. Wired into x/rep as its
// vault checker: a vault can never be invited, accept an invitation or be the
// target of a member migration.
func (k Keeper) IsVault(ctx context.Context, addr string) bool {
	has, err := k.VaultByAddress.Has(ctx, addr)
	return err == nil && has
}

// canActAs is the alias-aware authorization check; equality when x/rep is
// not wired (unit tests).
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if k.late.rep == nil {
		return signer != "" && signer == stored
	}
	return k.late.rep.CanActAs(ctx, signer, stored)
}

// canonical resolves a possibly migrated address; identity without x/rep.
func (k Keeper) canonical(ctx context.Context, addr string) string {
	if k.late.rep == nil {
		return addr
	}
	return k.late.rep.Canonical(ctx, addr)
}

// allAddressesOf lists every address of a member; just addr without x/rep.
func (k Keeper) allAddressesOf(ctx context.Context, addr string) []string {
	if k.late.rep == nil {
		return []string{addr}
	}
	return k.late.rep.AllAddressesOf(ctx, addr)
}

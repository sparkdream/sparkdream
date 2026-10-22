package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/artifact/types"
)

type (
	// U64Pair keys a token: (class_id, token_id).
	U64Pair = collections.Pair[uint64, uint64]
	// AddrTokenKey keys a per-address token index: (address, class_id, token_id).
	AddrTokenKey = collections.Triple[string, uint64, uint64]
	// TimeTokenKey keys a token expiry index: (expires_at, class_id, token_id).
	TimeTokenKey = collections.Triple[int64, uint64, uint64]
)

// lateKeepers are the cross-module dependencies wired after depinject.
type lateKeepers struct {
	identity types.IdentityKeeper
	rep      types.RepKeeper
	commons  types.CommonsKeeper
	distr    types.DistrKeeper
	gnovm    types.GnoVMKeeper
	session  types.SessionKeeper
	season   types.SeasonKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	authKeeper types.AuthKeeper
	bankKeeper types.BankKeeper

	// Late-wired from app.go (Set*Keeper) to avoid depinject cycles. Held
	// by pointer so the AppModule's value copy of the keeper sees the
	// wiring too (docs/development-conventions.md, AppModule Value-Copy Bug).
	late *lateKeepers

	Schema collections.Schema
	Params collections.Item[types.Params]

	ClassSeq            collections.Sequence
	Classes             collections.Map[uint64, types.Class]
	ClassesByOwner      collections.KeySet[collections.Pair[string, uint64]]
	ClassCountByCreator collections.Map[string, uint64]

	Tokens          collections.Map[U64Pair, types.Token]
	TokensByOwner   collections.KeySet[AddrTokenKey]
	PublicMintCount collections.Map[collections.Pair[uint64, string], uint64]
	ReceivePolicies collections.Map[string, uint64]

	PendingTransfers collections.Map[U64Pair, types.PendingTransfer]
	PendingMints     collections.Map[U64Pair, types.PendingMint]
	InboxByRecipient collections.Map[AddrTokenKey, uint64] // value: InboxKind
	OutboxBySender   collections.Map[AddrTokenKey, uint64] // value: InboxKind
	PendingPairCount collections.Map[collections.Pair[string, string], uint64]
	InboxCount       collections.Map[string, uint64]
	PendingExpiry    collections.KeySet[TimeTokenKey]

	Listings         collections.Map[U64Pair, types.Listing]
	ListingsBySeller collections.KeySet[AddrTokenKey]
	ListingExpiry    collections.KeySet[TimeTokenKey]

	ClassCancelQueue collections.Map[uint64, uint64] // class_id -> next token id to visit
	ClassScrubQueue  collections.Map[uint64, uint64]

	PendingClassOwners collections.Map[uint64, types.PendingClassOwner]
	PendingOwnerExpiry collections.KeySet[collections.Pair[int64, uint64]]

	HideSeq            collections.Sequence
	HideRecords        collections.Map[uint64, types.HideRecord]
	HideByTarget       collections.Map[U64Pair, uint64] // open (PENDING) record per target
	HidesByTargetAll   collections.KeySet[collections.Triple[uint64, uint64, uint64]]
	HideExpiry         collections.KeySet[collections.Pair[int64, uint64]]
	SentinelDailyHides collections.Map[collections.Pair[string, int64], uint64]

	// FailedExpiry records EndBlocker entries that errored, keyed by
	// (pass, key), so they never block the queue.
	FailedExpiry collections.KeySet[collections.Pair[string, string]]

	// TokenExpiry indexes EXPIRY_MODE_BURN tokens by (expires_at, class, token).
	TokenExpiry collections.KeySet[TimeTokenKey]
	// MediaProposals holds open media versions offered to a token's holder,
	// keyed (class_id, token_id, proposal_id).
	MediaProposals collections.Map[collections.Triple[uint64, uint64, uint64], types.MediaProposal]
	// ClassReleaseQueue drains escrowed public-mint payments of a generative
	// class after its seed reveal (class_id -> next token id to visit).
	ClassReleaseQueue collections.Map[uint64, uint64]

	// Editions (docs/x-artifact-spec.md §7.20): (class, edition) records,
	// (class, edition, owner) balances with an owner index, pending unit
	// items keyed by id, and unit listings with edition, seller and expiry
	// indexes. CertificatesByInitiative is (initiative, class, edition).
	Editions                 collections.Map[U64Pair, types.Edition]
	Balances                 collections.Map[BalanceKey, types.Balance]
	BalancesByOwner          collections.KeySet[AddrTokenKey]
	PendingUnitSeq           collections.Sequence
	PendingUnits             collections.Map[uint64, types.PendingUnits]
	UnitListingSeq           collections.Sequence
	UnitListings             collections.Map[uint64, types.UnitListing]
	UnitListingsByEdition    collections.KeySet[collections.Triple[uint64, uint64, uint64]]
	UnitListingsBySeller     collections.KeySet[collections.Pair[string, uint64]]
	UnitListingExpiry        collections.KeySet[collections.Pair[int64, uint64]]
	CertificatesByInitiative collections.KeySet[collections.Triple[uint64, uint64, uint64]]

	// CraftRecipes are keyed (target class, recipe id).
	CraftRecipes collections.Map[U64Pair, types.CraftRecipe]

	// Rentals and rental offers, keyed by token, with renter and expiry
	// indexes.
	Rentals           collections.Map[U64Pair, types.Rental]
	RentalsByUser     collections.KeySet[AddrTokenKey]
	RentalExpiry      collections.KeySet[TimeTokenKey]
	RentalOffers      collections.Map[U64Pair, types.RentalOffer]
	RentalOfferExpiry collections.KeySet[TimeTokenKey]

	// Children lists each token's direct children: (parent, child).
	Children collections.KeySet[ChildKey]

	// HarbergerDue indexes the next tax pull of each Harberger token.
	HarbergerDue collections.KeySet[TimeTokenKey]

	// Escrowed offers, keyed by id, with (class, token, id) (token 0 =
	// collection offers), (bidder, id) and (expires_at, id) indexes.
	OfferSeq       collections.Sequence
	Offers         collections.Map[uint64, types.Offer]
	OffersByToken  collections.KeySet[collections.Triple[uint64, uint64, uint64]]
	OffersByBidder collections.KeySet[collections.Pair[string, uint64]]
	OfferExpiry    collections.KeySet[collections.Pair[int64, uint64]]

	// Auctions, keyed by id, with token, end-time and seller indexes.
	AuctionSeq       collections.Sequence
	Auctions         collections.Map[uint64, types.Auction]
	AuctionByToken   collections.Map[U64Pair, uint64]
	AuctionEnds      collections.KeySet[collections.Pair[int64, uint64]]
	AuctionsBySeller collections.KeySet[collections.Pair[string, uint64]]
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
	u64pair := collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)
	addrToken := collections.TripleKeyCodec(collections.StringKey, collections.Uint64Key, collections.Uint64Key)
	timeToken := collections.TripleKeyCodec(collections.Int64Key, collections.Uint64Key, collections.Uint64Key)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,
		authKeeper:   authKeeper,
		bankKeeper:   bankKeeper,
		late:         &lateKeepers{},

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		ClassSeq:            collections.NewSequence(sb, types.ClassSeqKey, "class_seq"),
		Classes:             collections.NewMap(sb, types.ClassesKey, "classes", collections.Uint64Key, codec.CollValue[types.Class](cdc)),
		ClassesByOwner:      collections.NewKeySet(sb, types.ClassesByOwnerKey, "classes_by_owner", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		ClassCountByCreator: collections.NewMap(sb, types.ClassCountByCreatorKey, "class_count_by_creator", collections.StringKey, collections.Uint64Value),

		Tokens:          collections.NewMap(sb, types.TokensKey, "tokens", u64pair, codec.CollValue[types.Token](cdc)),
		TokensByOwner:   collections.NewKeySet(sb, types.TokensByOwnerKey, "tokens_by_owner", addrToken),
		PublicMintCount: collections.NewMap(sb, types.PublicMintCountKey, "public_mint_count", collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), collections.Uint64Value),
		ReceivePolicies: collections.NewMap(sb, types.ReceivePoliciesKey, "receive_policies", collections.StringKey, collections.Uint64Value),

		PendingTransfers: collections.NewMap(sb, types.PendingTransfersKey, "pending_transfers", u64pair, codec.CollValue[types.PendingTransfer](cdc)),
		PendingMints:     collections.NewMap(sb, types.PendingMintsKey, "pending_mints", u64pair, codec.CollValue[types.PendingMint](cdc)),
		InboxByRecipient: collections.NewMap(sb, types.InboxByRecipientKey, "inbox_by_recipient", addrToken, collections.Uint64Value),
		OutboxBySender:   collections.NewMap(sb, types.OutboxBySenderKey, "outbox_by_sender", addrToken, collections.Uint64Value),
		PendingPairCount: collections.NewMap(sb, types.PendingPairCountKey, "pending_pair_count", collections.PairKeyCodec(collections.StringKey, collections.StringKey), collections.Uint64Value),
		InboxCount:       collections.NewMap(sb, types.InboxCountKey, "inbox_count", collections.StringKey, collections.Uint64Value),
		PendingExpiry:    collections.NewKeySet(sb, types.PendingExpiryKey, "pending_expiry", timeToken),

		Listings:         collections.NewMap(sb, types.ListingsKey, "listings", u64pair, codec.CollValue[types.Listing](cdc)),
		ListingsBySeller: collections.NewKeySet(sb, types.ListingsBySellerKey, "listings_by_seller", addrToken),
		ListingExpiry:    collections.NewKeySet(sb, types.ListingExpiryKey, "listing_expiry", timeToken),

		ClassCancelQueue: collections.NewMap(sb, types.ClassCancelQueueKey, "class_cancel_queue", collections.Uint64Key, collections.Uint64Value),
		ClassScrubQueue:  collections.NewMap(sb, types.ClassScrubQueueKey, "class_scrub_queue", collections.Uint64Key, collections.Uint64Value),

		PendingClassOwners: collections.NewMap(sb, types.PendingClassOwnersKey, "pending_class_owners", collections.Uint64Key, codec.CollValue[types.PendingClassOwner](cdc)),
		PendingOwnerExpiry: collections.NewKeySet(sb, types.PendingOwnerExpiryKey, "pending_owner_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		HideSeq:            collections.NewSequence(sb, types.HideSeqKey, "hide_seq"),
		HideRecords:        collections.NewMap(sb, types.HideRecordsKey, "hide_records", collections.Uint64Key, codec.CollValue[types.HideRecord](cdc)),
		HideByTarget:       collections.NewMap(sb, types.HideByTargetKey, "hide_by_target", u64pair, collections.Uint64Value),
		HidesByTargetAll:   collections.NewKeySet(sb, types.HidesByTargetAllKey, "hides_by_target_all", collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.Uint64Key)),
		HideExpiry:         collections.NewKeySet(sb, types.HideExpiryKey, "hide_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		SentinelDailyHides: collections.NewMap(sb, types.SentinelDailyHidesKey, "sentinel_daily_hides", collections.PairKeyCodec(collections.StringKey, collections.Int64Key), collections.Uint64Value),

		FailedExpiry: collections.NewKeySet(sb, types.FailedExpiryKey, "failed_expiry", collections.PairKeyCodec(collections.StringKey, collections.StringKey)),

		TokenExpiry:       collections.NewKeySet(sb, types.TokenExpiryKey, "token_expiry", timeToken),
		MediaProposals:    collections.NewMap(sb, types.MediaProposalsKey, "media_proposals", collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.Uint64Key), codec.CollValue[types.MediaProposal](cdc)),
		ClassReleaseQueue: collections.NewMap(sb, types.ClassReleaseQueueKey, "class_release_queue", collections.Uint64Key, collections.Uint64Value),

		Editions:                 collections.NewMap(sb, types.EditionsKey, "editions", u64pair, codec.CollValue[types.Edition](cdc)),
		Balances:                 collections.NewMap(sb, types.BalancesKey, "balances", collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.StringKey), codec.CollValue[types.Balance](cdc)),
		BalancesByOwner:          collections.NewKeySet(sb, types.BalancesByOwnerKey, "balances_by_owner", addrToken),
		PendingUnitSeq:           collections.NewSequence(sb, types.PendingUnitSeqKey, "pending_unit_seq"),
		PendingUnits:             collections.NewMap(sb, types.PendingUnitsKey, "pending_units", collections.Uint64Key, codec.CollValue[types.PendingUnits](cdc)),
		UnitListingSeq:           collections.NewSequence(sb, types.UnitListingSeqKey, "unit_listing_seq"),
		UnitListings:             collections.NewMap(sb, types.UnitListingsKey, "unit_listings", collections.Uint64Key, codec.CollValue[types.UnitListing](cdc)),
		UnitListingsByEdition:    collections.NewKeySet(sb, types.UnitListingsByEditionKey, "unit_listings_by_edition", collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.Uint64Key)),
		UnitListingsBySeller:     collections.NewKeySet(sb, types.UnitListingsBySellerKey, "unit_listings_by_seller", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		UnitListingExpiry:        collections.NewKeySet(sb, types.UnitListingExpiryKey, "unit_listing_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		CertificatesByInitiative: collections.NewKeySet(sb, types.CertificatesByInitiativeKey, "certificates_by_initiative", collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.Uint64Key)),

		CraftRecipes: collections.NewMap(sb, types.CraftRecipesKey, "craft_recipes", u64pair, codec.CollValue[types.CraftRecipe](cdc)),

		Rentals:           collections.NewMap(sb, types.RentalsKey, "rentals", u64pair, codec.CollValue[types.Rental](cdc)),
		RentalsByUser:     collections.NewKeySet(sb, types.RentalsByUserKey, "rentals_by_user", addrToken),
		RentalExpiry:      collections.NewKeySet(sb, types.RentalExpiryKey, "rental_expiry", timeToken),
		RentalOffers:      collections.NewMap(sb, types.RentalOffersKey, "rental_offers", u64pair, codec.CollValue[types.RentalOffer](cdc)),
		RentalOfferExpiry: collections.NewKeySet(sb, types.RentalOfferExpiryKey, "rental_offer_expiry", timeToken),

		Children: collections.NewKeySet(sb, types.ChildrenKey, "children", collections.PairKeyCodec(u64pair, u64pair)),

		HarbergerDue: collections.NewKeySet(sb, types.HarbergerDueKey, "harberger_due", timeToken),

		OfferSeq:       collections.NewSequence(sb, types.OfferSeqKey, "offer_seq"),
		Offers:         collections.NewMap(sb, types.OffersKey, "offers", collections.Uint64Key, codec.CollValue[types.Offer](cdc)),
		OffersByToken:  collections.NewKeySet(sb, types.OffersByTokenKey, "offers_by_token", collections.TripleKeyCodec(collections.Uint64Key, collections.Uint64Key, collections.Uint64Key)),
		OffersByBidder: collections.NewKeySet(sb, types.OffersByBidderKey, "offers_by_bidder", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		OfferExpiry:    collections.NewKeySet(sb, types.OfferExpiryKey, "offer_expiry", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		AuctionSeq:       collections.NewSequence(sb, types.AuctionSeqKey, "auction_seq"),
		Auctions:         collections.NewMap(sb, types.AuctionsKey, "auctions", collections.Uint64Key, codec.CollValue[types.Auction](cdc)),
		AuctionByToken:   collections.NewMap(sb, types.AuctionByTokenKey, "auction_by_token", u64pair, collections.Uint64Value),
		AuctionEnds:      collections.NewKeySet(sb, types.AuctionEndsKey, "auction_ends", collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		AuctionsBySeller: collections.NewKeySet(sb, types.AuctionsBySellerKey, "auctions_by_seller", collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
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

// SetIdentityKeeper wires the identity keeper post-depinject.
func (k Keeper) SetIdentityKeeper(ik types.IdentityKeeper) { k.late.identity = ik }

// SetRepKeeper wires the rep keeper post-depinject.
func (k Keeper) SetRepKeeper(rk types.RepKeeper) { k.late.rep = rk }

// SetCommonsKeeper wires the commons keeper post-depinject.
func (k Keeper) SetCommonsKeeper(ck types.CommonsKeeper) { k.late.commons = ck }

// SetDistrKeeper wires the distribution adapter post-depinject.
func (k Keeper) SetDistrKeeper(dk types.DistrKeeper) { k.late.distr = dk }

// SetGnoVMKeeper wires the x/gnovm adapter renderers evaluate through.
func (k Keeper) SetGnoVMKeeper(gk types.GnoVMKeeper) { k.late.gnovm = gk }

// SetSessionKeeper wires the x/session adapter Harberger taxes are pulled
// through.
func (k Keeper) SetSessionKeeper(sk types.SessionKeeper) { k.late.session = sk }

// SetSeasonKeeper wires the x/season adapter renderer state v2 reads.
func (k Keeper) SetSeasonKeeper(sk types.SeasonKeeper) { k.late.season = sk }

// GetRepKeeper returns the rep keeper (simulation use).
func (k Keeper) GetRepKeeper() types.RepKeeper { return k.late.rep }

// ModuleAddress returns the module account address (deposit escrow).
func (k Keeper) ModuleAddress() sdk.AccAddress { return authtypes.NewModuleAddress(types.ModuleName) }

// BondDenom returns the chain's bond denom. Panics if identity isn't wired;
// every call site needs a real denom.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identity == nil {
		panic("artifact keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identity.BondDenom(ctx)
}

// DreamDenom returns the chain's DREAM denom ("" when identity isn't wired).
func (k Keeper) DreamDenom(ctx context.Context) string {
	if k.late.identity == nil {
		return ""
	}
	return k.late.identity.DreamDenom(ctx)
}

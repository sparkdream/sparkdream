package artifact

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/artifact/types"
)

func pos(fields ...string) []*autocliv1.PositionalArgDescriptor {
	out := make([]*autocliv1.PositionalArgDescriptor, len(fields))
	for i, f := range fields {
		out[i] = &autocliv1.PositionalArgDescriptor{ProtoField: f}
	}
	return out
}

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
//
// Message-typed fields (flags, mint policy, metadata, entries, refs) are
// passed as JSON flags, e.g.
//
//	mint 1 CC0-1.0 --entries '{"recipient":"sprkdrm1...","metadata":{"name":"aurora"}}'
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: types.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{RpcMethod: "Params", Use: "params", Short: "Shows the parameters of the module"},
				{RpcMethod: "Class", Use: "class [class-id]", Short: "Show a class", PositionalArgs: pos("class_id")},
				{RpcMethod: "Classes", Use: "classes", Short: "List classes"},
				{RpcMethod: "ClassesByOwner", Use: "classes-by-owner [owner]", Short: "List the classes an address owns", PositionalArgs: pos("owner")},
				{RpcMethod: "Token", Use: "token [class-id] [token-id]", Short: "Show a token", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Tokens", Use: "tokens [class-id]", Short: "List the tokens of a class", PositionalArgs: pos("class_id")},
				{RpcMethod: "TokensByOwner", Use: "tokens-by-owner [owner]", Short: "List the tokens an address owns (--class-id to filter)", PositionalArgs: pos("owner")},
				{RpcMethod: "Owner", Use: "owner [class-id] [token-id]", Short: "Show a token's owner", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Supply", Use: "supply [class-id]", Short: "Show a class's supply counters", PositionalArgs: pos("class_id")},
				{RpcMethod: "Listing", Use: "listing [class-id] [token-id]", Short: "Show a token's active listing", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Listings", Use: "listings", Short: "List active listings (--class-id to filter)"},
				{RpcMethod: "ListingsBySeller", Use: "listings-by-seller [seller]", Short: "List a seller's active listings", PositionalArgs: pos("seller")},
				{RpcMethod: "Inbox", Use: "inbox [address]", Short: "List an address's open incoming items", PositionalArgs: pos("address")},
				{RpcMethod: "Outbox", Use: "outbox [address]", Short: "List an address's open outgoing items", PositionalArgs: pos("address")},
				{RpcMethod: "ReceivePolicy", Use: "receive-policy [address]", Short: "Show an address's effective receive policy", PositionalArgs: pos("address")},
				{RpcMethod: "PendingClassOwner", Use: "pending-class-owner [class-id]", Short: "Show a class's open handover proposal", PositionalArgs: pos("class_id")},
				{RpcMethod: "HideRecord", Use: "hide-record [hide-id]", Short: "Show a hide record", PositionalArgs: pos("hide_id")},
				{RpcMethod: "HideRecordsByTarget", Use: "hide-records-by-target [class-id] [token-id]", Short: "List hide records for a class (token-id 0) or token", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "PublicMintQuote", Use: "public-mint-quote [class-id] [buyer] [quantity]", Short: "Price a public mint", PositionalArgs: pos("class_id", "buyer", "quantity")},
				{RpcMethod: "MediaProposals", Use: "media-proposals [class-id] [token-id]", Short: "List a token's open media proposals", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Edition", Use: "edition [class-id] [edition-id]", Short: "Show an edition", PositionalArgs: pos("class_id", "edition_id")},
				{RpcMethod: "Editions", Use: "editions [class-id]", Short: "List a class's editions", PositionalArgs: pos("class_id")},
				{RpcMethod: "Balance", Use: "balance [class-id] [edition-id] [owner]", Short: "Show an edition balance and its claimable retro funding", PositionalArgs: pos("class_id", "edition_id", "owner")},
				{RpcMethod: "BalancesByOwner", Use: "balances-by-owner [owner]", Short: "List every edition balance an address holds", PositionalArgs: pos("owner")},
				{RpcMethod: "EditionHolders", Use: "edition-holders [class-id] [edition-id]", Short: "List an edition's balances", PositionalArgs: pos("class_id", "edition_id")},
				{RpcMethod: "UnitListing", Use: "unit-listing [listing-id]", Short: "Show a unit listing", PositionalArgs: pos("listing_id")},
				{RpcMethod: "UnitListings", Use: "unit-listings [class-id] [edition-id]", Short: "List an edition's active unit listings", PositionalArgs: pos("class_id", "edition_id")},
				{RpcMethod: "CraftRecipes", Use: "craft-recipes [class-id]", Short: "List a class's crafting recipes", PositionalArgs: pos("class_id")},
				{RpcMethod: "Rental", Use: "rental [class-id] [token-id]", Short: "Show a token's active rental", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "RentalOffer", Use: "rental-offer [class-id] [token-id]", Short: "Show a token's open rental offer", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "RentalsByUser", Use: "rentals-by-user [user]", Short: "List the rentals an address holds as renter", PositionalArgs: pos("user")},
				{RpcMethod: "Children", Use: "children [class-id] [token-id]", Short: "List the tokens nested in a token, with its contents hash", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "HarbergerQuote", Use: "harberger-quote [class-id] [token-id]", Short: "Show a Harberger token's current price and tax", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Offer", Use: "offer [offer-id]", Short: "Show an escrowed offer", PositionalArgs: pos("offer_id")},
				{RpcMethod: "Offers", Use: "offers [class-id] [token-id]", Short: "List the open offers on a token (token-id 0 = collection offers)", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "OffersByBidder", Use: "offers-by-bidder [bidder]", Short: "List an address's open offers", PositionalArgs: pos("bidder")},
				{RpcMethod: "Auction", Use: "auction [auction-id]", Short: "Show an auction and the lowest bid it accepts now", PositionalArgs: pos("auction_id")},
				{RpcMethod: "Auctions", Use: "auctions", Short: "List open auctions (--class-id to filter)"},
				{
					RpcMethod:      "AuctionsBySeller",
					Use:            "auctions-by-seller [seller]",
					Short:          "List open auctions started by an address",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{{ProtoField: "seller"}},
				},
				{RpcMethod: "RentalOffers", Use: "rental-offers", Short: "List unexpired rental offers (--user for the ones an address may accept)"},
				{
					RpcMethod: "NextClassId",
					Use:       "next-class-id",
					Short:     "The id the next created class will get",
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              types.Msg_serviceDesc.ServiceName,
			EnhanceCustomCommand: true, // only required if you want to use the custom command
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      true, // skipped because authority gated
				},
				{RpcMethod: "CreateClass", Use: "create-class [name] [accepted-content-license]", Short: "Create a class (flags: --symbol --uri --flags --mint-policy --royalty-bps ...)", Long: commontypes.ContentLicenseNotice, PositionalArgs: pos("name", "accepted_content_license")},
				{RpcMethod: "UpdateClass", Use: "update-class [class-id]", Short: "Edit the class fields named in --update-mask", PositionalArgs: pos("class_id")},
				{RpcMethod: "FreezeClassMetadata", Use: "freeze-class-metadata [class-id]", Short: "Freeze class and token metadata (irreversible)", PositionalArgs: pos("class_id")},
				{RpcMethod: "SetMintPolicy", Use: "set-mint-policy [class-id]", Short: "Replace the public mint policy (--mint-policy JSON)", PositionalArgs: pos("class_id")},
				{RpcMethod: "SetMinters", Use: "set-minters [class-id]", Short: "Add (--add) and remove (--remove) minters", PositionalArgs: pos("class_id")},
				{RpcMethod: "SetMaxSupply", Use: "set-max-supply [class-id] [max-supply]", Short: "Lower (or first set) the supply cap", PositionalArgs: pos("class_id", "max_supply")},
				{RpcMethod: "CloseMinting", Use: "close-minting [class-id]", Short: "Close minting permanently", PositionalArgs: pos("class_id")},
				{RpcMethod: "ProposeClassOwner", Use: "propose-class-owner [class-id] [proposed-owner]", Short: "Propose a class ownership handover", PositionalArgs: pos("class_id", "proposed_owner")},
				{RpcMethod: "AcceptClassOwner", Use: "accept-class-owner [class-id]", Short: "Accept a class ownership handover", PositionalArgs: pos("class_id")},
				{RpcMethod: "CancelClassOwner", Use: "cancel-class-owner [class-id]", Short: "Withdraw a handover proposal", PositionalArgs: pos("class_id")},
				{RpcMethod: "DeleteClass", Use: "delete-class [class-id]", Short: "Delete an empty class", PositionalArgs: pos("class_id")},
				{RpcMethod: "Mint", Use: "mint [class-id] [accepted-content-license]", Short: "Mint tokens (--entries JSON, repeatable)", Long: commontypes.ContentLicenseNotice, PositionalArgs: pos("class_id", "accepted_content_license")},
				{RpcMethod: "PublicMint", Use: "public-mint [class-id] [quantity] [max-price]", Short: "Buy tokens under the public mint policy", PositionalArgs: pos("class_id", "quantity", "max_price")},
				{RpcMethod: "Transfer", Use: "transfer", Short: "Transfer tokens (--entries JSON, repeatable)"},
				{RpcMethod: "AcceptIncoming", Use: "accept-incoming", Short: "Accept inbox items (--entries JSON, repeatable)"},
				{RpcMethod: "RejectIncoming", Use: "reject-incoming", Short: "Reject inbox items (--refs JSON, repeatable)"},
				{RpcMethod: "CancelOutgoing", Use: "cancel-outgoing", Short: "Withdraw pending items you sent (--refs JSON, repeatable)"},
				{RpcMethod: "SetReceivePolicy", Use: "set-receive-policy [policy]", Short: "Set your receive policy (open|members|inbox|unspecified)", PositionalArgs: pos("policy")},
				{RpcMethod: "Burn", Use: "burn", Short: "Burn tokens you own (--refs JSON, repeatable)"},
				{RpcMethod: "Revoke", Use: "revoke [class-id]", Short: "Revoke tokens of a revocable class (--token-ids, --reason)", PositionalArgs: pos("class_id")},
				{RpcMethod: "UpdateToken", Use: "update-token [class-id] [token-id]", Short: "Replace a token's metadata (--metadata JSON)", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "FreezeTokenMetadata", Use: "freeze-token-metadata", Short: "Freeze token metadata as class owner or holder (--refs JSON)"},
				{RpcMethod: "List", Use: "list [class-id] [token-id] [price] [duration-seconds]", Short: "List a token for sale", PositionalArgs: pos("class_id", "token_id", "price", "duration")},
				{RpcMethod: "UpdateListing", Use: "update-listing [class-id] [token-id] [price] [duration-seconds]", Short: "Change a listing's price or duration", PositionalArgs: pos("class_id", "token_id", "price", "duration")},
				{RpcMethod: "Delist", Use: "delist [class-id] [token-id]", Short: "Cancel a listing", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Buy", Use: "buy [class-id] [token-id] [expected-price] [expected-nonce]", Short: "Buy a listed token (--expected-metadata-hash to pin content)", PositionalArgs: pos("class_id", "token_id", "expected_price", "expected_nonce")},
				{RpcMethod: "HideContent", Use: "hide-content [target-kind] [class-id] [token-id] [reason]", Short: "Hide a class or token (sentinel or council)", PositionalArgs: pos("target_kind", "class_id", "token_id", "reason")},
				{RpcMethod: "UnhideContent", Use: "unhide-content [hide-id]", Short: "Reverse a hide before appeal", PositionalArgs: pos("hide_id")},
				{RpcMethod: "AppealHide", Use: "appeal-hide [hide-id]", Short: "Appeal a hide through the x/rep jury (holder or class owner; --reason)", PositionalArgs: pos("hide_id")},
				{RpcMethod: "UpdateOperationalParams", Use: "update-operational-params", Short: "Update operational params (--operational-params JSON, all fields)"},
				{RpcMethod: "Issue", Use: "issue [class-id] [accepted-content-license]", Short: "Issue one metadata to many recipients (--recipients, --metadata JSON)", Long: commontypes.ContentLicenseNotice, PositionalArgs: pos("class_id", "accepted_content_license")},
				{RpcMethod: "Renew", Use: "renew [class-id] [token-id]", Short: "Extend an expiring token by the class term (holder: --max-price)", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Redeem", Use: "redeem [class-id]", Short: "Mark tokens as redeemed as the class owner or a minter (--token-ids)", PositionalArgs: pos("class_id")},
				{RpcMethod: "LockRoyalty", Use: "lock-royalty [class-id]", Short: "Freeze the royalty split forever", PositionalArgs: pos("class_id")},
				{RpcMethod: "Reveal", Use: "reveal [class-id] [token-id] [salt]", Short: "Reveal a blind-minted token (--metadata JSON, --proof hex...)", PositionalArgs: pos("class_id", "token_id", "salt")},
				{RpcMethod: "RevealSeed", Use: "reveal-seed [class-id] [seed]", Short: "Reveal a generative class's seed", PositionalArgs: pos("class_id", "seed")},
				{RpcMethod: "ProposeMedia", Use: "propose-media [class-id] [token-id]", Short: "Offer the holder a media version (--metadata JSON)", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "AcceptMedia", Use: "accept-media [class-id] [token-id] [proposal-id]", Short: "Accept a media proposal (--display to show it)", PositionalArgs: pos("class_id", "token_id", "proposal_id")},
				{RpcMethod: "RejectMedia", Use: "reject-media [class-id] [token-id] [proposal-id]", Short: "Reject or withdraw a media proposal", PositionalArgs: pos("class_id", "token_id", "proposal_id")},
				{RpcMethod: "SetDisplayedMedia", Use: "set-displayed-media [class-id] [token-id] [version]", Short: "Pick the displayed media version (0 = stored metadata)", PositionalArgs: pos("class_id", "token_id", "version")},
				{RpcMethod: "CreateEdition", Use: "create-edition [class-id] [accepted-content-license]", Short: "Add an edition to an editions class (--metadata, --max-supply, --claim JSON for an impact certificate)", Long: commontypes.ContentLicenseNotice, PositionalArgs: pos("class_id", "accepted_content_license")},
				{RpcMethod: "MintUnits", Use: "mint-units [class-id] [edition-id]", Short: "Mint units of an edition (--entries JSON, repeatable)", PositionalArgs: pos("class_id", "edition_id")},
				{RpcMethod: "TransferUnits", Use: "transfer-units", Short: "Transfer edition units (--entries JSON, repeatable)"},
				{RpcMethod: "BurnUnits", Use: "burn-units [class-id] [edition-id] [amount]", Short: "Burn units you hold", PositionalArgs: pos("class_id", "edition_id", "amount")},
				{RpcMethod: "RevokeUnits", Use: "revoke-units [class-id] [edition-id] [holder] [amount]", Short: "Revoke a holder's units as the class owner (--reason)", PositionalArgs: pos("class_id", "edition_id", "holder", "amount")},
				{RpcMethod: "ListUnits", Use: "list-units [class-id] [edition-id] [quantity] [unit-price] [duration-seconds]", Short: "List edition units at a per-unit price", PositionalArgs: pos("class_id", "edition_id", "quantity", "unit_price", "duration")},
				{RpcMethod: "DelistUnits", Use: "delist-units [listing-id]", Short: "Cancel a unit listing", PositionalArgs: pos("listing_id")},
				{RpcMethod: "BuyUnits", Use: "buy-units [listing-id] [quantity] [expected-unit-price]", Short: "Buy some or all of a unit listing", PositionalArgs: pos("listing_id", "quantity", "expected_unit_price")},
				{RpcMethod: "FundEdition", Use: "fund-edition [class-id] [edition-id] [amount]", Short: "Pay SPARK to an edition's holders, pro rata (retro funding)", PositionalArgs: pos("class_id", "edition_id", "amount")},
				{RpcMethod: "ClaimEditionRewards", Use: "claim-edition-rewards", Short: "Claim retro funding accrued to your balances (--refs JSON, repeatable)"},
				{RpcMethod: "AddCraftRecipe", Use: "add-craft-recipe [class-id] [accepted-content-license]", Short: "Add a crafting recipe (--inputs JSON repeatable, --output-metadata or --output-edition-id/--output-quantity)", Long: commontypes.ContentLicenseNotice, PositionalArgs: pos("class_id", "accepted_content_license")},
				{RpcMethod: "RemoveCraftRecipe", Use: "remove-craft-recipe [class-id] [recipe-id]", Short: "Remove a crafting recipe", PositionalArgs: pos("class_id", "recipe_id")},
				{RpcMethod: "Craft", Use: "craft [class-id] [recipe-id]", Short: "Burn a recipe's inputs and mint its output (--input-tokens JSON, repeatable)", PositionalArgs: pos("class_id", "recipe_id")},
				{RpcMethod: "OfferRental", Use: "offer-rental [class-id] [token-id] [price] [duration-seconds] [offer-duration-seconds]", Short: "Offer a token's user right for rent (--user, --owner-may-end)", PositionalArgs: pos("class_id", "token_id", "price", "duration", "offer_duration")},
				{RpcMethod: "CancelRentalOffer", Use: "cancel-rental-offer [class-id] [token-id]", Short: "Withdraw a rental offer", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "AcceptRental", Use: "accept-rental [class-id] [token-id] [expected-price] [expected-nonce]", Short: "Rent a token (--expected-contents-hash for a token holding nested tokens)", PositionalArgs: pos("class_id", "token_id", "expected_price", "expected_nonce")},
				{RpcMethod: "EndRental", Use: "end-rental [class-id] [token-id]", Short: "End a rental (renter any time; owner when allowed or expired)", PositionalArgs: pos("class_id", "token_id")},
				{RpcMethod: "Nest", Use: "nest", Short: "Put a token inside another token you own (--child, --parent JSON)"},
				{RpcMethod: "Detach", Use: "detach", Short: "Take a nested token out of its parent (--child JSON)"},
				{RpcMethod: "SetHarbergerPrice", Use: "set-harberger-price [class-id] [token-id] [price]", Short: "Self-assess a Harberger token's price (--grant-id: your x/session RecurringPull to the artifact module)", PositionalArgs: pos("class_id", "token_id", "price")},
				{RpcMethod: "ForceBuy", Use: "force-buy [class-id] [token-id] [max-price]", Short: "Buy a Harberger token at its current price", PositionalArgs: pos("class_id", "token_id", "max_price")},
				{RpcMethod: "UpdateUnitListing", Use: "update-unit-listing [listing-id] [quantity] [unit-price] [duration-seconds]", Short: "Change a unit listing's quantity, price and duration", PositionalArgs: pos("listing_id", "quantity", "unit_price", "duration")},
				{RpcMethod: "MakeOffer", Use: "make-offer [class-id] [token-id] [price] [duration-seconds]", Short: "Escrow an offer for a token (token-id 0 = any token of the class)", PositionalArgs: pos("class_id", "token_id", "price", "duration")},
				{RpcMethod: "CancelOffer", Use: "cancel-offer [offer-id]", Short: "Withdraw an offer and get its escrow back", PositionalArgs: pos("offer_id")},
				{RpcMethod: "AcceptOffer", Use: "accept-offer [offer-id] [token-id] [expected-price]", Short: "Sell a token to an offer's bidder", PositionalArgs: pos("offer_id", "token_id", "expected_price")},
				{RpcMethod: "StartAuction", Use: "start-auction [class-id] [token-id] [reserve-price] [duration-seconds]", Short: "Put a token up for an English auction (--min-increment-bps)", PositionalArgs: pos("class_id", "token_id", "reserve_price", "duration")},
				{RpcMethod: "Bid", Use: "bid [auction-id] [amount]", Short: "Bid on an auction (--expected-contents-hash for a token holding nested tokens)", PositionalArgs: pos("auction_id", "amount")},
				{RpcMethod: "SettleAuction", Use: "settle-auction [auction-id]", Short: "Settle an ended auction", PositionalArgs: pos("auction_id")},
				{RpcMethod: "CancelAuction", Use: "cancel-auction [auction-id]", Short: "Withdraw an auction nobody has bid on", PositionalArgs: pos("auction_id")},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

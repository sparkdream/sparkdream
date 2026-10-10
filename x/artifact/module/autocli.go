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
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}

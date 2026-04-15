package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "session"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	GovModuleName = "gov"
)

// Store prefixes
var (
	ParamsKey               = collections.NewPrefix("p_session")
	SessionsKey             = collections.NewPrefix(0)
	SessionsByGranterKey    = collections.NewPrefix(1)
	SessionsByGranteeKey    = collections.NewPrefix(2)
	SessionsByExpirationKey = collections.NewPrefix(3)
)

// NonDelegableSessionMsgs are session module messages that can never appear in the
// allowlist. This prevents recursive execution (MsgExecSession containing MsgExecSession)
// and session-key self-management.
var NonDelegableSessionMsgs = map[string]bool{
	"/sparkdream.session.v1.MsgCreateSession":           true,
	"/sparkdream.session.v1.MsgRevokeSession":           true,
	"/sparkdream.session.v1.MsgExecSession":             true,
	"/sparkdream.session.v1.MsgUpdateParams":            true,
	"/sparkdream.session.v1.MsgUpdateOperationalParams": true,
}

// DreamFieldsToStrip maps message type URLs to field names that commit DREAM
// tokens and must be zeroed when dispatched via session key.
var DreamFieldsToStrip = map[string][]string{
	"/sparkdream.blog.v1.MsgCreatePost":  {"author_bond"},
	"/sparkdream.blog.v1.MsgCreateReply": {"author_bond"},
}

// ContextKeySessionFeePaid is set by SessionFeeDecorator when it pays fees
// on behalf of the granter. Reuses the shield module's ContextKeyFeePaid
// so SkipIfFeePaidDecorator can detect it.
const ContextKeySessionFeePaid = "shield_fee_paid"

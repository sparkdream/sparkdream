package keeper

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

func (k msgServer) ExecSession(ctx context.Context, msg *types.MsgExecSession) (*types.MsgExecSessionResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 8. Batch cap
	if len(msg.Msgs) == 0 {
		return nil, types.ErrEmptyMsgs
	}
	if len(msg.Msgs) > 10 {
		return nil, types.ErrTooManyMsgs
	}

	// 1. Look up the SESSION_KEY grant for this (granter, grantee) pair.
	id, err := k.SessionKeyByPair.Get(ctx, collections.Join(msg.Granter, msg.Grantee))
	if err != nil {
		return nil, types.ErrSessionNotFound
	}
	grant, err := k.Grants.Get(ctx, id)
	if err != nil {
		return nil, types.ErrSessionNotFound
	}
	sk := grant.GetSessionKey()
	if sk == nil {
		return nil, types.ErrSessionNotFound
	}
	if grant.Status != types.GrantStatus_GRANT_STATUS_ACTIVE {
		return nil, types.ErrSessionNotFound
	}

	// 2. Not expired
	blockTime := sdkCtx.BlockTime()
	if !grant.ExpiresAt.After(blockTime) {
		return nil, types.ErrSessionExpired
	}

	// 3. Exec count check
	if sk.MaxExecCount > 0 && sk.ExecCount >= sk.MaxExecCount {
		return nil, types.ErrExecCountExceeded
	}

	// Get current params for allowlist validation
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Build lookup sets
	sessionAllowed := make(map[string]bool, len(sk.AllowedMsgTypes))
	for _, t := range sk.AllowedMsgTypes {
		sessionAllowed[t] = true
	}
	globalAllowed := make(map[string]bool, len(params.AllowedMsgTypes))
	for _, t := range params.AllowedMsgTypes {
		globalAllowed[t] = true
	}

	// Validate and dispatch inner messages
	var executedTypeURLs []string

	for _, anyMsg := range msg.Msgs {
		typeURL := anyMsg.TypeUrl

		// 6. Non-recursive
		if types.NonDelegableSessionMsgs[typeURL] {
			return nil, types.ErrNestedExec.Wrapf("type: %s", typeURL)
		}

		// 5. Dual allowlist validation
		if !sessionAllowed[typeURL] {
			return nil, types.ErrMsgTypeNotAllowed.Wrapf("type: %s", typeURL)
		}
		if !globalAllowed[typeURL] {
			return nil, types.ErrMsgTypeNotInAllowlist.Wrapf("type: %s", typeURL)
		}

		// Decode inner message
		var innerMsg sdk.Msg
		cdcAny := &codectypes.Any{
			TypeUrl: anyMsg.TypeUrl,
			Value:   anyMsg.Value,
		}
		if err := k.cdc.UnpackAny(cdcAny, &innerMsg); err != nil {
			return nil, fmt.Errorf("failed to unpack inner message %s: %w", typeURL, err)
		}

		// 7. Single signer check + rewrite signer to granter
		pc, ok := k.cdc.(*codec.ProtoCodec)
		if !ok {
			return nil, fmt.Errorf("codec is not ProtoCodec")
		}
		signerBytes, _, err := pc.GetMsgV1Signers(innerMsg)
		if err != nil {
			return nil, types.ErrMultipleSigners.Wrapf("cannot determine signers: %v", err)
		}
		if len(signerBytes) != 1 {
			return nil, types.ErrMultipleSigners.Wrapf("expected 1 signer, got %d", len(signerBytes))
		}

		// Rewrite signer field to granter address using Go reflection.
		// Try common signer field names in order of prevalence.
		if err := rewriteSignerField(innerMsg, msg.Granter); err != nil {
			return nil, fmt.Errorf("cannot rewrite signer on %s: %w", typeURL, err)
		}

		// Strip DREAM fields (e.g., author_bond on blog messages)
		stripDreamFields(innerMsg, typeURL)

		// Dispatch via router
		if k.late.router == nil {
			return nil, fmt.Errorf("message router not set")
		}
		handler := k.late.router.Handler(innerMsg)
		if handler == nil {
			return nil, fmt.Errorf("no handler found for %s", typeURL)
		}

		result, err := handler(sdkCtx, innerMsg)
		if err != nil {
			return nil, err // atomic: entire MsgExecSession reverts
		}
		_ = result

		executedTypeURLs = append(executedTypeURLs, typeURL)
	}

	// Update grant state: increment by number of inner messages executed.
	// SESSION-S2-1 fix: Spend-limit accounting moved to the ante handler, which
	// debits Spent in fee units (uspark) atomically with the actual fee
	// deduction. This avoids the gas-vs-fee unit mismatch and ensures failed
	// inner messages still consume budget (SESSION-S2-2).
	sk.ExecCount += uint64(len(msg.Msgs))
	sk.LastUsedAt = blockTime
	grant.Payload = &types.Grant_SessionKey{SessionKey: sk}
	if err := k.Grants.Set(ctx, id, grant); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"session_executed",
		sdk.NewAttribute("granter", msg.Granter),
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("msg_type_urls", strings.Join(executedTypeURLs, ",")),
		sdk.NewAttribute("exec_count", fmt.Sprintf("%d", sk.ExecCount)),
		sdk.NewAttribute("grant_id", fmt.Sprintf("%d", id)),
	))

	return &types.MsgExecSessionResponse{}, nil
}

// rewriteSignerField sets the signer field on a message to the granter address.
// Tries common signer field names in order of prevalence across the codebase.
// All allowlisted messages (blog, forum, name, collect) use "Creator" or "Authority".
func rewriteSignerField(msg sdk.Msg, granter string) error {
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("message is not a struct")
	}

	// Try common signer field names in order of prevalence across Cosmos SDK modules.
	// SESSION-5 fix: expanded list to cover all known signer field conventions.
	for _, fieldName := range []string{
		"Creator", "Sender", "Authority", "Proposer", "Validator",
		"Delegator", "Granter", "FromAddress", "Signer", "Admin",
		"Owner", "Operator",
	} {
		field := v.FieldByName(fieldName)
		if field.IsValid() && field.Kind() == reflect.String && field.CanSet() {
			field.SetString(granter)
			return nil
		}
	}

	return fmt.Errorf("no known signer field found on %T", msg)
}

// snakeToPascal converts a snake_case string to PascalCase.
func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

// stripDreamFields zeros out DREAM-commitment fields on allowlisted messages.
// Uses Go reflection to set fields to their zero value.
func stripDreamFields(msg sdk.Msg, typeURL string) {
	fields, ok := types.DreamFieldsToStrip[typeURL]
	if !ok {
		return
	}

	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	for _, fieldName := range fields {
		goName := snakeToPascal(fieldName)
		field := v.FieldByName(goName)
		if field.IsValid() && field.CanSet() {
			field.Set(reflect.Zero(field.Type()))
		}
	}
}

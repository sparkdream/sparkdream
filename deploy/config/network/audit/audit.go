// Package audit provides shared helpers for verifying that each network's
// genesis.json carries a complete set of params for every module the chain
// registers. See the per-network genesis_audit_test.go files for usage.
package audit

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/cosmos/gogoproto/jsonpb"
	"github.com/cosmos/gogoproto/proto"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	gnovmtypes "github.com/sparkdream/gnovm/x/gnovm/types"

	// Blank import for its package init, which sets and seals the SDK's
	// bech32 config. Params validation parses addresses, and without this
	// every sprkdrm1… address in genesis reads as "expected cosmos".
	_ "sparkdream/app"

	blogtypes "sparkdream/x/blog/types"
	collecttypes "sparkdream/x/collect/types"
	commonstypes "sparkdream/x/commons/types"
	ecosystemtypes "sparkdream/x/ecosystem/types"
	federationtypes "sparkdream/x/federation/types"
	forumtypes "sparkdream/x/forum/types"
	futarchytypes "sparkdream/x/futarchy/types"
	nametypes "sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
	revealtypes "sparkdream/x/reveal/types"
	seasontypes "sparkdream/x/season/types"
	sessiontypes "sparkdream/x/session/types"
	shieldtypes "sparkdream/x/shield/types"
	sparkdreamtypes "sparkdream/x/sparkdream/types"
	splittypes "sparkdream/x/split/types"
)

// ProjectModules maps each Spark Dream-owned module's app_state key to a
// zero-value Params struct. The audit reflects over its JSON tags to derive
// the canonical key set.
var ProjectModules = map[string]any{
	"blog":       blogtypes.Params{},
	"collect":    collecttypes.Params{},
	"commons":    commonstypes.Params{},
	"ecosystem":  ecosystemtypes.Params{},
	"federation": federationtypes.Params{},
	"forum":      forumtypes.Params{},
	"futarchy":   futarchytypes.Params{},
	"name":       nametypes.Params{},
	"rep":        reptypes.Params{},
	"reveal":     revealtypes.Params{},
	"season":     seasontypes.Params{},
	"session":    sessiontypes.Params{},
	"shield":     shieldtypes.Params{},
	"sparkdream": sparkdreamtypes.Params{},
	"split":      splittypes.Params{},
}

// SDKModules maps third-party (Cosmos SDK / IBC / gnovm) module app_state
// keys to their Params struct. Audited separately so failures from upstream
// proto changes are visibly distinct from project-owned drift.
var SDKModules = map[string]any{
	"auth":         authtypes.Params{},
	"bank":         banktypes.Params{},
	"distribution": distrtypes.Params{},
	"gov":          govv1types.Params{},
	"mint":         minttypes.Params{},
	"slashing":     slashingtypes.Params{},
	"staking":      stakingtypes.Params{},
	"transfer":     ibctransfertypes.Params{},
	"gnovm":        gnovmtypes.Params{},
}

// ParamsKeys returns the JSON field names defined on the given Params struct.
// Reflection over struct tags (rather than json.Marshal of a value) so fields
// with `omitempty` and zero-value defaults — e.g. `bool` defaulting to false
// — are still counted.
func ParamsKeys(params any) map[string]struct{} {
	t := reflect.TypeOf(params)
	out := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

// RoundTripJSON unmarshals raw proto-JSON into a fresh instance of the same
// type as `template`, using the gogoproto/jsonpb unmarshaler that the chain
// uses for genesis. Catches type mismatches (bool given as string, malformed
// enums, nested struct shape errors) that stdlib encoding/json would either
// reject incorrectly (proto3 ints are JSON strings) or silently accept.
//
// Unknown fields are tolerated; rely on the symmetric ParamsKeys check for
// stricter, friendlier "extra key" diagnostics.
func RoundTripJSON(raw []byte, template any) error {
	_, err := decodeParams(raw, template)
	return err
}

// ValidateParams runs a module's own Params.Validate over the values in
// genesis, for modules that have one.
//
// The key and round-trip checks answer "is every param present, spelled
// right and of the right type" — which a param whose VALUE the module
// rejects passes cleanly. Live (2026-08-25): devnet and testnet shipped
// `jury_size: 3` after a rule landed requiring it to exceed the seated-jury
// floor of 3, so both files were valid-looking JSON that the binary refused
// at the first `genesis gentx`, one error line deep under a screenful of
// cobra help. Nothing in this audit had any reason to look.
func ValidateParams(raw []byte, template any) error {
	msg, err := decodeParams(raw, template)
	if err != nil {
		return err
	}
	v, ok := msg.(interface{ Validate() error })
	if !ok {
		return nil // module defines no validation of its own
	}
	return v.Validate()
}

func decodeParams(raw []byte, template any) (proto.Message, error) {
	ptr := reflect.New(reflect.TypeOf(template)).Interface()
	msg, ok := ptr.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("template %T does not implement proto.Message", template)
	}
	u := &jsonpb.Unmarshaler{AllowUnknownFields: true}
	if err := u.Unmarshal(bytes.NewReader(raw), msg); err != nil {
		return nil, err
	}
	return msg, nil
}

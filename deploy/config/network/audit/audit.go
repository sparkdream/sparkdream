// Package audit provides shared helpers for verifying that each network's
// genesis.json agrees with the binary that will boot on it: every module's
// params are present, correctly typed, individually valid, and — for anything
// the network's config.yml does not deliberately pin — carrying the same value
// the build-tag-active code default produces. See the per-network
// genesis_audit_test.go files for usage, and drift.go for why the last check
// needs config.yml to be meaningful.
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
// constructor for its DefaultParams() under the active build tag.
//
// One registry, not two: the key/type checks need a zero-value struct and the
// value-drift check needs the populated defaults, but a module listed in one
// registry and forgotten in the other would silently lose half its coverage.
// The zero-value template is derived from the constructor instead
// (ZeroTemplate).
var ProjectModules = map[string]func() proto.Message{
	"blog":       func() proto.Message { p := blogtypes.DefaultParams(); return &p },
	"collect":    func() proto.Message { p := collecttypes.DefaultParams(); return &p },
	"commons":    func() proto.Message { p := commonstypes.DefaultParams(); return &p },
	"ecosystem":  func() proto.Message { p := ecosystemtypes.DefaultParams(); return &p },
	"federation": func() proto.Message { p := federationtypes.DefaultParams(); return &p },
	"forum":      func() proto.Message { p := forumtypes.DefaultParams(); return &p },
	"futarchy":   func() proto.Message { p := futarchytypes.DefaultParams(); return &p },
	"name":       func() proto.Message { p := nametypes.DefaultParams(); return &p },
	"rep":        func() proto.Message { p := reptypes.DefaultParams(); return &p },
	"reveal":     func() proto.Message { p := revealtypes.DefaultParams(); return &p },
	"season":     func() proto.Message { p := seasontypes.DefaultParams(); return &p },
	"session":    func() proto.Message { p := sessiontypes.DefaultParams(); return &p },
	"shield":     func() proto.Message { p := shieldtypes.DefaultParams(); return &p },
	"sparkdream": func() proto.Message { p := sparkdreamtypes.DefaultParams(); return &p },
	"split":      func() proto.Message { p := splittypes.DefaultParams(); return &p },
}

// SDKModules maps third-party (Cosmos SDK / IBC / gnovm) module app_state
// keys to their DefaultParams(). Audited separately so failures from upstream
// proto or default changes are visibly distinct from project-owned drift.
var SDKModules = map[string]func() proto.Message{
	"auth":         func() proto.Message { p := authtypes.DefaultParams(); return &p },
	"bank":         func() proto.Message { p := banktypes.DefaultParams(); return &p },
	"distribution": func() proto.Message { p := distrtypes.DefaultParams(); return &p },
	"gov":          func() proto.Message { p := govv1types.DefaultParams(); return &p },
	"mint":         func() proto.Message { p := minttypes.DefaultParams(); return &p },
	"slashing":     func() proto.Message { p := slashingtypes.DefaultParams(); return &p },
	"staking":      func() proto.Message { p := stakingtypes.DefaultParams(); return &p },
	"transfer":     func() proto.Message { p := ibctransfertypes.DefaultParams(); return &p },
	"gnovm":        func() proto.Message { p := gnovmtypes.DefaultParams(); return &p },
}

// ZeroTemplate returns a zero-value struct of the type the constructor
// produces, for the checks that reflect over the type rather than the values.
func ZeroTemplate(ctor func() proto.Message) any {
	return reflect.Zero(reflect.TypeOf(ctor()).Elem()).Interface()
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

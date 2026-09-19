package types_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"sparkdream/x/federation/types"
)

// Field numbers of everything that crosses an IBC wire between sister chains.
//
// These are the ONE exception to the repo-wide "never leave reserved field
// numbers behind, renumber to stay dense" rule (CLAUDE.md, Proto). That rule
// is justified by "this chain has not launched and its networks can be reset,
// so a reserved tag buys wire compatibility nobody is asking for" -- true for
// state, params and tx, which only the chain that wrote them ever reads back.
//
// Federation breaks that premise. Sovereign sister chains upgrade on their own
// schedules by design ("bilateral relationships only, no supergovernment"), so
// a v1.0.38 chain and a v1.0.40 chain exchange these messages routinely. proto3
// does not error on a tag mismatch: renumber a field here and the peer silently
// reads a different field, or drops it. On ReputationResponseData that means
// importing a wrong trust level across a chain boundary.
//
// The messages below are the transitive closure of what FederationPacketData
// can carry plus the acknowledgement payloads module_ibc.go marshals back:
//
//	FederationPacketData -> ReputationQueryPacket, ContentPacket,
//	                        IdentityVerificationPacket,
//	                        IdentityVerificationConfirmPacket
//	acks                 -> ReputationResponseData -> TagReputation
//	                        IdentityVerificationAck
//
// Adding a field is fine and this test stays green -- append-only is exactly
// the intended freedom. Moving or removing one fails here rather than in
// production. If a packet genuinely must change incompatibly, that is what the
// channel version is for: bump `federation-1` to `federation-2` and negotiate
// (x-federation-spec.md 2074-2079), rather than editing a pinned number.
var wireFields = map[string]map[string]int{
	"ReputationQueryPacket": {
		"queried_address": 1,
		"requester":       2,
	},
	"ContentPacket": {
		"content_type":      1,
		"remote_content_id": 2,
		"creator":           3,
		"creator_name":      4,
		"title":             5,
		"body":              6,
		"content_uri":       7,
		"created_at":        8,
		"content_hash":      9,
		"protocol_metadata": 10,
	},
	"IdentityVerificationPacket": {
		"claimed_address":  1,
		"claimant_address": 2,
		"challenge":        3,
	},
	"IdentityVerificationConfirmPacket": {
		"claimed_address":  1,
		"claimant_address": 2,
		"challenge":        3,
		"confirmed":        4,
	},
	"ReputationResponseData": {
		"address":      1,
		"trust_level":  2,
		"is_active":    3,
		"member_since": 4,
		"reputations":  5,
	},
	"IdentityVerificationAck": {
		"exists": 1,
	},
	"TagReputation": {
		"tag":   1,
		"score": 2,
	},
}

// wireMessages maps each pinned name to a live value, so a renamed or deleted
// message fails to compile here instead of silently dropping out of the pin.
var wireMessages = map[string]any{
	"ReputationQueryPacket":             types.ReputationQueryPacket{},
	"ContentPacket":                     types.ContentPacket{},
	"IdentityVerificationPacket":        types.IdentityVerificationPacket{},
	"IdentityVerificationConfirmPacket": types.IdentityVerificationConfirmPacket{},
	"ReputationResponseData":            types.ReputationResponseData{},
	"IdentityVerificationAck":           types.IdentityVerificationAck{},
	"TagReputation":                     types.TagReputation{},
}

// protoNumbers reads the generated `protobuf:"...,N,...,name=X"` struct tags
// back into name -> number. Reading the tags rather than the .proto text keeps
// the check on the bytes that actually get marshalled.
func protoNumbers(t *testing.T, msg any) map[string]int {
	t.Helper()
	out := make(map[string]int)
	rt := reflect.TypeOf(msg)
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("protobuf")
		if tag == "" {
			continue
		}
		parts := strings.Split(tag, ",")
		if len(parts) < 2 {
			continue
		}
		num, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		for _, p := range parts[2:] {
			if name, ok := strings.CutPrefix(p, "name="); ok {
				out[name] = num
				break
			}
		}
	}
	return out
}

func TestIBCPacketFieldNumbersArePinned(t *testing.T) {
	for name, msg := range wireMessages {
		t.Run(name, func(t *testing.T) {
			pinned, ok := wireFields[name]
			if !ok {
				t.Fatalf("%s is in wireMessages but has no pinned field numbers", name)
			}
			live := protoNumbers(t, msg)

			byNumber := make(map[int]string, len(live))
			for field, num := range live {
				byNumber[num] = field
			}

			for field, want := range pinned {
				got, present := live[field]
				if !present {
					t.Errorf("field %q (= %d) is gone from %s. Removing a wire field silently changes how "+
						"every sister chain on an older release parses this message -- reserve the tag instead, "+
						"or bump the channel version to federation-2 and negotiate the change",
						field, want, name)
					continue
				}
				if got != want {
					t.Errorf("%s.%s moved from %d to %d. A peer on an older release still reads tag %d and "+
						"would take %q for it; proto3 reports no error, it just decodes the wrong field",
						name, field, want, got, want, byNumber[want])
				}
			}
		})
	}
}

// TestWirePinCoversEveryPacketVariant fails when a new variant is added to the
// FederationPacketData oneof without being pinned, so the closure above cannot
// silently fall behind the thing it is meant to cover.
func TestWirePinCoversEveryPacketVariant(t *testing.T) {
	variants := map[string]int{
		"reputation_query":      1,
		"content":               2,
		"identity_verification": 3,
		"identity_confirmation": 4,
	}
	for _, wrapper := range []any{
		types.FederationPacketData_ReputationQuery{},
		types.FederationPacketData_Content{},
		types.FederationPacketData_IdentityVerification{},
		types.FederationPacketData_IdentityConfirmation{},
	} {
		for field, num := range protoNumbers(t, wrapper) {
			want, ok := variants[field]
			if !ok {
				t.Errorf("FederationPacketData gained oneof variant %q (= %d) with no entry in wireFields; "+
					"add the message it carries to the pin", field, num)
				continue
			}
			if want != num {
				t.Errorf("FederationPacketData oneof variant %q moved from %d to %d; older peers will "+
					"decode this packet as a different variant entirely", field, want, num)
			}
			delete(variants, field)
		}
	}
	for field, num := range variants {
		t.Errorf("FederationPacketData lost oneof variant %q (= %d); an older peer still sends it", field, num)
	}
}

package simulation

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commonstypes "sparkdream/x/commons/types"
)

// Committee names of the bootstrapped "Three Pillars" structure.
const (
	commonsOpsCommittee   = "Commons Operations Committee"
	commonsGovCommittee   = "Commons Governance Committee"
	technicalOpsCommittee = "Technical Operations Committee"
	ecosystemOpsCommittee = "Ecosystem Operations Committee"
)

// opsCommitteeOf returns the Operations Committee of a council short name
// ("commons", "technical", "ecosystem"), the group whose members
// IsCommitteeMember(council, "operations") accepts.
func opsCommitteeOf(council string) string {
	switch council {
	case "commons":
		return commonsOpsCommittee
	case "technical":
		return technicalOpsCommittee
	case "ecosystem":
		return ecosystemOpsCommittee
	}
	return ""
}

// allGroups returns every x/commons group with a policy address that is not
// a sim account (no sim account is ever a policy; a group whose policy were
// one could be signed for without a vote).
func (e *simEnv) allGroups() []commonstypes.Group {
	if e.groups != nil {
		return e.groups
	}
	out := []commonstypes.Group{}
	var next []byte
	for {
		res, err := e.commonsQuery.ListGroups(e.ctx, &commonstypes.QueryAllGroupRequest{
			Pagination: &query.PageRequest{Key: next, Limit: 200},
		})
		if err != nil {
			break
		}
		for _, g := range res.Group {
			if g.PolicyAddress == "" {
				continue
			}
			if _, isAcc := getAccount(e.accs, g.PolicyAddress); isAcc {
				continue
			}
			out = append(out, g)
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			break
		}
		next = res.Pagination.NextKey
	}
	e.groups = out
	return out
}

// groupMembers returns the member addresses of a group.
func (e *simEnv) groupMembers(name string) []string {
	if m, ok := e.seatCache[name]; ok {
		return m
	}
	res, err := e.commonsQuery.GetCouncilMembers(e.ctx, &commonstypes.QueryGetCouncilMembersRequest{CouncilName: name})
	var out []string
	if err == nil {
		for _, m := range res.Members {
			out = append(out, m.Address)
		}
	}
	if e.seatCache == nil {
		e.seatCache = map[string][]string{}
	}
	e.seatCache[name] = out
	return out
}

// liveSeats returns the live sim accounts sitting on a group.
func (e *simEnv) liveSeats(name string) []simtypes.Account {
	var out []simtypes.Account
	for _, addr := range e.groupMembers(name) {
		if acc, ok := e.account(addr); ok && e.spendable(acc).IsPositive() {
			out = append(out, acc)
		}
	}
	return out
}

// sitsOn reports whether addr sits on the group.
func (e *simEnv) sitsOn(name, addr string) bool {
	for _, m := range e.groupMembers(name) {
		if m == addr {
			return true
		}
	}
	return false
}

// groupByPolicy returns the group whose policy address is policy.
func (e *simEnv) groupByPolicy(policy string) (commonstypes.Group, bool) {
	for _, g := range e.allGroups() {
		if g.PolicyAddress == policy {
			return g, true
		}
	}
	return commonstypes.Group{}, false
}

// groupsAllowing returns the names of the groups whose policy allowlist
// carries typeURL (the messages a proposal of the group may execute).
func (e *simEnv) groupsAllowing(typeURL string) []string {
	var out []string
	for _, g := range e.allGroups() {
		perms, err := e.commonsQuery.GetPolicyPermissions(e.ctx, &commonstypes.QueryGetPolicyPermissionsRequest{PolicyAddress: g.PolicyAddress})
		if err != nil {
			continue
		}
		for _, m := range perms.PolicyPermissions.AllowedMessages {
			if m == typeURL {
				out = append(out, g.Index)
				break
			}
		}
	}
	return out
}

// protoMsg is a gogoproto message that can decode itself.
type protoMsg interface {
	sdk.Msg
	Unmarshal([]byte) error
}

// decoder returns a proposal-message decoder for messages built by fresh.
func decoder[T protoMsg](fresh func() T) func(*codectypes.Any) (sdk.Msg, bool) {
	return func(a *codectypes.Any) (sdk.Msg, bool) {
		m := fresh()
		if a == nil || m.Unmarshal(a.Value) != nil {
			return nil, false
		}
		return m, true
	}
}

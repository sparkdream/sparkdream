package simulation

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// commonsCouncilName is the council that registers groups without x/gov
// (spec §7) and is the parent of every simulated group.
const commonsCouncilName = "Commons Council"

// maxSimGroups bounds the live simulated groups.
const maxSimGroups = 6

// simGroupAllowlist is what a simulated group may be delegated: a subset of
// the Commons Council's own allowlist (RegisterGroup refuses anything else).
var simGroupAllowlist = []string{
	sdk.MsgTypeURL(&types.MsgSpendFromCommons{}),
	sdk.MsgTypeURL(&types.MsgUpdatePolicyPermissions{}),
	sdk.MsgTypeURL(&types.MsgVoteProposal{}),
}

// SimulateMsgRegisterGroup registers a small group under the Commons
// Council through a Commons Council proposal: two to four live sim accounts,
// a short term (so MsgRenewGroup becomes reachable within the run), a
// positive spend cap within the council's own, no x/split share, and a
// subset of the council's allowlist.
func SimulateMsgRegisterGroup(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRegisterGroup{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			council, found, err := e.group(commonsCouncilName)
			if err != nil || !found {
				return proposal{}, "no Commons Council", err
			}
			existing, err := e.groups(func(name string, _ types.Group) bool { return isSimGroup(name) })
			if err != nil {
				return proposal{}, "", err
			}
			if len(existing) >= maxSimGroups {
				return proposal{}, "simulated group cap reached", nil
			}
			policy, proposer, reason, err := e.pickCarrier([]string{council.PolicyAddress}, sdk.MsgTypeURL(label))
			if err != nil || reason != "" {
				return proposal{}, reason, err
			}
			msg, reason := e.registerGroupMsg(policy, council)
			if reason != "" {
				return proposal{}, reason, nil
			}
			return proposal{policy: policy, proposer: proposer, inner: msg}, "", nil
		})
	})
}

// registerGroupMsg builds a valid council-signed MsgRegisterGroup.
func (e *simEnv) registerGroupMsg(policy string, council types.Group) (*types.MsgRegisterGroup, string) {
	live := e.liveAccounts()
	if len(live) < 2 {
		return nil, "fewer than two live sim accounts"
	}
	n := 2 + e.r.Intn(min(3, len(live)-1))
	members := make([]string, 0, n)
	weights := make([]string, 0, n)
	for _, i := range e.r.Perm(len(live))[:n] {
		members = append(members, live[i].Address.String())
		weights = append(weights, "1")
	}

	policyType := keeper.PolicyTypePercentage
	threshold := math.LegacyNewDecWithPrec(int64(50+e.r.Intn(26)), 2) // 0.50-0.75
	if e.r.Intn(2) == 0 {
		policyType = keeper.PolicyTypeThreshold
		threshold = math.LegacyNewDec(int64(1 + e.r.Intn(n)))
	}

	maxSpend := math.NewInt(int64(1_000_000 + e.r.Intn(999_000_000)))
	if council.MaxSpendPerEpoch != nil && council.MaxSpendPerEpoch.IsPositive() && maxSpend.GT(*council.MaxSpendPerEpoch) {
		maxSpend = *council.MaxSpendPerEpoch
	}

	var allowed []string
	for _, m := range simGroupAllowlist {
		if e.r.Intn(5) > 0 {
			allowed = append(allowed, m)
		}
	}

	return &types.MsgRegisterGroup{
		Authority:          policy,
		Name:               fmt.Sprintf("%s%s-%s", simGroupPrefix, randomFictional(e.r), randomLowerAlnum(e.r, 5)),
		Description:        "Simulated working group",
		Members:            members,
		MemberWeights:      weights,
		MaxSpendPerEpoch:   &maxSpend,
		UpdateCooldown:     int64(e.r.Intn(3600)),
		MinMembers:         2,
		MaxMembers:         commontypes.Saturate[uint64](n + 2),
		TermDuration:       day + e.r.Int63n(4*day),
		VotingPeriod:       day + e.r.Int63n(2*day),
		MinExecutionPeriod: 0,
		VoteThreshold:      &threshold,
		PolicyType:         policyType,
		AllowedMessages:    allowed,
		BallotMode:         types.BallotMode_BALLOT_MODE_OPEN,
	}, ""
}

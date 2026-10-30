package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgUpdateGroupConfig reconfigures a simulated group through a
// proposal of its parent (the only non-gov signer the handler accepts):
// cooldown, term, spend cap within the parent's, ballot mode, and sometimes
// a new decision policy. Size bounds, the x/split share and futarchy are
// left alone (x/gov only, or a confidence market the run cannot fund).
// Bootstrapped bodies are never reconfigured: every other module's
// simulation depends on their decision policies.
func SimulateMsgUpdateGroupConfig(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUpdateGroupConfig{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return e.advance(label, func() (proposal, string, error) {
			now := e.ctx.BlockTime().Unix()
			targets, err := e.groups(func(name string, g types.Group) bool {
				// The parent is throttled by the group's update_cooldown.
				return isSimGroup(name) && (g.UpdateCooldown <= 0 || g.LastConfigUpdate <= 0 || now >= g.LastConfigUpdate+g.UpdateCooldown)
			})
			if err != nil {
				return proposal{}, "", err
			}
			if len(targets) == 0 {
				return proposal{}, "no simulated group outside its config cooldown", nil
			}
			target := targets[e.r.Intn(len(targets))]
			policy, proposer, reason, err := e.pickCarrier([]string{target.g.ParentPolicyAddress}, sdk.MsgTypeURL(label))
			if err != nil || reason != "" {
				return proposal{}, reason, err
			}
			msg, err := e.groupConfigMsg(target, policy)
			if err != nil {
				return proposal{}, "", err
			}
			return proposal{policy: policy, proposer: proposer, inner: msg}, "", nil
		})
	})
}

// groupConfigMsg builds a random config change of target signed by policy.
func (e *simEnv) groupConfigMsg(target namedGroup, policy string) (*types.MsgUpdateGroupConfig, error) {
	msg := &types.MsgUpdateGroupConfig{Authority: policy, GroupName: target.name}
	if e.r.Intn(2) == 0 {
		msg.UpdateCooldown = 1 + e.r.Int63n(3600)
	}
	if e.r.Intn(2) == 0 {
		msg.TermDuration = day + e.r.Int63n(4*day)
	}
	if e.r.Intn(2) == 0 {
		capAmt := math.NewInt(1_000_000 + e.r.Int63n(999_000_000))
		if parent, ok, err := e.policyBody(target.g.ParentPolicyAddress); err != nil {
			return nil, err
		} else if ok && parent.g.MaxSpendPerEpoch != nil && parent.g.MaxSpendPerEpoch.IsPositive() && capAmt.GT(*parent.g.MaxSpendPerEpoch) {
			capAmt = *parent.g.MaxSpendPerEpoch
		}
		msg.MaxSpendPerEpoch = &capAmt
	}
	if e.r.Intn(3) == 0 {
		modes := []types.BallotMode{types.BallotMode_BALLOT_MODE_OPEN, types.BallotMode_BALLOT_MODE_PROPOSER_CHOICE}
		msg.BallotMode = modes[e.r.Intn(len(modes))]
	}
	if e.r.Intn(3) == 0 {
		members, _, total, err := e.seats(target.name)
		if err != nil {
			return nil, err
		}
		msg.VotingPeriod = day + e.r.Int63n(2*day)
		if e.r.Intn(2) == 0 || !total.IsPositive() {
			msg.PolicyType = keeper.PolicyTypePercentage
			threshold := math.LegacyNewDecWithPrec(int64(50+e.r.Intn(26)), 2)
			msg.VoteThreshold = &threshold
		} else {
			msg.PolicyType = keeper.PolicyTypeThreshold
			threshold := math.LegacyNewDec(int64(1 + e.r.Intn(max(1, len(members)))))
			if threshold.GT(total) {
				threshold = math.LegacyOneDec()
			}
			msg.VoteThreshold = &threshold
		}
	}
	return msg, nil
}

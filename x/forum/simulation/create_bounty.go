package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgCreateBounty delivers MsgCreateBounty from the author of an
// ACTIVE, unlocked thread with no open bounty, who is an active member and
// can escrow at least the minimum bounty. When no such thread exists, a
// member first starts one (a real MsgCreatePost).
func SimulateMsgCreateBounty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCreateBounty{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCreateBounty{}
		roots, err := e.bountyThreads()
		if err != nil {
			return noOpErr(msg, err)
		}
		if len(roots) == 0 {
			if _, _, err := e.ensureThread(func(a simtypes.Account) bool {
				return e.member(a) && e.spendable(a).GT(math.NewInt(2*types.MinBountyAmount))
			}); err != nil {
				return noOpErr(msg, err)
			}
			if roots, err = e.bountyThreads(); err != nil {
				return noOpErr(msg, err)
			}
		}
		root, ok := pick(e.r, roots)
		if !ok {
			return noOp(msg, "no thread a member author could put a bounty on")
		}
		m, spent, reason := e.newBountyMsg(root)
		if reason != "" {
			return noOp(msg, reason)
		}
		author, _ := e.account(root.Author)
		return e.submit(author, m, spent)
	})
}

// bountyThreads returns the ACTIVE, unlocked threads without an open bounty
// whose author is a live sim account and an active member able to escrow
// the minimum bounty with room for the tx fee.
func (e *simEnv) bountyThreads() ([]types.Post, error) {
	if e.params.ForumPaused || !e.params.BountiesEnabled {
		return nil, nil
	}
	return e.postsWhere(func(p types.Post) bool {
		if !activeRoot(p) || p.Locked || !e.isActiveMember(p.Author) {
			return false
		}
		author, ok := e.account(p.Author)
		if !ok || e.spendable(author).LT(math.NewInt(2*types.MinBountyAmount)) {
			return false
		}
		_, open := e.openBounty(p.PostId)
		return !open
	})
}

// newBountyMsg builds a bounty of one to three times the minimum, sized to
// what the thread author can escrow, with a random duration within the max.
func (e *simEnv) newBountyMsg(root types.Post) (*types.MsgCreateBounty, math.Int, string) {
	msg := &types.MsgCreateBounty{Creator: root.Author, ThreadId: root.PostId}
	author, _ := e.account(root.Author)
	amount := math.NewInt(types.MinBountyAmount).MulRaw(int64(1 + e.r.Intn(3)))
	// Leave room for the tx fee.
	if room := e.spendable(author).QuoRaw(2); amount.GT(room) {
		amount = room
	}
	if amount.LT(math.NewInt(types.MinBountyAmount)) {
		return msg, math.ZeroInt(), "thread author cannot escrow the minimum bounty"
	}
	msg.Amount = amount.String()
	msg.Duration = 86400 + e.r.Int63n(types.DefaultMaxBountyDuration-86400)
	return msg, amount, ""
}

// ensureBounty opens a bounty by delivering MsgCreateBounty on a random
// eligible thread. It returns false when none could be opened.
func (e *simEnv) ensureBounty() (bool, error) {
	roots, err := e.bountyThreads()
	if err != nil {
		return false, err
	}
	root, ok := pick(e.r, roots)
	if !ok {
		return false, nil
	}
	msg, spent, reason := e.newBountyMsg(root)
	if reason != "" {
		return false, nil
	}
	author, _ := e.account(root.Author)
	return e.ensure(author, msg, spent)
}

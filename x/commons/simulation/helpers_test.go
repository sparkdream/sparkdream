package simulation

import (
	"fmt"
	"math/rand"
	"regexp"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/types"
)

// groupNameRegex mirrors the RegisterGroup handler's name rule.
var groupNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 \-]*[a-zA-Z0-9]$`)

func TestSimGroupNamesAreValid(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for range 200 {
		name := fmt.Sprintf("%s%s-%s", simGroupPrefix, randomFictional(r), randomLowerAlnum(r, 5))
		require.True(t, isSimGroup(name))
		require.Regexp(t, groupNameRegex, name)
		require.GreaterOrEqual(t, len(name), 3)
		require.LessOrEqual(t, len(name), 50)
	}
	require.False(t, isSimGroup("Commons Council"))
	require.False(t, isSimGroup("Commons Operations Committee"))
}

func TestSimGroupAllowlistIsHeldByCommonsCouncil(t *testing.T) {
	// The bootstrapped Commons Council allowlist (genesis_bootstrap.go) holds
	// every message a simulated group may be delegated; RegisterGroup refuses
	// a child allowlist that is not a subset of the parent's.
	council := map[string]bool{
		sdk.MsgTypeURL(&types.MsgSpendFromCommons{}):        true,
		sdk.MsgTypeURL(&types.MsgUpdatePolicyPermissions{}): true,
		sdk.MsgTypeURL(&types.MsgVoteProposal{}):            true,
	}
	for _, m := range simGroupAllowlist {
		require.True(t, council[m], m)
	}
}

func TestPickAndAccountsWhere(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	_, ok := pick[int](r, nil)
	require.False(t, ok)
	v, ok := pick(r, []int{4})
	require.True(t, ok)
	require.Equal(t, 4, v)

	accs := simtypes.RandomAccounts(rand.New(rand.NewSource(1)), 5)
	first := accs[0].Address.String()
	kept := accountsWhere(accs, func(a simtypes.Account) bool { return a.Address.String() != first })
	require.Len(t, kept, 4)
	got, found := getAccount(accs, first)
	require.True(t, found)
	require.Equal(t, accs[0].Address, got.Address)
}

func TestFeeExemptMsgsMatchAnte(t *testing.T) {
	// x/commons/ante ProposalFeeDecorator exempts exactly these two.
	require.Len(t, feeExemptMsgs, 2)
	require.True(t, feeExemptMsgs[sdk.MsgTypeURL(&types.MsgVetoGroupProposals{})])
	require.True(t, feeExemptMsgs[sdk.MsgTypeURL(&types.MsgEmergencyCancelGovProposal{})])
}

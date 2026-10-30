package simulation

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// The season simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op first checks the handler's preconditions against live state
// and dry-runs the message through its real handler on a throwaway cache
// (dryRunMsg), returning a NoOp when it cannot hold. No op writes a store:
// prerequisite state is only ever created by delivering earlier real
// messages (ensureProfile, or for quest, achievement and title definitions
// an executed x/commons proposal, see council.go).

// deliver signs msg with acc and delivers it as a real transaction.
func deliver(
	r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig,
	ak types.AuthKeeper, bk types.BankKeeper, acc simtypes.Account, msg sdk.Msg,
) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
		R:               r,
		App:             app,
		TxGen:           txGen,
		Cdc:             nil,
		Msg:             msg,
		CoinsSpentInMsg: sdk.NewCoins(),
		Context:         ctx,
		SimAccount:      acc,
		AccountKeeper:   ak,
		Bankkeeper:      bk,
		ModuleName:      types.ModuleName,
	})
}

// noOp returns a skipped operation for msg's type.
func noOp(msg sdk.Msg, reason string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), reason), nil, nil
}

// noOpErr returns a skipped operation together with a store error, which
// fails the simulation: a failing store read is a real bug.
func noOpErr(msg sdk.Msg, err error) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), err.Error()), nil, err
}

// pick returns a random element of items.
func pick[T any](r *rand.Rand, items []T) (T, bool) {
	var zero T
	if len(items) == 0 {
		return zero, false
	}
	return items[r.Intn(len(items))], true
}

// accountsWhere returns the sim accounts accepted by keep.
func accountsWhere(accs []simtypes.Account, keep func(simtypes.Account) bool) []simtypes.Account {
	var out []simtypes.Account
	for _, acc := range accs {
		if keep(acc) {
			out = append(out, acc)
		}
	}
	return out
}

// getAccountForAddress finds a simulation account for the given address
func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// hasDream reports whether addr is an x/rep member holding at least amount
// DREAM (the handler's lock/burn is still dry-run before delivery).
func hasDream(ctx sdk.Context, k keeper.Keeper, addr string, amount uint64) bool {
	if !k.IsMember(ctx, addr) {
		return false
	}
	bal, err := k.GetDREAMBalance(ctx, addr)
	return err == nil && bal >= amount
}

// guildsWhere returns the guilds accepted by keep.
func guildsWhere(ctx sdk.Context, k keeper.Keeper, keep func(types.Guild) bool) ([]types.Guild, error) {
	var out []types.Guild
	err := k.Guild.Walk(ctx, nil, func(_ uint64, g types.Guild) (bool, error) {
		if keep(g) {
			out = append(out, g)
		}
		return false, nil
	})
	return out, err
}

// guildMembers returns the addresses holding an active membership in the
// guild (the handler's IsGuildMember view).
func guildMembers(ctx sdk.Context, k keeper.Keeper, guildID uint64) ([]string, error) {
	var out []string
	err := k.GuildMembership.Walk(ctx, nil, func(addr string, m types.GuildMembership) (bool, error) {
		if m.GuildId == guildID && m.LeftEpoch == 0 {
			out = append(out, addr)
		}
		return false, nil
	})
	return out, err
}

// getProfile returns addr's member profile.
func getProfile(ctx sdk.Context, k keeper.Keeper, addr string) (types.MemberProfile, bool) {
	p, err := k.MemberProfile.Get(ctx, addr)
	return p, err == nil
}

// displayNameBlocked mirrors the handler's check that a name upheld as
// rejected for the member stays refused.
func displayNameBlocked(ctx sdk.Context, k keeper.Keeper, addr, name string) bool {
	m, err := k.DisplayNameModeration.Get(ctx, addr)
	if err != nil {
		return false
	}
	n := strings.ToLower(strings.TrimSpace(name))
	for _, b := range m.BlockedNames {
		if b == n {
			return true
		}
	}
	return false
}

// ensureProfile makes sure acc has a member profile, creating one the way
// the chain does: by delivering MsgSetDisplayName (which creates a missing
// profile). It returns false when no profile exists and none could be
// created without a handler rejection.
func ensureProfile(
	r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig,
	ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, acc simtypes.Account,
) (bool, error) {
	addr := acc.Address.String()
	if _, ok := getProfile(ctx, k, addr); ok {
		return true, nil
	}
	name := randomDisplayName(r)
	if k.ValidateDisplayName(ctx, name) != nil || displayNameBlocked(ctx, k, addr, name) {
		return false, nil
	}
	if _, _, err := deliver(r, app, ctx, txGen, ak, bk, acc, &types.MsgSetDisplayName{Creator: addr, Name: name}); err != nil {
		return false, err
	}
	_, ok := getProfile(ctx, k, addr)
	return ok, nil
}

// guildsJoinedThisSeason mirrors the keeper's per-season join count: a
// record from an earlier season reads as zero.
func guildsJoinedThisSeason(ctx sdk.Context, k keeper.Keeper, m types.GuildMembership) uint64 {
	season, err := k.Season.Get(ctx)
	if err != nil || m.GuildsJoinedSeason != season.Number {
		return 0
	}
	return m.GuildsJoinedThisSeason
}

// joinBlocker returns why addr cannot enter a guild (create, join or accept
// an invite), or "" when it can: it needs a profile outside any guild, no
// active membership record, an elapsed guild-hop cooldown and a free slot in
// the per-season guild cap.
func joinBlocker(ctx sdk.Context, k keeper.Keeper, params types.Params, addr string) string {
	profile, ok := getProfile(ctx, k, addr)
	if !ok {
		return "no member profile"
	}
	if profile.GuildId != 0 {
		return "already in a guild"
	}
	membership, err := k.GuildMembership.Get(ctx, addr)
	if err != nil {
		return ""
	}
	if membership.GuildId != 0 && membership.LeftEpoch == 0 {
		return "active guild membership record"
	}
	// Any past membership starts the hop cooldown, one left in epoch 0 too.
	since := k.GetCurrentEpoch(ctx) - membership.LeftEpoch
	if commontypes.Saturate[uint64](since) < params.GuildHopCooldownEpochs {
		return "guild hop cooldown"
	}
	if guildsJoinedThisSeason(ctx, k, membership) >= uint64(params.MaxGuildsPerSeason) {
		return "max guilds per season reached"
	}
	return ""
}

// guildHasRoom reports whether the guild is below max_guild_members.
func guildHasRoom(ctx sdk.Context, k keeper.Keeper, params types.Params, guildID uint64) bool {
	return k.GetGuildMemberCount(ctx, guildID) < uint64(params.MaxGuildMembers)
}

// inviteLive reports whether invitee holds an unexpired invite to the guild.
func inviteLive(ctx sdk.Context, k keeper.Keeper, guildID uint64, invitee string) bool {
	inv, err := k.GuildInvite.Get(ctx, fmt.Sprintf("%d:%s", guildID, invitee))
	if err != nil {
		return false
	}
	return inv.ExpiresEpoch == 0 || k.GetCurrentEpoch(ctx) <= inv.ExpiresEpoch
}

// isOfficer reports whether addr is one of the guild's officers.
func isOfficer(g types.Guild, addr string) bool {
	for _, o := range g.Officers {
		if o == addr {
			return true
		}
	}
	return false
}

// randomGuildName generates a random guild name. Guild names are also
// reserved in x/name, so they use its character set (a-z, 0-9, -, _).
func randomGuildName(r *rand.Rand) string {
	names := []string{"warriors", "explorers", "guardians", "pioneers", "champions", "legends", "defenders", "builders"}
	return fmt.Sprintf("%s-%d", names[r.Intn(len(names))], r.Intn(100000))
}

// randomDisplayName generates a random display name
func randomDisplayName(r *rand.Rand) string {
	prefixes := []string{"Cool", "Epic", "Super", "Mega", "Ultra", "Pro", "Elite", "Master"}
	suffixes := []string{"Player", "Gamer", "User", "Member", "Hero", "Star", "Champion", "Legend"}
	return fmt.Sprintf("%s%s%d", prefixes[r.Intn(len(prefixes))], suffixes[r.Intn(len(suffixes))], r.Intn(1000))
}

// randomUsername generates a random username
func randomUsername(r *rand.Rand) string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	username := make([]byte, 8)
	for i := range username {
		username[i] = chars[r.Intn(len(chars))]
	}
	return string(username)
}

// randomQuestId generates a random quest ID
func randomQuestId(r *rand.Rand) string {
	return fmt.Sprintf("quest_%d", r.Intn(10000))
}

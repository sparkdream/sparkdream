package rep

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/module"

	commontypes "sparkdream/x/common/types"
	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// simMaxMembers caps the simulated genesis membership. Several operations
// read every member once per operation, so a few hundred members would slow
// the simulation without exercising anything new.
const simMaxMembers = 150

// simDreamUnit is one DREAM in its base unit.
const simDreamUnit = int64(1_000_000)

// simGenesisMembers returns the simulated genesis membership.
//
// DREAM is an x/rep ledger (Member.dream_balance), not an x/bank coin, so
// seeded balances need no matching bank supply.
//
// Every sim account holding an x/commons seat (founding members and extra
// committee seats from the commons simulated genesis, generated before this
// module's) is a member at TRUSTED or CORE. About three in five of the other
// accounts are members too, spread across the trust ladder with at least
// three ESTABLISHED and one TRUSTED; the rest stay non-members for the
// invitation paths. Every member holds enough DREAM for the bonded roles at
// ESTABLISHED and above, and reputation and completed-interim counts that
// meet its trust level's thresholds without reaching the next level's, so
// UpdateTrustLevel never moves a seeded member.
func simGenesisMembers(simState *module.SimulationState, params types.Params, tags []string) []types.Member {
	n := len(simState.Accounts)
	if n == 0 {
		return nil
	}
	r := simGenesisRand(simState, types.ModuleName)
	seated := simSeatedAccounts(simState)

	var members []types.Member
	isMember := make(map[string]bool, n)
	add := func(addr string, level types.TrustLevel) {
		if isMember[addr] || len(members) >= simMaxMembers {
			return
		}
		isMember[addr] = true
		members = append(members, simMember(r, simState, params, tags, addr, level))
	}

	// Seated accounts first, in account order (deterministic): the founder
	// at CORE, every other seat-holder at CORE or TRUSTED.
	for _, acc := range simState.Accounts {
		addr := acc.Address.String()
		founder, ok := seated[addr]
		if !ok {
			continue
		}
		level := types.TrustLevel_TRUST_LEVEL_TRUSTED
		if founder || r.Intn(2) == 0 {
			level = types.TrustLevel_TRUST_LEVEL_CORE
		}
		add(addr, level)
	}

	var unseated []string
	for _, acc := range simState.Accounts {
		if addr := acc.Address.String(); !isMember[addr] {
			unseated = append(unseated, addr)
		}
	}
	r.Shuffle(len(unseated), func(i, j int) { unseated[i], unseated[j] = unseated[j], unseated[i] })
	want := len(unseated) * 3 / 5
	for i, addr := range unseated[:want] {
		add(addr, simTrustLevel(r, i))
	}
	return members
}

// simSeatedAccounts returns the addresses holding a seat in the simulated
// x/commons genesis, mapped to whether the address is the founder.
func simSeatedAccounts(simState *module.SimulationState) map[string]bool {
	seated := map[string]bool{}
	bz, ok := simState.GenState[commonstypes.ModuleName]
	if !ok {
		return seated
	}
	var gs commonstypes.GenesisState
	if err := simState.Cdc.UnmarshalJSON(bz, &gs); err != nil {
		panic(err)
	}
	for _, m := range gs.FoundingMembers {
		seated[m.Address] = m.Founder
	}
	for _, cm := range gs.CouncilMembers {
		for _, m := range cm.Members {
			if _, ok := seated[m.Address]; !ok {
				seated[m.Address] = false
			}
		}
	}
	return seated
}

// simTrustLevel returns the trust level of the i-th unseated member: the
// first four guarantee several ESTABLISHED members and a TRUSTED one, the
// rest are drawn across the ladder.
func simTrustLevel(r *rand.Rand, i int) types.TrustLevel {
	switch {
	case i < 3:
		return types.TrustLevel_TRUST_LEVEL_ESTABLISHED
	case i == 3:
		return types.TrustLevel_TRUST_LEVEL_TRUSTED
	}
	switch x := r.Intn(100); {
	case x < 20:
		return types.TrustLevel_TRUST_LEVEL_NEW
	case x < 45:
		return types.TrustLevel_TRUST_LEVEL_PROVISIONAL
	case x < 80:
		return types.TrustLevel_TRUST_LEVEL_ESTABLISHED
	case x < 93:
		return types.TrustLevel_TRUST_LEVEL_TRUSTED
	default:
		return types.TrustLevel_TRUST_LEVEL_CORE
	}
}

// simMember builds a genesis member at level.
func simMember(r *rand.Rand, simState *module.SimulationState, params types.Params, tags []string, addr string, level types.TrustLevel) types.Member {
	cfg := params.TrustLevelConfig

	// DREAM (whole units) drawn from [lo, hi).
	var lo, hi int64
	// Reputation total drawn from [repLo, repHi) and completed interims.
	var repLo, repHi math.LegacyDec
	var interims uint32
	switch level {
	case types.TrustLevel_TRUST_LEVEL_NEW:
		lo, hi = 50, 500
		repLo, repHi = math.LegacyZeroDec(), cfg.ProvisionalMinRep
	case types.TrustLevel_TRUST_LEVEL_PROVISIONAL:
		lo, hi = 300, 1500
		repLo, repHi = cfg.ProvisionalMinRep, cfg.EstablishedMinRep
		interims = cfg.ProvisionalMinInterims
	case types.TrustLevel_TRUST_LEVEL_ESTABLISHED:
		lo, hi = 2000, 5000
		repLo, repHi = cfg.EstablishedMinRep, cfg.TrustedMinRep
		interims = cfg.EstablishedMinInterims
	case types.TrustLevel_TRUST_LEVEL_TRUSTED:
		lo, hi = 3000, 6000
		repLo, repHi = cfg.TrustedMinRep, cfg.CoreMinRep
		interims = cfg.EstablishedMinInterims + 2
	default:
		lo, hi = 4000, 8000
		repLo, repHi = cfg.CoreMinRep, cfg.CoreMinRep.MulInt64(2)
		interims = cfg.EstablishedMinInterims + 4
	}
	dream := math.NewInt((lo + r.Int63n(hi-lo)) * simDreamUnit)

	// Spread the reputation total over one to three seeded tags. The total
	// is drawn strictly inside [repLo, repHi) in whole points, so a member
	// never qualifies for the next level.
	total := simRepTotal(r, repLo, repHi)
	scores := map[string]string{}
	lifetime := map[string]string{}
	if len(tags) > 0 && total.IsPositive() {
		k := 1 + r.Intn(min(3, len(tags)))
		picked := r.Perm(len(tags))[:k]
		share := total.QuoInt64(int64(k))
		rest := total
		for i, ti := range picked {
			v := share
			if i == k-1 {
				v = rest
			}
			rest = rest.Sub(v)
			scores[tags[ti]] = v.String()
			lifetime[tags[ti]] = v.String()
		}
	}

	joinedAt := simState.GenTimestamp.Unix()
	return types.Member{
		Address:                         addr,
		DreamBalance:                    intPtr(dream),
		StakedDream:                     intPtr(math.ZeroInt()),
		LifetimeEarned:                  intPtr(dream),
		LifetimeBurned:                  intPtr(math.ZeroInt()),
		ReputationScores:                scores,
		LifetimeReputation:              lifetime,
		TrustLevel:                      level,
		TrustLevelUpdatedAt:             joinedAt,
		JoinedSeason:                    0,
		JoinedAt:                        joinedAt,
		InvitationCredits:               keeper.GetMaxInvitationCredits(cfg, level),
		Status:                          types.MemberStatus_MEMBER_STATUS_ACTIVE,
		TipsSentThisEpoch:               intPtr(math.ZeroInt()),
		CompletedInterimsCount:          interims,
		TransferReceivedThisEpoch:       intPtr(math.ZeroInt()),
		ContentStakedDream:              intPtr(math.ZeroInt()),
		TransferReceivedThisSeason:      intPtr(math.ZeroInt()),
		InitiativeBountyFundedThisEpoch: intPtr(math.ZeroInt()),
		ContentLicense:                  commontypes.ChainContentLicense,
	}
}

// simRepTotal draws a whole-point reputation total in [lo, hi).
func simRepTotal(r *rand.Rand, lo, hi math.LegacyDec) math.LegacyDec {
	base := lo.Ceil().TruncateInt64()
	span := hi.TruncateInt64() - base
	if hi.IsInteger() {
		span-- // stay strictly below an integral upper threshold
	}
	if span <= 0 {
		return math.LegacyNewDec(base)
	}
	return math.LegacyNewDec(base + r.Int63n(span+1))
}

func intPtr(v math.Int) *math.Int { return &v }

// simGenesisRand returns a source for the simulated genesis seeded from the
// sim accounts (themselves drawn from the simulation seed) and tag. It never
// draws from simState.Rand, so the genesis of the modules generated after
// this one, and the whole run that follows, is not shifted by it.
func simGenesisRand(simState *module.SimulationState, tag string) *rand.Rand {
	h := sha256.New()
	h.Write([]byte(tag))
	for _, acc := range simState.Accounts {
		h.Write(acc.Address)
	}
	return rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(h.Sum(nil)[:8]) >> 1))) //nolint:gosec // simulation only
}

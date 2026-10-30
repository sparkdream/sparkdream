package simulation

import (
	"sort"

	"cosmossdk.io/math"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Prerequisites. Each builder returns an existing record in the wanted
// state when there is one, and otherwise delivers the real messages that
// produce it (project -> initiative -> assignment -> submission). ok is
// false when no valid path exists right now.

// maxPrereqTries bounds the dry-runs a prerequisite builder spends.
const maxPrereqTries = 12

// tagsFor returns 1-3 registered tags, preferring those the member holds
// reputation in (so the initiatives created under them can be taken).
func (e *simEnv) tagsFor(m types.Member) ([]string, error) {
	registered, err := e.tags()
	if err != nil {
		return nil, err
	}
	reg := make(map[string]bool, len(registered))
	for _, t := range registered {
		reg[t] = true
	}
	var mine []string
	for _, t := range repTags(m) {
		if reg[t] {
			mine = append(mine, t)
		}
	}
	pool := mine
	if len(pool) == 0 {
		pool = registered
	}
	if len(pool) == 0 {
		return nil, nil
	}
	n := 1 + e.r.Intn(min(3, len(pool)))
	out := make([]string, 0, n)
	for _, i := range e.r.Perm(len(pool))[:n] {
		out = append(out, pool[i])
	}
	sort.Strings(out)
	return out, nil
}

// projectMsg builds a MsgProposeProject for ma: permissionless (no budget,
// the creation fee burned) when the member's trust level allows it and
// permissionless is asked for, otherwise budget-backed below the
// large-project threshold so one Operations Committee member may approve it.
func (e *simEnv) projectMsg(ma memberAcc, permissionless bool) (*types.MsgProposeProject, error) {
	tags, err := e.tagsFor(ma.m)
	if err != nil {
		return nil, err
	}
	budget := math.ZeroInt()
	if !permissionless {
		hi := e.params.LargeProjectBudgetThreshold
		if hi.IsNil() || !hi.IsPositive() {
			hi = dream(10_000)
		}
		budget = randBetween(e.r, dream(1_000), math.MinInt(hi, dream(8_000)))
	}
	return &types.MsgProposeProject{
		Creator:         ma.addr(),
		Name:            randomName(e.r, "Project"),
		Description:     "Simulation generated project",
		Tags:            tags,
		Category:        randomProjectCategory(e.r),
		Council:         randomCouncil(e.r),
		RequestedBudget: PtrInt(budget),
		RequestedSpark:  PtrInt(math.ZeroInt()),
	}, nil
}

// canPermissionless reports whether ma may open a permissionless project.
func (e *simEnv) canPermissionless(ma memberAcc) bool {
	return commontypes.Saturate[uint32](ma.m.TrustLevel) >= e.params.PermissionlessMinTrustLevel &&
		unlocked(ma.m).GT(e.params.ProjectCreationFee.MulRaw(2))
}

// newestProjectBy returns the id of creator's newest project.
func (e *simEnv) newestProjectBy(creator string) (types.Project, bool, error) {
	ps, err := e.projects(func(p types.Project) bool { return p.Creator == creator })
	if err != nil || len(ps) == 0 {
		return types.Project{}, false, err
	}
	best := ps[0]
	for _, p := range ps[1:] {
		if p.Id > best.Id {
			best = p
		}
	}
	return best, true, nil
}

// proposeProject delivers a MsgProposeProject from a random member and
// returns the new project. permissionless asks for a permissionless one
// when the member qualifies.
func (e *simEnv) proposeProject(permissionless bool) (types.Project, bool, error) {
	ms, err := e.members(nil)
	if err != nil {
		return types.Project{}, false, err
	}
	tries := 0
	for _, i := range e.r.Perm(len(ms)) {
		if tries >= maxPrereqTries {
			break
		}
		ma := ms[i]
		perm := permissionless && e.canPermissionless(ma)
		if permissionless && !perm {
			continue
		}
		tries++
		msg, err := e.projectMsg(ma, perm)
		if err != nil {
			return types.Project{}, false, err
		}
		ok, err := e.ensure(ma.acc, msg, math.ZeroInt())
		if err != nil {
			return types.Project{}, false, err
		}
		if ok {
			return e.newestProjectBy(ma.addr())
		}
	}
	return types.Project{}, false, nil
}

// approveMsgs returns the direct MsgApproveProjectBudget candidates for a
// PROPOSED project: one per live seat of its council's Operations
// Committee, approving the requested budget when it sits below the
// large-project threshold.
func (e *simEnv) approveCandidates(p types.Project, requested math.Int) []candidate {
	committee := opsCommitteeOf(p.Council)
	if committee == "" {
		return nil
	}
	budget := requested
	if budget.IsNil() || !budget.IsPositive() {
		budget = randBetween(e.r, dream(500), dream(5_000))
	}
	if budget.GT(e.params.LargeProjectBudgetThreshold) {
		budget = e.params.LargeProjectBudgetThreshold
	}
	var out []candidate
	for _, acc := range e.liveSeats(committee) {
		out = append(out, candidate{acc: acc, msg: &types.MsgApproveProjectBudget{
			Approver:       acc.Address.String(),
			ProjectId:      p.Id,
			ApprovedBudget: PtrInt(budget),
			ApprovedSpark:  PtrInt(math.ZeroInt()),
		}, spent: math.ZeroInt()})
	}
	return out
}

// activeProjects returns the ACTIVE projects.
func (e *simEnv) activeProjects() ([]types.Project, error) {
	return e.projects(func(p types.Project) bool { return p.Status == types.ProjectStatus_PROJECT_STATUS_ACTIVE })
}

// initiativeSigners returns who may create an initiative under p: its
// creator (when a live member) and, for a budget-backed project, the
// members of the Operations Committees IsOperationsCommittee accepts.
func (e *simEnv) initiativeSigners(p types.Project) []memberAcc {
	var out []memberAcc
	if acc, ok := e.account(p.Creator); ok {
		if m, ok := e.member(p.Creator); ok && m.Status == types.MemberStatus_MEMBER_STATUS_ACTIVE {
			out = append(out, memberAcc{acc: acc, m: m})
		}
	}
	if p.Permissionless {
		ms, err := e.members(nil)
		if err == nil {
			for _, ma := range ms {
				if ma.addr() != p.Creator {
					out = append(out, ma)
				}
			}
		}
		return out
	}
	for _, c := range []string{commonsOpsCommittee, technicalOpsCommittee} {
		for _, acc := range e.liveSeats(c) {
			if m, ok := e.member(acc.Address.String()); ok && acc.Address.String() != p.Creator {
				out = append(out, memberAcc{acc: acc, m: m})
			}
		}
	}
	return out
}

// initiativeMsg builds a MsgCreateInitiative by signer under p.
func (e *simEnv) initiativeMsg(signer memberAcc, p types.Project) *types.MsgCreateInitiative {
	tier := types.InitiativeTier_INITIATIVE_TIER_APPRENTICE
	if e.r.Intn(3) == 0 {
		tier = types.InitiativeTier_INITIATIVE_TIER_STANDARD
	}
	if !p.Permissionless && e.r.Intn(6) == 0 {
		tier = randomInitiativeTier(e.r)
	}
	maxBudget := e.tierConfig(tier).MaxBudget
	if maxBudget.IsNil() || !maxBudget.IsPositive() {
		maxBudget = dream(100)
	}
	hi := maxBudget
	if !p.Permissionless {
		left := keeper.DerefInt(p.ApprovedBudget).Sub(keeper.DerefInt(p.AllocatedBudget))
		hi = math.MinInt(hi, left)
	}
	lo := math.MinInt(dream(10), hi)
	budget := randBetween(e.r, lo, hi)
	tags := p.Tags
	if len(tags) == 0 {
		tags, _ = e.tagsFor(signer.m)
	}
	return &types.MsgCreateInitiative{
		Creator:     signer.addr(),
		ProjectId:   p.Id,
		Title:       randomName(e.r, "Initiative"),
		Description: "Simulation generated initiative",
		Tags:        tags,
		Tier:        commontypes.Saturate[uint64](tier),
		Category:    commontypes.Saturate[uint64](randomInitiativeCategory(e.r)),
		Budget:      PtrInt(budget),
	}
}

// createInitiativeCandidates returns MsgCreateInitiative candidates over the
// ACTIVE projects.
func (e *simEnv) createInitiativeCandidates() ([]candidate, error) {
	ps, err := e.activeProjects()
	if err != nil {
		return nil, err
	}
	var out []candidate
	for _, p := range ps {
		signers := e.initiativeSigners(p)
		if len(signers) == 0 {
			continue
		}
		s := signers[e.r.Intn(len(signers))]
		out = append(out, candidate{acc: s.acc, msg: e.initiativeMsg(s, p), spent: math.ZeroInt()})
	}
	return out, nil
}

// ensureActiveProject delivers what it takes to have an ACTIVE project: a
// permissionless proposal, or a budget-backed one approved at once by a
// seat of its council's Operations Committee.
func (e *simEnv) ensureActiveProject() (bool, error) {
	if e.r.Intn(2) == 0 {
		if _, ok, err := e.proposeProject(true); err != nil || ok {
			return ok, err
		}
	}
	p, ok, err := e.proposeProject(false)
	if err != nil || !ok {
		return false, err
	}
	for _, c := range e.approveCandidates(p, keeper.DerefInt(p.ApprovedBudget)) {
		if ok, err := e.ensure(c.acc, c.msg, math.ZeroInt()); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// openInitiative returns an OPEN initiative, creating one (and its project
// when needed) by delivering the real messages.
func (e *simEnv) openInitiative(keep func(types.Initiative) bool) (types.Initiative, bool, error) {
	open := func() ([]types.Initiative, error) {
		return e.initiatives(func(in types.Initiative) bool {
			return in.Status == types.InitiativeStatus_INITIATIVE_STATUS_OPEN && (keep == nil || keep(in))
		})
	}
	ins, err := open()
	if err != nil {
		return types.Initiative{}, false, err
	}
	if in, ok := pick(e.r, ins); ok {
		return in, true, nil
	}
	ok, err := e.createInitiative()
	if err != nil {
		return types.Initiative{}, false, err
	}
	if !ok {
		if ok, err = e.ensureActiveProject(); err != nil || !ok {
			return types.Initiative{}, false, err
		}
		if ok, err = e.createInitiative(); err != nil || !ok {
			return types.Initiative{}, false, err
		}
	}
	ins, err = open()
	if err != nil {
		return types.Initiative{}, false, err
	}
	in, ok := pick(e.r, ins)
	return in, ok, nil
}

// createInitiative delivers one MsgCreateInitiative under an ACTIVE project.
func (e *simEnv) createInitiative() (bool, error) {
	cands, err := e.createInitiativeCandidates()
	if err != nil {
		return false, err
	}
	for n, i := range e.r.Perm(len(cands)) {
		if n >= maxPrereqTries {
			break
		}
		if ok, err := e.ensure(cands[i].acc, cands[i].msg, math.ZeroInt()); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// assignCandidates returns self-assignment candidates for an OPEN
// initiative: live members with the tier's reputation in its tags.
func (e *simEnv) assignCandidates(in types.Initiative) ([]candidate, error) {
	minRep := e.tierConfig(in.Tier).MinReputation
	ms, err := e.members(func(ma memberAcc) bool {
		return minRep.IsNil() || avgReputation(ma.m, in.Tags).GTE(minRep)
	})
	if err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(ms))
	for _, ma := range ms {
		out = append(out, candidate{acc: ma.acc, msg: &types.MsgAssignInitiative{
			Creator:      ma.addr(),
			InitiativeId: in.Id,
			Assignee:     ma.addr(),
		}, spent: math.ZeroInt()})
	}
	return out, nil
}

// assignedInitiative returns an ASSIGNED initiative whose assignee is a
// live sim account accepted by keep, assigning an OPEN one when none is.
func (e *simEnv) assignedInitiative(keep func(types.Initiative) bool) (types.Initiative, simtypes.Account, bool, error) {
	assigned := func() ([]types.Initiative, error) {
		return e.initiatives(func(in types.Initiative) bool {
			if in.Status != types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED {
				return false
			}
			_, ok := e.account(in.Assignee)
			return ok && (keep == nil || keep(in))
		})
	}
	ins, err := assigned()
	if err != nil {
		return types.Initiative{}, simtypes.Account{}, false, err
	}
	if in, ok := pick(e.r, ins); ok {
		acc, _ := e.account(in.Assignee)
		return in, acc, true, nil
	}
	in, ok, err := e.openInitiative(nil)
	if err != nil || !ok {
		return types.Initiative{}, simtypes.Account{}, false, err
	}
	cands, err := e.assignCandidates(in)
	if err != nil {
		return types.Initiative{}, simtypes.Account{}, false, err
	}
	for n, i := range e.r.Perm(len(cands)) {
		if n >= maxPrereqTries {
			break
		}
		ok, err := e.ensure(cands[i].acc, cands[i].msg, math.ZeroInt())
		if err != nil {
			return types.Initiative{}, simtypes.Account{}, false, err
		}
		if ok {
			fresh, found := e.initiative(in.Id)
			if !found || (keep != nil && !keep(fresh)) {
				return types.Initiative{}, simtypes.Account{}, false, nil
			}
			return fresh, cands[i].acc, true, nil
		}
	}
	return types.Initiative{}, simtypes.Account{}, false, nil
}

// submitMsg builds the assignee's MsgSubmitInitiativeWork.
func submitMsg(acc simtypes.Account, id uint64) *types.MsgSubmitInitiativeWork {
	return &types.MsgSubmitInitiativeWork{
		Creator:        acc.Address.String(),
		InitiativeId:   id,
		DeliverableUri: "ipfs://sim-deliverable",
		Comments:       "simulation submission",
	}
}

// submittedInitiative returns a SUBMITTED or IN_REVIEW initiative accepted
// by keep, submitting the work of an ASSIGNED one when none is.
func (e *simEnv) submittedInitiative(keep func(types.Initiative) bool) (types.Initiative, bool, error) {
	ins, err := e.initiatives(func(in types.Initiative) bool {
		return (in.Status == types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED ||
			in.Status == types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW) && (keep == nil || keep(in))
	})
	if err != nil {
		return types.Initiative{}, false, err
	}
	if in, ok := pick(e.r, ins); ok {
		return in, true, nil
	}
	in, acc, ok, err := e.assignedInitiative(nil)
	if err != nil || !ok {
		return types.Initiative{}, false, err
	}
	ok, err = e.ensure(acc, submitMsg(acc, in.Id), math.ZeroInt())
	if err != nil || !ok {
		return types.Initiative{}, false, err
	}
	fresh, found := e.initiative(in.Id)
	if !found || (keep != nil && !keep(fresh)) {
		return types.Initiative{}, false, nil
	}
	return fresh, true, nil
}

// stakeAmount picks a stake for ma: min_stake_amount up to a tenth of its
// unlocked DREAM (never more than half of it).
func (e *simEnv) stakeAmount(ma memberAcc) (math.Int, bool) {
	floor := e.params.MinStakeAmount
	if floor.IsNil() || !floor.IsPositive() {
		floor = math.OneInt()
	}
	free := unlocked(ma.m).QuoRaw(2)
	if free.LT(floor) {
		return math.ZeroInt(), false
	}
	return randBetween(e.r, floor, math.MaxInt(floor, unlocked(ma.m).QuoRaw(10))), true
}

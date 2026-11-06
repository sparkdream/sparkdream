package types

// IsMaintenance reports whether a transition phase runs in maintenance
// mode: the reputation and DREAM the snapshot records must stop moving from
// SNAPSHOT through RESET_XP (SEA-16). SeasonTransitionState.maintenance_mode
// is always this value for the state's phase: the keeper derives it when a
// phase begins and genesis Validate refuses an imported state that
// disagrees (SEA-203).
func (p TransitionPhase) IsMaintenance() bool {
	switch p {
	case TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
		TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION,
		TransitionPhase_TRANSITION_PHASE_RESET_REPUTATION,
		TransitionPhase_TRANSITION_PHASE_RESET_XP:
		return true
	default:
		return false
	}
}

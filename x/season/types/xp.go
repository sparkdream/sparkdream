package types

// XPSource identifies where an XP grant came from. It selects the
// EpochXpTracker bucket the grant counts against and is emitted on the
// xp_granted event.
type XPSource int

const (
	// Governance participation
	XPSourceVoteCast XPSource = iota
	XPSourceProposalCreated

	// Forum engagement (received from others)
	XPSourceForumReply
	XPSourceForumHelpful

	// Mentorship outcomes (paid to the inviter)
	XPSourceInviteeFirstInitiative
	XPSourceInviteeEstablished

	// Definition-driven rewards
	XPSourceAchievement
	XPSourceQuest
)

// String names the source for events.
func (s XPSource) String() string {
	switch s {
	case XPSourceVoteCast:
		return "vote_cast"
	case XPSourceProposalCreated:
		return "proposal_created"
	case XPSourceForumReply:
		return "forum_reply"
	case XPSourceForumHelpful:
		return "forum_helpful"
	case XPSourceInviteeFirstInitiative:
		return "invitee_first_initiative"
	case XPSourceInviteeEstablished:
		return "invitee_established"
	case XPSourceAchievement:
		return "achievement"
	case XPSourceQuest:
		return "quest"
	default:
		return "other"
	}
}

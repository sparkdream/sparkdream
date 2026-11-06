package types

import (
	commontypes "sparkdream/x/common/types"

	errorsmod "cosmossdk.io/errors"
)

// Query-time display rules (docs/x-artifact-spec.md §7.17). Stored state
// never changes for a schedule or a media choice; the view picks what to
// show.

// Display sources reported in TokenView.display_source.
const (
	DisplayStored       = "stored"
	DisplaySchedule     = "schedule"
	DisplayMediaVersion = "media_version"
	DisplayRenderer     = "renderer"
	DisplayUnrevealed   = "unrevealed"
)

// ValidateSchedule checks a metadata schedule: at most max stages, strictly
// increasing from_time, every stage after now, every stage valid metadata.
func ValidateSchedule(stages []MetadataStage, now int64, p Params) error {
	if commontypes.Saturate[uint32](len(stages)) > p.MaxScheduleStages {
		return errorsmod.Wrapf(ErrInvalidSchedule, "at most %d stages", p.MaxScheduleStages)
	}
	last := now
	for i, st := range stages {
		if st.FromTime <= last {
			return errorsmod.Wrapf(ErrInvalidSchedule, "stage %d from_time %d must be after %d", i, st.FromTime, last)
		}
		last = st.FromTime
		if err := ValidateTokenMetadata(st.Metadata, p); err != nil {
			return errorsmod.Wrapf(err, "stage %d", i)
		}
	}
	return nil
}

// StageInEffect returns the schedule stage shown at now (-1 = the stored
// metadata, before the first stage).
func StageInEffect(stages []MetadataStage, now int64) int {
	idx := -1
	for i, st := range stages {
		if st.FromTime <= now {
			idx = i
		}
	}
	return idx
}

// StoredDisplay picks a token's displayed metadata from stored state alone
// (no renderer): the holder's media version first, then the schedule stage
// in effect, then the stored metadata.
func StoredDisplay(t Token, now int64) (TokenMetadata, string) {
	if t.Unrevealed {
		return TokenMetadata{}, DisplayUnrevealed
	}
	if t.DisplayedVersion > 0 && int(t.DisplayedVersion) <= len(t.MediaVersions) {
		return t.MediaVersions[t.DisplayedVersion-1], DisplayMediaVersion
	}
	if i := StageInEffect(t.Schedule, now); i >= 0 {
		return t.Schedule[i].Metadata, DisplaySchedule
	}
	return t.Metadata, DisplayStored
}

// LabelTokenAll ORs the media flags of the stored metadata, every schedule
// stage and every accepted media version, so a token with any external
// media anywhere is labelled.
func LabelTokenAll(t Token) uint32 {
	flags := LabelToken(t.Metadata)
	for _, st := range t.Schedule {
		flags |= LabelToken(st.Metadata)
	}
	for _, v := range t.MediaVersions {
		flags |= LabelToken(v)
	}
	return flags
}

// IsExpired reports whether a token's expiry has passed (0 = never).
func IsExpired(t Token, now int64) bool {
	return t.ExpiresAt > 0 && t.ExpiresAt <= now
}

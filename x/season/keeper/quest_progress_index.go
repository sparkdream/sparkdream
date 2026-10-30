package keeper

import (
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	"sparkdream/x/season/types"
)

// QuestProgressIndexes are the secondary indexes of MemberQuestProgress.
type QuestProgressIndexes struct {
	// Open: (member + openSuffix, "member:quest") for every record that is
	// not completed, and (member + doneSuffix, key) for the rest, so the
	// quest hooks, which run inside other modules' transactions, read only a
	// member's open quests (SEA-15).
	Open *indexes.Multi[string, string, types.MemberQuestProgress]
}

const (
	questOpenSuffix = "|open"
	questDoneSuffix = "|done"
)

func (i QuestProgressIndexes) IndexesList() []collections.Index[string, types.MemberQuestProgress] {
	return []collections.Index[string, types.MemberQuestProgress]{i.Open}
}

func newQuestProgressIndexes(sb *collections.SchemaBuilder) QuestProgressIndexes {
	return QuestProgressIndexes{
		Open: indexes.NewMulti(sb, types.MemberQuestProgressOpenKey, "member_quest_progress_open",
			collections.StringKey, collections.StringKey,
			func(pk string, v types.MemberQuestProgress) (string, error) {
				member, _, _ := strings.Cut(pk, ":")
				if v.Completed {
					return member + questDoneSuffix, nil
				}
				return member + questOpenSuffix, nil
			}),
	}
}

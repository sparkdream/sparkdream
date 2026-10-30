package types

import "cosmossdk.io/collections"

// MemberQuestProgressKey is the prefix to retrieve all MemberQuestProgress
var MemberQuestProgressKey = collections.NewPrefix("memberQuestProgress/value/")

// MemberQuestProgressOpenKey: index of MemberQuestProgress by (member,
// open), maintained by the IndexedMap. The quest hooks read only a member's
// open (not completed) quests through it (SEA-15).
var MemberQuestProgressOpenKey = collections.NewPrefix("memberQuestProgress/open/")

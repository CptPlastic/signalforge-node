package database

import "sort"

// GroupCallsIntoConversations groups calls when their start time is no more
// than gapSeconds after the end of the preceding call on the same system and
// talkgroup. It does not persist grouping metadata, so changing the configured
// gap immediately changes both historic and new call-log views.
func GroupCallsIntoConversations(calls []Call, gapSeconds float64) []Conversation {
	if len(calls) == 0 {
		return nil
	}
	if gapSeconds < 0 {
		gapSeconds = 0
	}

	sorted := append([]Call(nil), calls...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].DateTime == sorted[j].DateTime {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].DateTime < sorted[j].DateTime
	})

	conversations := make([]Conversation, 0, len(sorted))
	activeByChannel := make(map[conversationChannel]int)
	for _, call := range sorted {
		end := callEndTime(call)
		key := conversationChannel{system: call.System, talkgroup: call.Talkgroup}
		if index, found := activeByChannel[key]; found &&
			float64(call.DateTime) <= float64(conversations[index].EndDateTime)+gapSeconds {
			current := &conversations[index]
			current.Parts = append(current.Parts, call)
			current.PartCount++
			current.Duration += call.Duration
			if end > current.EndDateTime {
				current.EndDateTime = end
			}
			continue
		}

		conversations = append(conversations, Conversation{
			Call:        call,
			EndDateTime: end,
			PartCount:   1,
			Parts:       []Call{call},
		})
		activeByChannel[key] = len(conversations) - 1
	}
	return conversations
}

type conversationChannel struct {
	system    int
	talkgroup int
}

func callEndTime(call Call) int64 {
	duration := int64(call.Duration)
	if call.Duration > float64(duration) {
		duration++
	}
	if duration < 0 {
		duration = 0
	}
	return call.DateTime + duration
}

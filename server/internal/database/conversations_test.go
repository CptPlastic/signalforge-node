package database

import (
	"math"
	"testing"
)

func TestGroupCallsIntoConversations(t *testing.T) {
	calls := []Call{
		{ID: 3, DateTime: 110, System: 1, Talkgroup: 100, Duration: 2},
		{ID: 1, DateTime: 100, System: 1, Talkgroup: 100, Duration: 2.1},
		{ID: 2, DateTime: 106, System: 1, Talkgroup: 100, Duration: 0.2},
		{ID: 4, DateTime: 114, System: 1, Talkgroup: 100, Duration: 1},
		{ID: 5, DateTime: 111, System: 2, Talkgroup: 100, Duration: 1},
		{ID: 6, DateTime: 200, System: 1, Talkgroup: 100, Duration: 1},
	}

	got := GroupCallsIntoConversations(calls, 4)
	if len(got) != 3 {
		t.Fatalf("conversation count = %d, want 3", len(got))
	}
	first := got[0]
	if first.ID != 1 || first.PartCount != 4 {
		t.Fatalf("first conversation = id %d, %d parts; want id 1, 4 parts", first.ID, first.PartCount)
	}
	if math.Abs(first.Duration-5.3) > 0.00001 {
		t.Fatalf("first duration = %.1f, want 5.3", first.Duration)
	}
	if first.EndDateTime != 115 {
		t.Fatalf("first end = %d, want 115", first.EndDateTime)
	}
	for index, wantID := range []int64{1, 2, 3, 4} {
		if first.Parts[index].ID != wantID {
			t.Fatalf("first part %d = %d, want %d", index, first.Parts[index].ID, wantID)
		}
	}
	if got[1].ID != 5 || got[2].ID != 6 {
		t.Fatalf("unexpected standalone conversations: %d, %d", got[1].ID, got[2].ID)
	}
}

func TestGroupCallsIntoConversationsRespectsGapAfterEnd(t *testing.T) {
	calls := []Call{
		{ID: 1, DateTime: 100, System: 1, Talkgroup: 100, Duration: 5},
		{ID: 2, DateTime: 109, System: 1, Talkgroup: 100, Duration: 1},
		{ID: 3, DateTime: 110, System: 1, Talkgroup: 100, Duration: 1},
	}

	got := GroupCallsIntoConversations(calls, 4)
	if len(got) != 1 || got[0].PartCount != 3 {
		t.Fatalf("got %#v, want one three-part conversation", got)
	}
	if got[0].EndDateTime != 111 {
		t.Fatalf("end = %d, want 111", got[0].EndDateTime)
	}
}

func TestGroupCallsIntoConversationsDoesNotCrossGap(t *testing.T) {
	calls := []Call{
		{ID: 1, DateTime: 100, System: 1, Talkgroup: 100, Duration: 1},
		{ID: 2, DateTime: 106, System: 1, Talkgroup: 100, Duration: 1},
	}

	if got := GroupCallsIntoConversations(calls, 4); len(got) != 2 {
		t.Fatalf("conversation count = %d, want 2", len(got))
	}
}

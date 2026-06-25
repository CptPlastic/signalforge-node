package api

import (
	"log/slog"
	"testing"
	"time"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

func TestStreamHubPushDeliversToMultipleSourceMatchedListeners(t *testing.T) {
	hub := newStreamHub(slog.Default())
	first, unsubscribeFirst := hub.subscribe("owner-a", "", []int{101}, nil, []string{"src-main"})
	defer unsubscribeFirst()
	second, unsubscribeSecond := hub.subscribe("owner-a", "", []int{101}, nil, []string{"src-main"})
	defer unsubscribeSecond()

	hub.push(&database.Call{ID: 42, SourceID: "src-main", Talkgroup: 101, AudioType: "audio/mpeg"}, []byte("audio"))

	assertStreamCall(t, first, 42)
	assertStreamCall(t, second, 42)
}

func TestStreamHubPushFiltersByTalkgroupAndSource(t *testing.T) {
	hub := newStreamHub(slog.Default())
	wrongTalkgroup, unsubscribeWrongTalkgroup := hub.subscribe("owner-a", "", []int{202}, nil, []string{"src-main"})
	defer unsubscribeWrongTalkgroup()
	wrongSource, unsubscribeWrongSource := hub.subscribe("owner-a", "", []int{101}, nil, []string{"src-other"})
	defer unsubscribeWrongSource()
	matched, unsubscribeMatched := hub.subscribe("owner-a", "", []int{101}, nil, []string{"src-main"})
	defer unsubscribeMatched()

	hub.push(&database.Call{ID: 7, SourceID: "src-main", Talkgroup: 101}, []byte("audio"))

	assertNoStreamCall(t, wrongTalkgroup)
	assertNoStreamCall(t, wrongSource)
	assertStreamCall(t, matched, 7)
}

func TestStreamHubPushMatchesTalkgroupGroup(t *testing.T) {
	hub := newStreamHub(slog.Default())
	matched, unsubscribeMatched := hub.subscribe("owner-a", "", nil, []string{"FIRE"}, []string{"src-main"})
	defer unsubscribeMatched()
	unmatched, unsubscribeUnmatched := hub.subscribe("owner-a", "", nil, []string{"POLICE"}, []string{"src-main"})
	defer unsubscribeUnmatched()

	hub.push(&database.Call{ID: 11, SourceID: "src-main", Talkgroup: 5001, TalkgroupGroup: "FIRE"}, []byte("audio"))

	assertStreamCall(t, matched, 11)
	assertNoStreamCall(t, unmatched)
}

func TestStreamHubPushStillMatchesCallOwner(t *testing.T) {
	hub := newStreamHub(slog.Default())
	ch, unsubscribe := hub.subscribe("owner-a", "", []int{303}, nil, nil)
	defer unsubscribe()

	hub.push(&database.Call{ID: 9, UserID: "owner-a", Talkgroup: 303}, []byte("audio"))

	assertStreamCall(t, ch, 9)
}

func assertStreamCall(t *testing.T, ch <-chan streamChunk, wantID int64) {
	t.Helper()
	select {
	case chunk := <-ch:
		if chunk.meta.ID != wantID {
			t.Fatalf("stream call id = %d, want %d", chunk.meta.ID, wantID)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for stream call %d", wantID)
	}
}

func assertNoStreamCall(t *testing.T, ch <-chan streamChunk) {
	t.Helper()
	select {
	case chunk := <-ch:
		t.Fatalf("unexpected stream call %d", chunk.meta.ID)
	case <-time.After(25 * time.Millisecond):
	}
}

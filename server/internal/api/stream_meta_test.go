package api

import (
	"log/slog"
	"testing"

	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

func TestPublicCallMetaFromCallRF(t *testing.T) {
	msg := playerCallMetaFromCall(database.Call{
		ID:             1124569,
		Talkgroup:      3247,
		TalkgroupLabel: "OKC PD Santa Fe",
		TalkgroupGroup: "Oklahoma City",
		TalkgroupTag:   "Oklahoma",
		DateTime:       1781752255,
		Duration:       1.4142,
		Frequency:      853175000,
		SystemLabel:    "OKWIN",
		Origin:         "rf",
		SourceID:       "src-1",
	}, "OKWIN Scanner")

	if msg.Cmd != "call_meta" {
		t.Fatalf("cmd = %q, want call_meta", msg.Cmd)
	}
	if msg.Site != "Oklahoma" {
		t.Fatalf("site = %q, want Oklahoma", msg.Site)
	}
	if msg.SourceType != "RF" {
		t.Fatalf("sourceType = %q, want RF", msg.SourceType)
	}
	if msg.Category != "rf" {
		t.Fatalf("category = %q, want rf", msg.Category)
	}
	if msg.SourceLabel != "OKWIN Scanner" {
		t.Fatalf("sourceLabel = %q", msg.SourceLabel)
	}
}

func TestPublicCallMetaFromCallPTTCanary(t *testing.T) {
	msg := playerCallMetaFromCall(database.Call{
		ID:             99,
		Talkgroup:      9000001,
		TalkgroupLabel: "CANARY",
		TalkgroupGroup: "PTT",
		DateTime:       1781752255,
		Duration:       1.0,
		Origin:         "ptt",
		SenderEmail:    "ops@example.com",
	}, "CANARY")

	if msg.SourceType != "PTT" {
		t.Fatalf("sourceType = %q, want PTT", msg.SourceType)
	}
	if msg.Category != "ptt" {
		t.Fatalf("category = %q, want ptt", msg.Category)
	}
	if msg.SourceLabel != "CANARY" {
		t.Fatalf("sourceLabel = %q, want CANARY", msg.SourceLabel)
	}
}

func TestPublicCallMetaFromCallRFCANARY(t *testing.T) {
	msg := playerCallMetaFromCall(database.Call{
		ID:             100,
		Talkgroup:      5001,
		TalkgroupLabel: "CANARY",
		TalkgroupGroup: "OKWIN",
		SystemLabel:    "OKWIN",
		Origin:         "rf",
		AudioName:      "canary-1781752255.wav",
	}, "CANARY")

	if msg.SourceType != "RF" {
		t.Fatalf("sourceType = %q, want RF", msg.SourceType)
	}
	if msg.Category != "rf" {
		t.Fatalf("category = %q, want rf", msg.Category)
	}
	if msg.SourceLabel != "CANARY" {
		t.Fatalf("sourceLabel = %q, want CANARY", msg.SourceLabel)
	}
}

func TestStreamHubPushDeliversPTTOnVirtualTalkgroup(t *testing.T) {
	hub := newStreamHub(slog.Default())
	pttTG := 9000015
	ch, unsubscribe := hub.subscribe("owner-a", "", []int{101, pttTG}, []string{"PTT"}, []string{"src-main"})
	defer unsubscribe()

	hub.push(&database.Call{
		ID:             55,
		UserID:         "owner-b",
		Talkgroup:      pttTG,
		TalkgroupGroup: "PTT",
		Origin:         "ptt",
		SenderEmail:    "ops@example.com",
	}, []byte("audio"))

	assertStreamCall(t, ch, 55)
}

func TestPublicCallMetaClassifiesPTTByTalkgroupRange(t *testing.T) {
	msg := playerCallMetaFromCall(database.Call{
		ID:        1,
		Talkgroup: 9000015,
		Origin:    "rf",
	}, "PTT")

	if msg.SourceType != "PTT" {
		t.Fatalf("sourceType = %q, want PTT", msg.SourceType)
	}
	if msg.Category != "ptt" {
		t.Fatalf("category = %q, want ptt", msg.Category)
	}
}

func TestPublicCallSiteFallsBackToGroup(t *testing.T) {
	site := publicCallSite(database.Call{
		TalkgroupGroup: "Oklahoma City",
	})
	if site != "Oklahoma City" {
		t.Fatalf("site = %q", site)
	}
}

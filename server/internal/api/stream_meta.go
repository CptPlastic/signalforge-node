package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/projectseven-co-ltd/p7-scanner/server/internal/database"
)

// playerWSCallMetaMsg is the metadata-only frame for /public/ws-meta/{token}.
type playerWSCallMetaMsg struct {
	Cmd            string  `json:"cmd"`
	ID             int64   `json:"id"`
	Talkgroup      int     `json:"talkgroup"`
	TalkgroupLabel string  `json:"talkgroupLabel"`
	TalkgroupGroup string  `json:"talkgroupGroup"`
	DateTime       int64   `json:"dateTime"`
	Duration       float64 `json:"duration"`
	Frequency      int     `json:"frequency"`
	SystemLabel    string  `json:"systemLabel"`
	Site           string  `json:"site"`
	SourceType     string  `json:"sourceType"`
	SourceLabel    string  `json:"sourceLabel"`
	Category       string  `json:"category"`
}

func playerCallMetaFromCall(call database.Call, sourceLabel string) playerWSCallMetaMsg {
	return playerWSCallMetaMsg{
		Cmd:            "call_meta",
		ID:             call.ID,
		Talkgroup:      call.Talkgroup,
		TalkgroupLabel: call.TalkgroupLabel,
		TalkgroupGroup: call.TalkgroupGroup,
		DateTime:       call.DateTime,
		Duration:       call.Duration,
		Frequency:      call.Frequency,
		SystemLabel:    call.SystemLabel,
		Site:           publicCallSite(call),
		SourceType:     publicCallSourceType(call),
		SourceLabel:    sourceLabel,
		Category:       publicCallCategory(call),
	}
}

func playerCallMetaFromStreamMeta(meta streamCallMeta, sourceLabel string) playerWSCallMetaMsg {
	return playerCallMetaFromCall(database.Call{
		ID:             meta.ID,
		Talkgroup:      meta.Talkgroup,
		TalkgroupLabel: meta.TalkgroupLabel,
		TalkgroupGroup: meta.TalkgroupGroup,
		DateTime:       meta.DateTime,
		Duration:       meta.Duration,
		Frequency:      meta.Frequency,
		SystemLabel:    meta.SystemLabel,
		Origin:         meta.Origin,
		SenderEmail:    meta.SenderEmail,
		SourceID:       meta.SourceID,
		TalkgroupTag:   meta.TalkgroupTag,
		AudioName:      meta.AudioName,
	}, sourceLabel)
}

func publicCallSite(call database.Call) string {
	if site := strings.TrimSpace(call.TalkgroupTag); site != "" {
		return site
	}
	return strings.TrimSpace(call.TalkgroupGroup)
}

func publicCallSourceType(call database.Call) string {
	if isPTTCall(&call) {
		if strings.ToLower(strings.TrimSpace(call.Origin)) == "ptt-dispatch" {
			return "DISPATCH"
		}
		return "PTT"
	}
	return "RF"
}

func publicCallCategory(call database.Call) string {
	if isPTTCall(&call) {
		if strings.ToLower(strings.TrimSpace(call.Origin)) == "ptt-dispatch" {
			return "dispatch"
		}
		return "ptt"
	}
	return "rf"
}

func publicCallIsCanary(call database.Call) bool {
	label := strings.ToUpper(strings.TrimSpace(call.TalkgroupLabel))
	if strings.Contains(label, "CANARY") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(call.AudioName)), "canary-")
}

func (h *handler) publicCallSourceLabel(call database.Call, cache map[string]string) string {
	if publicCallIsCanary(call) {
		if label := strings.TrimSpace(call.TalkgroupLabel); label != "" {
			return strings.ToUpper(label)
		}
		return "CANARY"
	}

	origin := strings.ToLower(strings.TrimSpace(call.Origin))
	if origin == "ptt" || origin == "ptt-dispatch" {
		if email := strings.TrimSpace(call.SenderEmail); email != "" {
			if at := strings.Index(email, "@"); at > 0 {
				return strings.ToUpper(email[:at])
			}
			return strings.ToUpper(email)
		}
		return "PTT"
	}

	sourceID := strings.TrimSpace(call.SourceID)
	if sourceID == "" {
		return ""
	}
	if cached, ok := cache[sourceID]; ok {
		return cached
	}
	label := ""
	if src, ok, err := h.db.GetIngestionSource(sourceID); err == nil && ok {
		label = strings.TrimSpace(src.Label)
	}
	cache[sourceID] = label
	return label
}

func (h *handler) enrichCallSenderEmail(call *database.Call) {
	if strings.TrimSpace(call.SenderEmail) != "" || strings.TrimSpace(call.SenderUserID) == "" {
		return
	}
	user, ok, err := h.db.GetUserByID(call.SenderUserID)
	if err != nil || !ok {
		return
	}
	call.SenderEmail = user.Email
}

// handlePublicWSMeta serves metadata-only call events for a radio set share token.
// Same access rules as /public/ws/{token}; no audio is sent.
func (h *handler) handlePublicWSMeta(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	rs, err := h.db.GetRadioSetByShareToken(token)
	if err != nil || rs == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	sourceIDs, err := h.db.ListReadableSourceIDsForUser(rs.UserID)
	if err != nil {
		h.logger.Error("list public meta stream readable sources failed", "error", err)
		http.Error(w, "query sources", http.StatusInternalServerError)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.logger.Error("public meta ws accept failed", "error", err)
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	h.logger.Info("public meta stream connected",
		"radio_set_id", rs.ID,
		"radio_set_name", rs.Name,
		"owner_user_id", rs.UserID,
	)

	subscribedTalkgroups, subscribedGroups := publicStreamSubscription(rs)

	ch, unsubscribe := h.streamHub.subscribe(rs.UserID, rs.ID, subscribedTalkgroups, subscribedGroups, sourceIDs)
	defer unsubscribe()

	sourceLabels := make(map[string]string)

	sendMeta := func(meta streamCallMeta) error {
		call := database.Call{
			ID:             meta.ID,
			Talkgroup:      meta.Talkgroup,
			TalkgroupLabel: meta.TalkgroupLabel,
			TalkgroupGroup: meta.TalkgroupGroup,
			TalkgroupTag:   meta.TalkgroupTag,
			DateTime:       meta.DateTime,
			Duration:       meta.Duration,
			Frequency:      meta.Frequency,
			SystemLabel:    meta.SystemLabel,
			AudioName:      meta.AudioName,
			Origin:         meta.Origin,
			SenderEmail:    meta.SenderEmail,
			SenderUserID:   meta.SenderUserID,
			SourceID:       meta.SourceID,
		}
		h.enrichCallSenderEmail(&call)
		msg := playerCallMetaFromCall(call, h.publicCallSourceLabel(call, sourceLabels))
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, data)
	}

	skipSeed := r.URL.Query().Get("seed") == "0"
	if !skipSeed {
		seedCalls, seedErr := h.seedPublicStreamCalls(rs, subscribedTalkgroups)
		if seedErr != nil {
			h.logger.Error("public meta stream seed query failed", "radio_set_id", rs.ID, "error", seedErr)
		} else {
			for _, c := range seedCalls {
				meta := streamCallMeta{
					ID:             c.ID,
					Talkgroup:      c.Talkgroup,
					TalkgroupLabel: c.TalkgroupLabel,
					TalkgroupGroup: c.TalkgroupGroup,
					TalkgroupTag:   c.TalkgroupTag,
					DateTime:       c.DateTime,
					Duration:       c.Duration,
					Frequency:      c.Frequency,
					SystemLabel:    c.SystemLabel,
					Origin:         c.Origin,
					SenderUserID:   c.SenderUserID,
					SenderEmail:    c.SenderEmail,
					SourceID:       c.SourceID,
					AudioName:      c.AudioName,
				}
				if err := sendMeta(meta); err != nil {
					return
				}
			}
		}
	}

	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if err := sendMeta(chunk.meta); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

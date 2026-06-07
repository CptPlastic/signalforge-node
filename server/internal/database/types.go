package database

import "encoding/json"

// HubIdentity identifies this SignalForge Hub instance for future SignalHub federation.
type HubIdentity struct {
	HubID                     string `json:"hubId"`
	Name                      string `json:"name"`
	PublicURL                 string `json:"publicUrl"`
	Region                    string `json:"region"`
	Contact                   string `json:"contact"`
	PublicKey                 string `json:"publicKey"`
	PrivateKey                string `json:"-"`
	FederationEnabled         bool   `json:"federationEnabled"`
	DirectoryValidationStatus string `json:"directoryValidationStatus"`
	TrustLevel                string `json:"trustLevel"`
	TrustIssuerHubID          string `json:"trustIssuerHubId"`
	TrustCertificate          string `json:"trustCertificate"`
	TrustExpiresAt            int64  `json:"trustExpiresAt"`
	TrustVerifiedAt           int64  `json:"trustVerifiedAt"`
	CreatedAt                 int64  `json:"createdAt"`
	UpdatedAt                 int64  `json:"updatedAt"`
}

// HubInvite is an admin-generated token another hub can use to become a trusted peer.
type HubInvite struct {
	ID              string `json:"id"`
	Token           string `json:"token"`
	CreatedByUserID string `json:"createdByUserId,omitempty"`
	ExpiresAt       int64  `json:"expiresAt"`
	UsedAt          int64  `json:"usedAt"`
	RevokedAt       int64  `json:"revokedAt"`
	CreatedAt       int64  `json:"createdAt"`
}

// HubPeer is a trusted remote SignalForge Hub known to this SignalHub instance.
type HubPeer struct {
	ID         string `json:"id"`
	HubID      string `json:"hubId"`
	Name       string `json:"name"`
	PublicURL  string `json:"publicUrl"`
	Region     string `json:"region"`
	Contact    string `json:"contact"`
	Status     string `json:"status"`
	Direction  string `json:"direction"`
	AcceptedAt int64  `json:"acceptedAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// Call is a single decoded radio call stored by the server.
type Call struct {
	ID                 int64   `json:"id"`
	UserID             string  `json:"userId,omitempty"`
	SourceID           string  `json:"sourceId,omitempty"`
	Redacted           bool    `json:"redacted,omitempty"`
	DateTime           int64   `json:"dateTime"`
	System             int     `json:"system"`
	SystemLabel        string  `json:"systemLabel"`
	Talkgroup          int     `json:"talkgroup"`
	TalkgroupLabel     string  `json:"talkgroupLabel"`
	TalkgroupGroup     string  `json:"talkgroupGroup"`
	TalkgroupTag       string  `json:"talkgroupTag"`
	Frequency          int     `json:"frequency"`
	Duration           float64 `json:"duration"`
	AudioName          string  `json:"audioName"`
	AudioType          string  `json:"audioType"`
	TranscriptText     string  `json:"transcriptText,omitempty"`
	TranscriptStatus   string  `json:"transcriptStatus,omitempty"`
	TranscriptProvider string  `json:"transcriptProvider,omitempty"`
	Origin             string  `json:"origin,omitempty"`
	SenderUserID       string  `json:"senderUserId,omitempty"`
	// SenderEmail is a transient enrichment populated by the PTT handler for
	// live WS broadcasts. It is not stored in the calls table, so historical
	// PTT calls fetched from the DB will leave it empty — that's intentional.
	SenderEmail string `json:"senderEmail,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
}

// TranscriptionJob is a queued call ready for an external worker.
type TranscriptionJob struct {
	CallID         int64   `json:"callId"`
	AudioName      string  `json:"audioName"`
	AudioType      string  `json:"audioType"`
	Duration       float64 `json:"duration"`
	SystemLabel    string  `json:"systemLabel"`
	Talkgroup      int     `json:"talkgroup"`
	TalkgroupLabel string  `json:"talkgroupLabel"`
	TalkgroupGroup string  `json:"talkgroupGroup"`
	Attempts       int     `json:"attempts"`
	ClaimedUntil   int64   `json:"claimedUntil"`
}

// TalkgroupSetting stores per-talkgroup operator preferences.
type TalkgroupSetting struct {
	Talkgroup  int   `json:"talkgroup"`
	Favorite   bool  `json:"favorite"`
	Muted      bool  `json:"muted"`
	Transcribe bool  `json:"transcribe"`
	UpdatedAt  int64 `json:"updatedAt"`
}

// IngestionSource represents a scanner ingestion source (e.g., SDRTrunk, mock sender).
type IngestionSource struct {
	ID            string `json:"id"`
	UserID        string `json:"userId,omitempty"`
	Label         string `json:"label"`
	Enabled       bool   `json:"enabled"`
	IsShared      bool   `json:"isShared"`
	DeletedAt     int64  `json:"deletedAt,omitempty"`
	SystemID      int    `json:"systemId"`
	SystemLabel   string `json:"systemLabel"`
	LastSeenUnix  int64  `json:"lastSeenUnix"`
	ErrorCount    int    `json:"errorCount"`
	CallsReceived int    `json:"callsReceived"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// User represents an account that can own sources and sessions.
type User struct {
	ID                  string `json:"id"`
	Email               string `json:"email"`
	Role                string `json:"role"`
	Status              string `json:"status"`
	TxEnabled           bool   `json:"txEnabled"`
	DispatcherEnabled   bool   `json:"dispatcherEnabled"`
	PasswordConfigured  bool   `json:"passwordConfigured"`
	CreatedAt           int64  `json:"createdAt"`
	UpdatedAt           int64  `json:"updatedAt"`
}

// AuditLogEntry represents a single audit event.
type AuditLogEntry struct {
	ID          int64           `json:"id"`
	UserID      string          `json:"userId,omitempty"`
	UserEmail   string          `json:"userEmail,omitempty"`
	Action      string          `json:"action"`
	TargetType  string          `json:"targetType"`
	TargetID    string          `json:"targetId"`
	TargetEmail string          `json:"targetEmail,omitempty"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   int64           `json:"createdAt"`
}

// SourceAPIKey represents an API key for a source.
type SourceAPIKey struct {
	ID         string `json:"id"`
	SourceID   string `json:"sourceId"`
	UserID     string `json:"userId,omitempty"`
	APIKey     string `json:"apiKey"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
}

// SourceShare grants one user access to one ingestion source.
type SourceShare struct {
	SourceID  string `json:"sourceId"`
	UserID    string `json:"userId"`
	CreatedAt int64  `json:"createdAt"`
}

// TalkgroupInfo is a distinct talkgroup entry derived from call history.
type TalkgroupInfo struct {
	Talkgroup      int    `json:"talkgroup"`
	TalkgroupLabel string `json:"talkgroupLabel"`
	TalkgroupGroup string `json:"talkgroupGroup"`
	SystemLabel    string `json:"systemLabel"`
}

// RadioSet is a named, user-owned playset used to filter the call log and public player.
// selectionMode is "talkgroups" (explicit TG IDs) or "groups" (dynamic talkgroup_group names).
type RadioSet struct {
	ID              string   `json:"id"`
	UserID          string   `json:"userId,omitempty"`
	Name            string   `json:"name"`
	SelectionMode   string   `json:"selectionMode"`
	Talkgroups      []int    `json:"talkgroups"`
	TalkgroupGroups []string `json:"talkgroupGroups"`
	SourceIDs       []string `json:"sourceIds,omitempty"`
	ShareToken      *string  `json:"shareToken,omitempty"`
	PTTTalkgroup    *int     `json:"pttTalkgroup,omitempty"`
	CreatedAt       int64    `json:"createdAt"`
	UpdatedAt       int64    `json:"updatedAt"`
}

// IsGroupsMode reports whether the set follows talkgroup_group names instead of TG IDs.
func (rs RadioSet) IsGroupsMode() bool {
	return normalizeRadioSetSelectionMode(rs.SelectionMode) == "groups"
}

// ListCallsParams configures filtering and sorting for ListCalls.
type ListCallsParams struct {
	Limit       int
	SortBy      string
	Order       string
	Search      string
	Group       string
	Offset      int
	UserID      string
	OnlyUnowned bool
	Talkgroups      []int
	Groups          []string
}

// FederatedCall is a call payload shared between trusted hubs.
type FederatedCall struct {
	Call      Call   `json:"call"`
	Source    string `json:"sourceId"`
	Audio     []byte `json:"-"`
	AudioBase string `json:"audioBase64,omitempty"`
}

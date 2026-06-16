package hub

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Metadata struct {
	System         int
	SystemLabel    string
	Talkgroup      int
	TalkgroupLabel string
	TalkgroupGroup string
	TalkgroupTag   string
	Frequency      int
}

type UploadFields struct {
	Metadata  Metadata
	AudioName string
	AudioType string
	StartedAt time.Time
	Duration  time.Duration
}

type Client struct {
	baseURL   *url.URL
	http      *http.Client
	sourceKey string
}

func NewClient(hubURL, sourceKey string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	parsed, err := url.Parse(strings.TrimSpace(hubURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid hub url %q", hubURL)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &Client{
		baseURL:   parsed,
		http:      &http.Client{Timeout: timeout},
		sourceKey: strings.TrimSpace(sourceKey),
	}, nil
}

func (c *Client) ProbeSourceKey() error {
	if c.sourceKey == "" {
		return errors.New("source key is required")
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("key", c.sourceKey)
	_ = writer.WriteField("test", "1")
	_ = writer.Close()

	req, err := http.NewRequest(http.MethodPost, c.endpoint("/api/call-upload"), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "SignalForge Trunk")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	message := strings.TrimSpace(string(respBody))
	if resp.StatusCode == http.StatusExpectationFailed && strings.Contains(message, "incomplete call data") {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("source key rejected")
	}
	return fmt.Errorf("source key probe returned %s: %s", resp.Status, message)
}

func (c *Client) UploadFile(path string, fields UploadFields) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if fields.AudioName == "" {
		fields.AudioName = filepath.Base(path)
	}
	if fields.AudioType == "" {
		fields.AudioType = audioTypeForPath(path)
	}
	return c.uploadReader(file, fields)
}

func (c *Client) uploadReader(audio io.Reader, fields UploadFields) error {
	if c.sourceKey == "" {
		return errors.New("source key is required")
	}
	if fields.StartedAt.IsZero() {
		fields.StartedAt = time.Now()
	}
	if fields.AudioType == "" {
		fields.AudioType = "audio/wav"
	}
	if fields.Duration <= 0 {
		fields.Duration = time.Second
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	formFields := map[string]string{
		"key":            c.sourceKey,
		"system":         strconv.Itoa(fields.Metadata.System),
		"systemLabel":    fields.Metadata.SystemLabel,
		"talkgroup":      strconv.Itoa(fields.Metadata.Talkgroup),
		"talkgroupLabel": fields.Metadata.TalkgroupLabel,
		"talkgroupGroup": fields.Metadata.TalkgroupGroup,
		"talkgroupTag":   fields.Metadata.TalkgroupTag,
		"frequency":      strconv.Itoa(fields.Metadata.Frequency),
		"dateTime":       strconv.FormatInt(fields.StartedAt.Unix(), 10),
		"duration":       fmt.Sprintf("%.3f", fields.Duration.Seconds()),
		"audioName":      fields.AudioName,
		"audioType":      fields.AudioType,
	}
	for key, value := range formFields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("audio", fields.AudioName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, audio); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint("/api/call-upload"), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "SignalForge Trunk")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upload returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func (c *Client) endpoint(path string) string {
	clone := *c.baseURL
	clone.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	return clone.String()
}

func audioTypeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	default:
		return ""
	}
}

package api

import (
	"bytes"
	"encoding/json"
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

	"github.com/projectseven-co-ltd/p7-scanner/tools/signalforge-cli/internal/recorder"
)

type Client struct {
	baseURL   *url.URL
	http      *http.Client
	sourceKey string
}

type Health struct {
	Status string `json:"status"`
}

type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type UploadFields struct {
	Metadata  recorder.Metadata
	AudioName string
	AudioType string
	StartedAt time.Time
	Duration  time.Duration
}

func NewClient(hubURL, sourceKey string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	parsed, err := url.Parse(strings.TrimSpace(hubURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid hub url %q", hubURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("hub url must use http or https")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return &Client{
		baseURL:   parsed,
		http:      &http.Client{Timeout: timeout},
		sourceKey: strings.TrimSpace(sourceKey),
	}, nil
}

func (c *Client) BaseURL() string {
	return strings.TrimRight(c.baseURL.String(), "/")
}

func (c *Client) Health() (Health, error) {
	var health Health
	if err := c.getJSON("/api/v1/health", &health); err != nil {
		return Health{}, err
	}
	return health, nil
}

func (c *Client) Version() (Version, error) {
	var version Version
	if err := c.getJSON("/api/v1/version", &version); err != nil {
		return Version{}, err
	}
	return version, nil
}

func (c *Client) ProbeSourceKey() error {
	if c.sourceKey == "" {
		return errors.New("source key is required; pass --source-key or set SIGNALFORGE_SOURCE_KEY")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("key", c.sourceKey); err != nil {
		return err
	}
	if err := writer.WriteField("test", "1"); err != nil {
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
	req.Header.Set("User-Agent", "SignalForge CLI")

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
	if resp.StatusCode == http.StatusForbidden {
		return errors.New("source is disabled")
	}
	return fmt.Errorf("source key probe returned %s: %s", resp.Status, message)
}

func (c *Client) UploadFile(path string, fields UploadFields) error {
	if c.sourceKey == "" {
		return errors.New("source key is required; pass --source-key or set SIGNALFORGE_SOURCE_KEY")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	if fields.AudioName == "" {
		fields.AudioName = filepath.Base(path)
	}
	if fields.AudioType == "" {
		fields.AudioType = recorder.AudioTypeForPath(path)
	}
	if fields.StartedAt.IsZero() {
		fields.StartedAt = time.Now()
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
	if _, err := io.Copy(part, file); err != nil {
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
	req.Header.Set("User-Agent", "SignalForge CLI")
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

func (c *Client) getJSON(path string, target any) error {
	req, err := http.NewRequest(http.MethodGet, c.endpoint(path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "SignalForge CLI")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s returned %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *Client) endpoint(path string) string {
	clone := *c.baseURL
	clone.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	return clone.String()
}

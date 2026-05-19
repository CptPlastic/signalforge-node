package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const mailjetSendURL = "https://api.mailjet.com/v3.1/send"

func (h *handler) magicLinkVerifyURL(r *http.Request, token string) string {
	encodedToken := url.QueryEscape(token)
	if base := strings.TrimSpace(h.cfg.PublicBaseURL); base != "" {
		return strings.TrimRight(base, "/") + "/?token=" + encodedToken
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}

	return fmt.Sprintf("%s://%s/?token=%s", scheme, host, encodedToken)
}

func (h *handler) sendMagicLinkEmail(ctx context.Context, toEmail, verifyURL, token string) error {
	apiKey := strings.TrimSpace(h.cfg.MailjetAPIKey)
	secretKey := strings.TrimSpace(h.cfg.MailjetSecretKey)
	fromEmail := strings.TrimSpace(h.cfg.MailFromEmail)
	fromName := strings.TrimSpace(h.cfg.MailFromName)

	if fromName == "" {
		fromName = "P7 Scanner"
	}
	if apiKey == "" || secretKey == "" || fromEmail == "" {
		if h.cfg.AppEnv == "production" {
			return fmt.Errorf("mail delivery is not configured")
		}
		return nil
	}

	payload := map[string]any{
		"Messages": []map[string]any{
			{
				"From": map[string]string{
					"Email": fromEmail,
					"Name":  fromName,
				},
				"To": []map[string]string{{
					"Email": toEmail,
				}},
				"Subject": "Your P7 Scanner sign-in link",
				"TextPart": "Use this secure sign-in link (valid for 15 minutes):\n\n" + verifyURL +
					"\n\nIf your app asks for a token, paste this:\n\n" + token +
					"\n\nIf you did not request this, you can ignore this email.",
				"HTMLPart": "<p>Use this secure sign-in link (valid for 15 minutes):</p>" +
					"<p><a href=\"" + verifyURL + "\">Sign in to P7 Scanner</a></p>" +
					"<p>If your app asks for a token, paste this:</p>" +
					"<p><code>" + token + "</code></p>" +
					"<p>If you did not request this, you can ignore this email.</p>",
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mailjetSendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(apiKey, secretKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("mailjet send failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return nil
}

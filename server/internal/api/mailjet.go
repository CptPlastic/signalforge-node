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

func (h *handler) sendMagicLinkEmail(ctx context.Context, toEmail, verifyURL, token, code string) error {
	apiKey := strings.TrimSpace(h.cfg.MailjetAPIKey)
	secretKey := strings.TrimSpace(h.cfg.MailjetSecretKey)
	fromEmail := strings.TrimSpace(h.cfg.MailFromEmail)
	fromName := strings.TrimSpace(h.cfg.MailFromName)

	if fromName == "" {
		fromName = "SignalForge Hub"
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
				"Subject": "Your SignalForge Hub sign-in code: " + code,
				"TextPart": "Your sign-in code is:\n\n" + code +
					"\n\nEnter this code in the app to sign in (valid for 15 minutes).\n\n" +
					"Prefer a link? Tap to sign in instead:\n" + verifyURL +
					"\n\nIf the app asks for a token, paste this:\n\n" + token +
					"\n\nIf you did not request this, you can ignore this email.",
				"HTMLPart": "<p>Your sign-in code is:</p>" +
					"<p style=\"font-size:28px;font-weight:bold;letter-spacing:4px\">" + code + "</p>" +
					"<p>Enter this code in the app to sign in (valid for 15 minutes).</p>" +
					"<p>Prefer a link? <a href=\"" + verifyURL + "\">Sign in to SignalForge Hub</a></p>" +
					"<p>If the app asks for a token, paste this:</p>" +
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

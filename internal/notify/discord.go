// Copyright (c) 2026 Aristarh Ucolov.
//
// Minimal Discord webhook client. One POST per event, no retries beyond a
// single attempt — notifications are best-effort and must never block or
// crash server management.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 8 * time.Second}

// Discord posts a plain-content message to a Discord webhook URL.
func Discord(webhookURL, content string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return fmt.Errorf("discord: webhook URL is empty")
	}
	if !strings.HasPrefix(webhookURL, "https://") {
		return fmt.Errorf("discord: webhook URL must start with https://")
	}
	// Discord caps content at 2000 chars.
	if len(content) > 1900 {
		content = content[:1900] + "…"
	}
	body, _ := json.Marshal(map[string]string{"content": content})
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord: webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

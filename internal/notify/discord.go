// Package notify sends outbound alerts (currently only Discord webhooks)
// for SAM. It is intentionally minimal: one notifier, one Discord channel,
// best-effort delivery with a cooldown to avoid spam.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultCooldown = 60 * time.Minute

// Discord is a best-effort Discord webhook notifier with per-key cooldown.
//
// Cooldown is tracked per "key" (the worker uses the network name) so a
// low-bank alert for network "pocket" does not silence one for a different
// network and vice-versa.
type Discord struct {
	WebhookURL  string
	Template    string
	Cooldown    time.Duration
	HTTPClient  *http.Client
	Logger      *slog.Logger

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// NewDiscord builds a notifier. If template is empty a sensible default
// message is used. If cooldown is zero, 60 minutes is used.
func NewDiscord(webhookURL, template string, cooldown time.Duration, logger *slog.Logger) *Discord {
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	if template == "" {
		template = "⚠️ SAM bank low on {network}: balance {balance} POKT, need {needed} POKT (deficit {deficit} POKT). Top up {bank_address}."
	}
	return &Discord{
		WebhookURL: webhookURL,
		Template:   template,
		Cooldown:   cooldown,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Logger:     logger,
		lastSent:   make(map[string]time.Time),
	}
}

// Enabled reports whether the notifier has a webhook configured.
func (d *Discord) Enabled() bool {
	return d != nil && d.WebhookURL != ""
}

// Notify posts a templated message to Discord, respecting per-key cooldown.
//
// vars provides the substitution map for {placeholders} in the configured
// template. Unknown placeholders are left in place so misconfigurations are
// visible in the delivered message rather than silently dropped.
//
// Returns nil if the notifier is disabled or is still inside the cooldown
// window — both are treated as non-error skips so callers can call this
// unconditionally on each detection.
func (d *Discord) Notify(ctx context.Context, key string, vars map[string]string) error {
	if !d.Enabled() {
		return nil
	}

	d.mu.Lock()
	if last, ok := d.lastSent[key]; ok && time.Since(last) < d.Cooldown {
		d.mu.Unlock()
		d.Logger.Debug("discord notify: cooldown active, skipping",
			"key", key, "since_last", time.Since(last).Round(time.Second))
		return nil
	}
	d.mu.Unlock()

	message := d.Template
	for k, v := range vars {
		message = strings.ReplaceAll(message, "{"+k+"}", v)
	}

	payload, err := json.Marshal(map[string]string{"content": message})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("discord returned %d: %s", resp.StatusCode, string(body))
	}

	d.mu.Lock()
	d.lastSent[key] = time.Now()
	d.mu.Unlock()

	d.Logger.Info("discord notification sent", "key", key)
	return nil
}

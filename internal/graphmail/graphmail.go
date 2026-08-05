// Package graphmail sends email from a shared mailbox via Microsoft Graph using
// application permissions (client-credentials flow). It never acts on behalf of
// a user.
package graphmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultTokenURL = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"
	graphScope      = "https://graph.microsoft.com/.default"
)

// Config configures the app-only Graph client.
type Config struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	// TokenURL and GraphBaseURL default to the public Entra/Graph endpoints;
	// tests point them at local servers.
	TokenURL     string
	GraphBaseURL string
	HTTP         *http.Client
}

// Message is a plain-text email.
type Message struct {
	Subject string
	To      []string
	Body    string
}

// Client acquires app-only tokens and sends mail. Tokens are cached until just
// before expiry.
type Client struct {
	cfg Config
	mu  sync.Mutex
	now func() time.Time

	accessToken    string
	accessTokenExp time.Time
}

// New builds a client with the default endpoints applied.
func New(cfg Config) *Client {
	if cfg.TokenURL == "" {
		cfg.TokenURL = fmt.Sprintf(defaultTokenURL, cfg.TenantID)
	}
	if cfg.GraphBaseURL == "" {
		cfg.GraphBaseURL = "https://graph.microsoft.com/v1.0"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{cfg: cfg, now: time.Now}
}

// Send sends a plain-text email as the shared mailbox using application
// permissions (Mail.Send).
func (c *Client) Send(ctx context.Context, from string, msg Message) error {
	if from == "" {
		return errors.New("graphmail: empty shared mailbox")
	}
	if len(msg.To) == 0 {
		return errors.New("graphmail: no recipients")
	}
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"subject": msg.Subject,
			"body": map[string]string{
				"contentType": "Text",
				"content":     msg.Body,
			},
			"toRecipients": toRecipients(msg.To),
		},
		"saveToSentItems": false,
	})
	if err != nil {
		return err
	}

	u := c.cfg.GraphBaseURL + "/users/" + url.PathEscape(from) + "/sendMail"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("graphmail: sending: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("graphmail: sendMail returned %d: %s", resp.StatusCode, string(msg))
	}
	return nil
}

func toRecipients(emails []string) []map[string]any {
	out := make([]map[string]any, 0, len(emails))
	for _, e := range emails {
		out = append(out, map[string]any{"emailAddress": map[string]string{"address": e}})
	}
	return out
}

// token returns a cached app-only token or acquires a fresh one.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && c.now().Add(5*time.Minute).Before(c.accessTokenExp) {
		return c.accessToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("scope", graphScope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("graphmail: acquiring token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("graphmail: token endpoint returned %d: %s", resp.StatusCode, string(msg))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("graphmail: decoding token response: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("graphmail: token response had no access_token")
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	c.accessToken = out.AccessToken
	c.accessTokenExp = c.now().Add(ttl)
	return c.accessToken, nil
}

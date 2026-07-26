package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"steamswitch/internal/appclient"
)

// MailboxConfig points at a programmable-mailbox service the user runs.
//
// There is deliberately no default BaseURL. Baking one in would make this fork contact a
// host of someone else's choosing by default, which the fork rules forbid outright — and it
// would turn "a mailbox API you happen to run" into a dependency.
type MailboxConfig struct {
	BaseURL   string
	Token     string
	MailboxID string
}

type mailboxSource struct {
	cfg MailboxConfig
}

// NewMailbox returns a CodeSource backed by a mailbox API.
func NewMailbox(cfg MailboxConfig) (CodeSource, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.MailboxID = strings.TrimSpace(cfg.MailboxID)
	if cfg.BaseURL == "" || cfg.MailboxID == "" {
		return nil, ErrBadConfig
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, ErrBadConfig
	}
	// Refuse plain HTTP: the request carries a bearer token, and the response carries a
	// Guard code. Neither belongs on the wire in the clear, and a config typo is not a good
	// enough reason to allow it.
	if !strings.EqualFold(u.Scheme, "https") && !isLoopback(u.Hostname()) {
		return nil, ErrBadConfig
	}
	return &mailboxSource{cfg: cfg}, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// message is the subset of the mailbox contract this needs: when it arrived, who it was
// for, and the code the service already extracted. Everything else in the payload is
// ignored, so a richer service is compatible without changes here.
type message struct {
	ID         string   `json:"id"`
	ReceivedAt string   `json:"receivedAt"`
	To         []string `json:"to"`
	From       string   `json:"from"`
	Code       *string  `json:"code"`
	Body       string   `json:"body"`
}

func (s *mailboxSource) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.BaseURL+path, nil)
	if err != nil {
		return ErrBadConfig
	}
	if s.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	}
	req.Header.Set("Accept", "application/json")

	// appclient.Shared, never http.DefaultClient: offline mode is enforced at the transport
	// layer, and a client constructed here would route around it.
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return ErrConnect
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrAuth
	case resp.StatusCode >= 400:
		return ErrConnect
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return ErrConnect
	}
	return nil
}

func (s *mailboxSource) Probe(ctx context.Context) error {
	var msgs []message
	return s.do(ctx, http.MethodGet, s.listPath(false), &msgs)
}

func (s *mailboxSource) listPath(pendingOnly bool) string {
	p := "/v1/mailboxes/" + url.PathEscape(s.cfg.MailboxID) + "/messages"
	if pendingOnly {
		p += "?pending=true"
	}
	return p
}

func (s *mailboxSource) FetchCode(ctx context.Context, notBefore time.Time) (string, error) {
	return pollUntil(ctx, func(ctx context.Context) (string, error) {
		var msgs []message
		if err := s.do(ctx, http.MethodGet, s.listPath(true), &msgs); err != nil {
			return "", err
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			if m.From != "" && !FromSteam(m.From) {
				continue
			}
			if !FreshEnough(parseTime(m.ReceivedAt), notBefore) {
				continue
			}
			code := ""
			if m.Code != nil {
				code = strings.TrimSpace(*m.Code)
			}
			if code == "" {
				// The service did not extract one, so fall back to reading the body
				// ourselves rather than treating it as a miss.
				if c, ok := ExtractCode(m.Body); ok {
					code = c
				}
			}
			if code == "" {
				continue
			}
			// Consume is best-effort: having the code in hand is the success condition,
			// and failing the whole fetch because the bookkeeping call failed would be a
			// bad trade for the user standing at a Guard prompt.
			s.consume(ctx, m.ID)
			return code, nil
		}
		return "", ErrNoCode
	})
}

func (s *mailboxSource) consume(ctx context.Context, id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	path := fmt.Sprintf("/v1/mailboxes/%s/messages/%s/consume",
		url.PathEscape(s.cfg.MailboxID), url.PathEscape(id))
	_ = s.do(ctx, http.MethodPost, path, nil)
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, time.RFC1123Z} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

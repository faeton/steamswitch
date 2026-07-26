// Package mail retrieves a Steam Guard login code from wherever the user's mail lives.
//
// There is exactly one operation: "get me one code that is newer than this instant". It is
// not a mail client, it does not keep a background loop, and it never marks anything read.
package mail

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// CodeSource is the seam between "where the mail is" and everything above it. Two
// implementations ship: IMAP and a mailbox API the user runs.
type CodeSource interface {
	// FetchCode blocks until a Steam Guard code newer than notBefore arrives, or ctx ends.
	FetchCode(ctx context.Context, notBefore time.Time) (string, error)
	// Probe validates configuration without consuming anything.
	Probe(ctx context.Context) error
}

// Errors are i18n keys.
var (
	ErrNoSource   = errors.New("Toast_Vault_NoEmailSource")
	ErrNoCode     = errors.New("Toast_Vault_NoCodeFound")
	ErrBadConfig  = errors.New("Toast_Vault_EmailConfigIncomplete")
	ErrConnect    = errors.New("Toast_Vault_EmailConnectFailed")
	ErrAuth       = errors.New("Toast_Vault_EmailAuthFailed")
	ErrOfflineOrN = errors.New("Toast_Vault_OfflineMode")
)

// StaleSkew is how far before notBefore a message may be dated and still be accepted.
//
// Steam Guard codes are single use. Without this window the poller happily returns the code
// from the *previous* login, which Steam then rejects — and the user is left retyping a
// code that was never going to work. The skew exists only to absorb clock difference
// between this machine and the mail server, so it is small and deliberate.
const StaleSkew = 60 * time.Second

// PollInterval is how often a source re-checks. Fast enough to feel immediate, slow enough
// not to look like a hammering client to a mail host.
const PollInterval = 5 * time.Second

// SenderDomain is the only sender a login code is accepted from. Matching on the domain
// rather than a full address tolerates Valve's several noreply@ mailboxes without accepting
// a lookalike from anywhere else.
const SenderDomain = "steampowered.com"

// Steam's login mail contains the code on its own line under a "Login Code" label. Anchoring
// on the label and bounding the window is what stops the regex matching an unrelated
// 5-character run elsewhere in the message — the body also contains order numbers, support
// links and the recipient's own account name.
//
// The alphabet is Steam's: uppercase letters and digits, no lowercase.
// The case-insensitive flag is scoped to the label only. Applying it to the whole pattern
// makes `[A-Z0-9]{5}` match lowercase, and the first thing it finds in Steam's HTML mail is
// the word `class` from `<div class="code">` — a plausible-looking five characters that is
// not a code at all.
var (
	loginCodeRe    = regexp.MustCompile(`(?s)(?i:login\s*code).{0,120}?\b([A-Z0-9]{5})\b`)
	bareCodeRe     = regexp.MustCompile(`(?m)^\s*([A-Z0-9]{5})\s*$`)
	recoveryCodeRe = regexp.MustCompile(`(?s)(?i:(?:recovery|verification)\s*code).{0,120}?\b([A-Z0-9]{5})\b`)
)

// ExtractCode pulls a Guard code out of a message body.
//
// Order matters. The labelled form is tried first because it is unambiguous; the bare
// line-on-its-own form is the fallback for the plain-text part of the same mail, where the
// label and the code are separated by more markup than the bounded window allows.
func ExtractCode(body string) (string, bool) {
	for _, re := range []*regexp.Regexp{loginCodeRe, recoveryCodeRe} {
		if m := re.FindStringSubmatch(body); len(m) == 2 {
			return m[1], true
		}
	}
	// The bare form only counts if the message looks like a Steam login mail at all —
	// otherwise any 5-character line anywhere would qualify.
	if strings.Contains(strings.ToLower(body), "steam") {
		if m := bareCodeRe.FindStringSubmatch(body); len(m) == 2 {
			return m[1], true
		}
	}
	return "", false
}

// FreshEnough reports whether a message dated at sent may be used for a login started at
// notBefore.
func FreshEnough(sent, notBefore time.Time) bool {
	if sent.IsZero() {
		// A message with no usable date is not evidence of freshness. Accepting it is how
		// a stale code gets handed over.
		return false
	}
	return !sent.Before(notBefore.Add(-StaleSkew))
}

// AddressedTo reports whether a message was sent to the address bound to this account.
//
// Shared mailboxes are common with bought accounts: one inbox receives codes for a dozen
// accounts, and returning the wrong one is worse than returning none, because the user
// cannot tell it is wrong until Steam rejects it.
func AddressedTo(recipients []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return true // no binding recorded; nothing to filter on
	}
	for _, r := range recipients {
		if strings.Contains(strings.ToLower(r), want) {
			return true
		}
	}
	return false
}

// FromSteam reports whether a sender address is Valve's.
//
// Matching is on the parsed domain, not on a substring of the header. A substring test
// accepts `noreply@steampowered.com.attacker.test`, which is the exact shape a phishing
// sender takes — and this function's answer is what decides whether a message is trusted
// enough to read a code out of.
func FromSteam(from string) bool {
	for _, d := range senderDomains(from) {
		if d == SenderDomain || strings.HasSuffix(d, "."+SenderDomain) {
			return true
		}
	}
	return false
}

// senderDomains pulls the domain out of every address-looking token in a header value.
// Headers arrive as anything from a bare address to `Name <addr>, Other <addr>`, so this
// tolerates the surrounding syntax rather than trying to fully parse it.
func senderDomains(header string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(strings.ToLower(header), func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == '<' || r == '>' || r == '"' || r == '\t'
	}) {
		at := strings.LastIndex(field, "@")
		if at < 0 || at == len(field)-1 {
			continue
		}
		out = append(out, strings.Trim(field[at+1:], ".)"))
	}
	return out
}

// pollUntil runs check on an interval until it yields a code, ctx ends, or check reports a
// terminal error. Shared by both sources so the cancellation and deadline behaviour cannot
// drift between them.
func pollUntil(ctx context.Context, check func(context.Context) (string, error)) (string, error) {
	// One immediate attempt: the mail is very often already there, and a mandatory
	// first-interval wait is the difference between "instant" and "sluggish".
	if code, err := check(ctx); err != nil {
		if !errors.Is(err, ErrNoCode) {
			return "", err
		}
	} else if code != "" {
		return code, nil
	}

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			code, err := check(ctx)
			if err != nil {
				if errors.Is(err, ErrNoCode) {
					continue
				}
				return "", err
			}
			if code != "" {
				return code, nil
			}
		}
	}
}

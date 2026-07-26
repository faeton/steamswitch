package logsanitize

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type secretReplacement struct {
	secret      string
	replacement string
}

// Redact replaces account identifiers in text with stable aliases (best-effort).
//
// Two passes, and they work differently on purpose:
//
//   - Email addresses are matched by shape. The vault's addresses are inside an encrypted
//     blob that this package cannot read and must not try to — it runs on the crash path,
//     where the vault is often locked and there is no master key to derive. Anything that
//     looks like an address is therefore aliased whether or not it is one we know about,
//     which is also what catches addresses that reached a log from somewhere else entirely.
//   - Account names and IDs are matched by value, against the identifiers actually on disk.
func Redact(text string) string {
	out := redactEmails(text)
	reps := collectReplacements()
	for _, r := range reps {
		out = replaceCI(out, r.secret, r.replacement)
	}
	return out
}

// Conservative on purpose: a local part, an @, and a domain that ends in a real alphabetic
// TLD. Version strings like `wails/v3@v3.0.0-alpha2.117` in a stack trace have no such
// ending and are left alone.
var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// redactEmails replaces each distinct address with emailN@redacted, numbered in order of
// first appearance so the same address reads the same throughout one report — otherwise a
// log that shows a code arriving at one inbox and a login failing for another becomes
// impossible to follow.
//
// The domain goes too. For bought accounts it is often a single provider's vanity domain,
// which identifies where the accounts came from as surely as the local part identifies who.
func redactEmails(text string) string {
	if !strings.Contains(text, "@") {
		return text
	}
	seen := map[string]string{}
	return emailRe.ReplaceAllStringFunc(text, func(addr string) string {
		key := strings.ToLower(addr)
		if alias, ok := seen[key]; ok {
			return alias
		}
		alias := fmt.Sprintf("email%d@redacted", len(seen)+1)
		seen[key] = alias
		return alias
	})
}

func collectReplacements() []secretReplacement {
	accounts := collectAccountIdentifiers()
	if len(accounts) == 0 {
		return nil
	}
	var reps []secretReplacement
	seen := map[string]struct{}{}
	for i, ids := range accounts {
		base := fmt.Sprintf("account%d", i+1)
		for _, secret := range ids {
			secret = strings.TrimSpace(secret)
			if secret == "" {
				continue
			}
			key := strings.ToLower(secret)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			reps = append(reps, secretReplacement{
				secret:      secret,
				replacement: aliasForAccount(base, secret),
			})
		}
	}
	sort.Slice(reps, func(i, j int) bool {
		return len(reps[i].secret) > len(reps[j].secret)
	})
	return reps
}

func aliasForAccount(base, original string) string {
	i := len(original)
	for i > 0 {
		c := original[i-1]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			break
		}
		i--
	}
	return base + original[i:]
}

func replaceCI(s, old, new string) string {
	if old == "" || s == "" {
		return s
	}
	lower := strings.ToLower(s)
	oldLower := strings.ToLower(old)
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for {
		j := strings.Index(lower[i:], oldLower)
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		j += i
		b.WriteString(s[i:j])
		b.WriteString(new)
		i = j + len(old)
	}
	return b.String()
}

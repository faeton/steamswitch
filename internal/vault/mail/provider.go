package mail

import (
	"context"
	"net"
	"strings"
)

// How a mail provider lets an app sign in over IMAP.
const (
	// AccessPassword is every provider not listed below: the mailbox's own password works,
	// which is how firstmail.ltd, notletters.com and most sold mailboxes behave.
	AccessPassword = "password"
	// AccessAppPassword means the normal password is refused and a generated one is needed.
	AccessAppPassword = "app-password"
	// AccessNoIMAP means a password sign-in over IMAP is not offered at all.
	AccessNoIMAP = "no-imap"
)

// Provider describes a mail service whose rules differ from "IMAP with your password".
type Provider struct {
	Name   string
	Host   string
	Access string
}

var knownProviders = func() map[string]Provider {
	out := map[string]Provider{}
	add := func(p Provider, domains ...string) {
		for _, d := range domains {
			out[d] = p
		}
	}
	add(Provider{"Gmail", "imap.gmail.com", AccessAppPassword}, "gmail.com", "googlemail.com")
	add(Provider{"Yahoo", "imap.mail.yahoo.com", AccessAppPassword}, "yahoo.com", "ymail.com", "rocketmail.com")
	add(Provider{"iCloud", "imap.mail.me.com", AccessAppPassword}, "icloud.com", "me.com", "mac.com")
	add(Provider{"AOL", "imap.aol.com", AccessAppPassword}, "aol.com")
	add(Provider{"Yandex", "imap.yandex.com", AccessAppPassword}, "yandex.ru", "yandex.com", "ya.ru")
	add(Provider{"Mail.ru", "imap.mail.ru", AccessAppPassword}, "mail.ru", "inbox.ru", "list.ru", "bk.ru")
	add(Provider{Name: "Outlook", Access: AccessNoIMAP},
		"outlook.com", "hotmail.com", "live.com", "msn.com",
		"outlook.de", "outlook.fr", "outlook.es", "hotmail.co.uk", "hotmail.de", "hotmail.fr",
		"hotmail.it", "hotmail.es", "live.co.uk", "live.de", "live.fr")
	add(Provider{Name: "Proton Mail", Access: AccessNoIMAP}, "proton.me", "protonmail.com", "pm.me")
	return out
}()

// KnownProvider looks an address up by its domain.
func KnownProvider(address string) (Provider, bool) {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return Provider{}, false
	}
	p, ok := knownProviders[strings.ToLower(strings.TrimSpace(address[at+1:]))]
	return p, ok
}

// Detection is what can be learned about an address without sending a password anywhere.
type Detection struct {
	Found    bool
	Host     string
	Port     int
	Provider string
	Access   string
	// Foreign means the server is not under the address's own domain. That is normal for a
	// vanity domain hosted elsewhere, but the name came from an unverified certificate or from
	// DNS, and strict TLS later only proves the server is that name, not that it belongs to
	// this address. So the UI asks the user to recognise it before a password goes there.
	Foreign bool
}

// Detect finds the IMAP server for an address. It sends no credentials: a known provider is
// answered from the table, and anything else is identified by its greeting and certificate.
func Detect(ctx context.Context, address string) Detection {
	if p, ok := KnownProvider(address); ok {
		return Detection{Found: p.Host != "", Host: p.Host, Port: 993, Provider: p.Name, Access: p.Access}
	}
	out := Detection{Port: 993, Access: AccessPassword}
	candidates := AutoconfigCandidates(address)
	if len(candidates) == 0 {
		return out
	}
	domain := address[strings.LastIndex(address, "@")+1:]
	candidates = append(candidates, mxCandidates(ctx, domain, net.DefaultResolver.LookupMX)...)

	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if ctx.Err() != nil {
			return out
		}
		if host, ok := identifyIMAPHost(candidate); ok {
			out.Found, out.Host = true, host
			out.Foreign = !hostUnderDomain(host, domain)
			return out
		}
	}
	return out
}

func hostUnderDomain(host, domain string) bool {
	host, domain = strings.ToLower(host), strings.ToLower(strings.TrimSpace(domain))
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// IsLocalHost reports a server on this machine, such as Proton Bridge. A provider's rules about
// its own servers say nothing about one of these.
func IsLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// maxMXHosts bounds the extra probing a domain's mail records can cause.
const maxMXHosts = 2

// mxCandidates turns a domain's mail records into hosts worth trying, for a vanity domain whose
// own name has no mail host: an MX of mx.firstmail.ltd suggests imap.firstmail.ltd.
func mxCandidates(ctx context.Context, domain string, lookup func(context.Context, string) ([]*net.MX, error)) []string {
	records, err := lookup(ctx, strings.ToLower(strings.TrimSpace(domain)))
	if err != nil {
		return nil
	}
	var out []string
	for i, r := range records {
		if i == maxMXHosts {
			break
		}
		host := strings.ToLower(strings.TrimSuffix(r.Host, "."))
		if host == "" {
			continue
		}
		if dot := strings.Index(host, "."); dot > 0 && strings.Count(host, ".") >= 2 {
			out = append(out, "imap."+host[dot+1:])
		}
		out = append(out, host)
	}
	return out
}

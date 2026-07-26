package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	// Registers the charset decoders go-message needs for non-UTF-8 mail. Steam's own mail
	// is UTF-8, but the surrounding inbox is not necessarily, and a charset it cannot
	// decode aborts the walk over the parts.
	_ "github.com/emersion/go-message/charset"
)

// IMAPConfig is the connection half of an email binding.
type IMAPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	UseTLS   bool

	// Address is the mailbox the code must be addressed to. Shared inboxes are common with
	// bought accounts, so this is a filter, not a label.
	Address string

	// PurgeConsumed deletes a Guard code from the mailbox once it has been read. It is opt-in
	// because deleting mail is destructive: it is meant for a throwaway inbox dedicated to a
	// single account's codes (the common shape for a bought account), never a personal one
	// where the Guard mail is history the user may want. When set, a successful fetch marks
	// the message \Deleted and expunges it — keeping a code-only mailbox from silting up and
	// removing any chance a future poll mistakes an old code for a new one.
	PurgeConsumed bool
}

// dialTimeout bounds the TCP+TLS handshake. Separate from the poll budget: a host that will
// never answer should fail in seconds, not tie up the whole 90.
const dialTimeout = 20 * time.Second

// ioTimeout bounds each individual IMAP command.
const ioTimeout = 30 * time.Second

type imapSource struct{ cfg IMAPConfig }

// NewIMAP returns a CodeSource backed by a real mailbox.
func NewIMAP(cfg IMAPConfig) (CodeSource, error) {
	if strings.TrimSpace(cfg.Host) == "" || strings.TrimSpace(cfg.User) == "" || cfg.Password == "" {
		return nil, ErrBadConfig
	}
	if cfg.Port == 0 {
		cfg.Port = 993
		cfg.UseTLS = true
	}
	return &imapSource{cfg: cfg}, nil
}

// connect dials, authenticates and selects INBOX.
//
// The dial is ours rather than the library's on purpose. go-imap's Client.Timeout covers
// commands, but its own Dial helpers only apply a connect timeout when they are handed a
// *net.Dialer — with anything else (a SOCKS dialer, once egress lands) the timeout silently
// does nothing, and a black-holed host would hang a switch with no way out but killing the
// app. Dialing here means that seam is already the right shape.
func (s *imapSource) connect() (*client.Client, error) { return s.connectSelect(true) }

// connectSelect is connect with control over whether INBOX is opened read-only. The poll uses
// read-write only when it will purge; everything else stays read-only so it can never mark the
// user's mail read (Peek is used regardless, so \Seen is safe either way — this is belt and
// braces for a server that misbehaves).
func (s *imapSource) connectSelect(readonly bool) (*client.Client, error) {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	d := &net.Dialer{Timeout: dialTimeout}

	var conn net.Conn
	var err error
	if s.cfg.UseTLS {
		conn, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return nil, ErrConnect
	}

	c, err := client.New(conn)
	if err != nil {
		_ = conn.Close()
		return nil, ErrConnect
	}
	// Per-command deadline. Set on the client rather than the conn because the client
	// re-arms it before every command; a one-shot conn deadline would expire partway
	// through a long poll and turn a working mailbox into a connection error.
	c.Timeout = ioTimeout
	// On the plaintext port, upgrade with STARTTLS — and refuse to go on without it. Sending
	// LOGIN over an un-upgraded connection puts the password (and then a Guard code) on the wire
	// in the clear; a server that does not offer STARTTLS is not one we will authenticate to.
	if !s.cfg.UseTLS {
		if ok, _ := c.SupportStartTLS(); !ok {
			_ = c.Logout()
			return nil, ErrConnect
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			_ = c.Logout()
			return nil, ErrConnect
		}
	}
	if err := c.Login(s.cfg.User, s.cfg.Password); err != nil {
		_ = c.Logout()
		return nil, ErrAuth
	}
	if _, err := c.Select("INBOX", readonly); err != nil {
		_ = c.Logout()
		return nil, ErrConnect
	}
	return c, nil
}

func (s *imapSource) Probe(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		c, err := s.connect()
		if err != nil {
			done <- err
			return
		}
		done <- c.Logout()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// FetchCode blocks until a fresh Steam Guard code shows up in the mailbox, or ctx ends.
//
// Two provider quirks, both confirmed against a live account (notletters.com), shape it:
//
//   - It opens a fresh connection for every poll. That is load-bearing, not tidiness: this
//     provider freezes a connection's view of the mailbox at SELECT time and never refreshes it —
//     not even on a re-SELECT. A held connection reported the same message count for four solid
//     minutes while a freshly-opened one saw the code six seconds after it was sent. Reconnecting
//     each poll is what makes newly-arrived mail visible at all.
//   - It does not use a server-side SEARCH. The same provider returns nothing for a SINCE or a
//     From-header search; fetching the last few messages by sequence number and filtering in Go
//     is what a broken search engine cannot defeat.
//
// Freshness is judged by the message's own Date header against notBefore — the moment the login
// that triggers the mail was started — with a small skew (see FreshEnough). This provider returns
// a zero IMAP *ENVELOPE* date, but its raw Date header is correct, so the date is read from the
// parsed header, not from ENVELOPE. Anchoring on notBefore rather than on an inbox watermark is
// deliberate: a watermark has to be captured before the mail is sent, an ordering no CodeSource
// caller can be forced to honour (verify.go begins the login and only then fetches), so a code
// landing between "login" and "first poll" would be misclassified as stale. A date window keyed
// on notBefore has no such ordering hazard.
func (s *imapSource) FetchCode(ctx context.Context, notBefore time.Time) (string, error) {
	return pollUntil(ctx, func(context.Context) (string, error) {
		return s.scanOnce(notBefore)
	})
}

// scanOnce is one poll: a fresh connection (see FetchCode for why fresh), a scan of the newest
// messages, and — if PurgeConsumed is set and a code was found — deletion of that message before
// the connection closes.
func (s *imapSource) scanOnce(notBefore time.Time) (string, error) {
	c, err := s.connectSelect(!s.cfg.PurgeConsumed) // read-write only when we will delete
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Logout() }()

	code, uid, seqNum, err := s.scanForCode(c, notBefore)
	if err != nil {
		return "", err
	}
	if s.cfg.PurgeConsumed {
		s.deleteConsumed(c, uid, seqNum) // best-effort: the code is in hand
	}
	return code, nil
}

// deleteConsumed removes a code that was just read, when PurgeConsumed is set. Best-effort by
// contract: the code is already in hand, and failing the fetch over a bookkeeping call would be a
// bad trade for a user standing at a Guard prompt — so every error here is swallowed, and callers
// must not assume the mailbox is left clean.
//
// It deletes by UID when the server gave one, else by sequence number. The trailing EXPUNGE
// removes EVERY message currently flagged \Deleted, not only this one: go-imap 1.2 exposes no UID
// EXPUNGE. That is safe only under PurgeConsumed's documented precondition — a throwaway inbox
// dedicated to one account's codes, where no other client marks mail \Deleted. It must never be
// enabled on a shared or personal mailbox.
func (s *imapSource) deleteConsumed(c *client.Client, uid, seqNum uint32) {
	set := new(imap.SeqSet)
	item := imap.FormatFlagsOp(imap.AddFlags, true) // +FLAGS.SILENT
	flags := []interface{}{imap.DeletedFlag}
	var err error
	switch {
	case uid > 0:
		set.AddNum(uid)
		err = c.UidStore(set, item, flags, nil)
	case seqNum > 0:
		set.AddNum(seqNum)
		err = c.Store(set, item, flags, nil)
	default:
		return
	}
	if err != nil {
		return
	}
	_ = c.Expunge(nil)
}

// maxCandidates bounds how many recent messages are examined per poll. Steam sends one mail per
// login, so the target is almost always among the newest few; the bound keeps a 5-second timer
// from fetching a whole inbox. The residual cost is on a busy *shared* catch-all inbox: if enough
// unrelated mail arrives after the code but before a poll, the code can fall outside this window
// and be missed. A dedicated per-account code mailbox — the intended shape, and what
// PurgeConsumed keeps clean — never hits that.
const maxCandidates = 15

// scanForCode fetches the newest messages by sequence number and returns the freshest Steam Guard
// code among them, with its UID and sequence number so the caller can delete it.
//
// It reads mailbox state from the (freshly connected) client rather than re-selecting: on this
// connection the SELECT already ran in connectSelect, and re-selecting is precisely the operation
// the notletters bug makes a no-op. It does not use a server-side SEARCH — the same provider that
// zeroes ENVELOPE dates returns nothing for a SINCE search; fetching the last few by sequence
// number and filtering in Go is what a broken search engine cannot defeat.
func (s *imapSource) scanForCode(c *client.Client, notBefore time.Time) (code string, uid, seqNum uint32, err error) {
	status := c.Mailbox()
	if status == nil {
		return "", 0, 0, ErrConnect
	}
	if status.Messages == 0 {
		return "", 0, 0, ErrNoCode
	}
	from := uint32(1)
	if status.Messages > maxCandidates {
		from = status.Messages - maxCandidates + 1
	}
	set := new(imap.SeqSet)
	set.AddRange(from, status.Messages)

	section := &imap.BodySectionName{Peek: true} // never marks the user's mail read
	messages := make(chan *imap.Message, maxCandidates)
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- c.Fetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}, messages)
	}()

	var candidates []*imap.Message
	for m := range messages {
		candidates = append(candidates, m)
	}
	if ferr := <-fetchErr; ferr != nil {
		return "", 0, 0, ErrConnect
	}

	// Newest first, so the freshest code wins when several fall inside the skew window. UID is
	// arrival order on a compliant server and a sound proxy on this one; sequence number is the
	// fallback when the server reports no UID.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Uid != candidates[j].Uid {
			return candidates[i].Uid > candidates[j].Uid
		}
		return candidates[i].SeqNum > candidates[j].SeqNum
	})
	for _, m := range candidates {
		if found, ok := s.codeFromMessage(m, section, notBefore); ok {
			return found, m.Uid, m.SeqNum, nil
		}
	}
	return "", 0, 0, ErrNoCode
}

func (s *imapSource) codeFromMessage(m *imap.Message, section *imap.BodySectionName, notBefore time.Time) (string, bool) {
	if m == nil || m.Envelope == nil {
		// FromSteam and AddressedTo below are security gates; without an envelope there is no
		// trustworthy sender or recipient to check, so the message cannot be read for a code.
		return "", false
	}
	if !FromSteam(envelopeFrom(m.Envelope)) {
		return "", false
	}
	if !AddressedTo(envelopeTo(m.Envelope), s.cfg.Address) {
		return "", false
	}
	body := m.GetBody(section)
	if body == nil {
		return "", false
	}
	text, hdrDate, err := readableText(body)
	if err != nil {
		return "", false
	}
	if !fresh(hdrDate, m, notBefore) {
		return "", false
	}
	return ExtractCode(text)
}

// fresh reports whether a message is recent enough to belong to the login started at notBefore.
//
// The date is taken from the message's own Date header (hdrDate) first, because that is the one
// timestamp this provider was seen to report correctly while its IMAP ENVELOPE date came back as
// zero. ENVELOPE date and INTERNALDATE are fallbacks for a message whose header would not parse. A
// message with no usable date at all is rejected by FreshEnough: an unknown date is not evidence
// of freshness, and accepting it is how a stale, single-use code gets handed over.
func fresh(hdrDate time.Time, m *imap.Message, notBefore time.Time) bool {
	sent := hdrDate
	if sent.IsZero() {
		sent = m.Envelope.Date
	}
	if sent.IsZero() {
		sent = m.InternalDate
	}
	return FreshEnough(sent, notBefore)
}

func envelopeFrom(e *imap.Envelope) string {
	var out []string
	for _, a := range e.From {
		if a != nil {
			out = append(out, a.Address())
		}
	}
	for _, a := range e.Sender {
		if a != nil {
			out = append(out, a.Address())
		}
	}
	return strings.Join(out, " ")
}

func envelopeTo(e *imap.Envelope) []string {
	var out []string
	for _, list := range [][]*imap.Address{e.To, e.Cc} {
		for _, a := range list {
			if a != nil {
				out = append(out, a.Address())
			}
		}
	}
	return out
}

// readableText flattens a message to text, walking MIME parts so a code that only appears
// in the HTML alternative is still found. Non-text parts are skipped rather than failing
// the walk — Steam's mail carries images, and one unreadable part must not lose the code.
//
// It also returns the message's Date header, parsed from the MIME header rather than taken from
// the IMAP ENVELOPE — the live provider zeroes the latter but sends the former correctly, and
// this is the timestamp freshness is judged on. A zero time means the date could not be read.
func readableText(r io.Reader) (string, time.Time, error) {
	mr, err := mail.CreateReader(r)
	if err != nil {
		// Not MIME at all: treat the whole thing as text rather than giving up. No parsed header,
		// so no date — the caller falls back to the ENVELOPE/INTERNALDATE.
		rest, rerr := io.ReadAll(r)
		if rerr != nil {
			return "", time.Time{}, rerr
		}
		return string(rest), time.Time{}, nil
	}
	var date time.Time
	if d, derr := mr.Header.Date(); derr == nil {
		date = d
	}
	var sb strings.Builder
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}
		switch p.Header.(type) {
		case *mail.InlineHeader:
			b, err := io.ReadAll(p.Body)
			if err != nil {
				continue
			}
			sb.Write(b)
			sb.WriteString("\n")
		default:
			// Attachment. Never read; a code is not in one, and reading it wastes the poll.
		}
	}
	return sb.String(), date, nil
}

// AutoconfigCandidates returns the hosts worth trying for a domain, in order.
//
// Vanity domains served by someone else's mail host are the single worst piece of onboarding
// friction here: the user knows their address and password and has no idea what the IMAP
// host is called. These three prefixes cover the overwhelming majority.
func AutoconfigCandidates(address string) []string {
	at := strings.LastIndex(address, "@")
	if at < 0 || at == len(address)-1 {
		return nil
	}
	domain := strings.ToLower(strings.TrimSpace(address[at+1:]))
	if domain == "" {
		return nil
	}
	return []string{"imap." + domain, "mail." + domain, "imap.mail." + domain, domain}
}

// Autoconfig finds a working IMAP host for an address.
//
// It identifies the service first and only then tries the credentials, because the two fail
// for different reasons and trying them together makes "wrong password" and "wrong host"
// indistinguishable.
func Autoconfig(ctx context.Context, address, password string) (IMAPConfig, error) {
	for _, candidate := range AutoconfigCandidates(address) {
		if ctx.Err() != nil {
			return IMAPConfig{}, ctx.Err()
		}
		host, ok := identifyIMAPHost(candidate)
		if !ok {
			continue
		}
		cfg := IMAPConfig{Host: host, Port: 993, User: address, Password: password, UseTLS: true, Address: address}
		src, err := NewIMAP(cfg)
		if err != nil {
			continue
		}
		if err := src.Probe(ctx); err == nil {
			return cfg, nil
		} else if errors.Is(err, ErrAuth) {
			// The host is right and the credentials are not. Trying the remaining
			// candidates would only produce connection errors and bury the real answer.
			return IMAPConfig{}, ErrAuth
		}
	}
	return IMAPConfig{}, ErrConnect
}

// identifyIMAPHost checks whether something at host:993 speaks IMAP, and returns the name
// that will verify under strict TLS at login time.
//
// The indirection exists for vanity domains: a mailbox at `zorrodemail.test` is commonly
// served by someone else's host, and the certificate presented is theirs. Connecting to
// `imap.zorrodemail.test` and demanding a certificate for that name fails, even though the
// service is there and working. Reading the certificate's own names and using one of those
// instead is what makes those addresses configurable at all — which is the single worst
// piece of onboarding friction this feature has.
//
// Verification is skipped for the probe only. Nothing is sent: the connection is opened,
// one greeting line is read, the names are taken from the certificate, and it is closed.
// Every real session afterwards verifies strictly against the name this returns.
func identifyIMAPHost(host string) (string, bool) {
	d := &net.Dialer{Timeout: probeTimeout}
	conn, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort(host, "993"), &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // probe only; see the doc comment above
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return "", false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(probeTimeout))

	// An IMAP server greets unprompted. Anything that does not is some other service that
	// happens to be listening on 993.
	line, _ := bufio.NewReader(conn).ReadString('\n')
	upper := strings.ToUpper(line)
	if !strings.Contains(upper, "OK") && !strings.Contains(upper, "IMAP") {
		return "", false
	}

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return host, true
	}
	if name, ok := preferredCertName(certs[0].DNSNames); ok && name != host {
		// Only if it actually resolves — a certificate can name hosts that do not exist.
		if _, err := net.LookupHost(name); err == nil {
			return name, true
		}
	}
	return host, true
}

// probeTimeout bounds one candidate. Four candidates are tried, so this has to stay small
// enough that a domain with no mail service at all fails in seconds.
const probeTimeout = 4 * time.Second

// preferredCertName picks the name from a certificate that an IMAP client should verify
// against: an explicit imap.* entry if there is one, otherwise the imap.* form of a
// wildcard.
func preferredCertName(names []string) (string, bool) {
	for _, n := range names {
		if lower := strings.ToLower(n); strings.HasPrefix(lower, "imap.") {
			return lower, true
		}
	}
	for _, n := range names {
		if lower := strings.ToLower(n); strings.HasPrefix(lower, "*.") {
			return "imap." + strings.TrimPrefix(lower, "*."), true
		}
	}
	return "", false
}

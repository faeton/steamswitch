package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
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
func (s *imapSource) connect() (*client.Client, error) {
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
	// STARTTLS for the plaintext port, so "not 993" does not silently mean "credentials in
	// the clear".
	if !s.cfg.UseTLS {
		if ok, _ := c.SupportStartTLS(); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				_ = c.Logout()
				return nil, ErrConnect
			}
		}
	}
	if err := c.Login(s.cfg.User, s.cfg.Password); err != nil {
		_ = c.Logout()
		return nil, ErrAuth
	}
	if _, err := c.Select("INBOX", true); err != nil { // read-only: never touch \Seen
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

func (s *imapSource) FetchCode(ctx context.Context, notBefore time.Time) (string, error) {
	c, err := s.connect()
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Logout() }()

	return pollUntil(ctx, func(ctx context.Context) (string, error) {
		return s.searchOnce(c, notBefore)
	})
}

// maxCandidates bounds how many recent messages are examined per poll. Steam sends one mail
// per login; anything beyond a handful is another account's code or an unrelated mail, and
// fetching a whole inbox on a 5-second timer is not acceptable behaviour towards the host.
const maxCandidates = 5

func (s *imapSource) searchOnce(c *client.Client, notBefore time.Time) (string, error) {
	crit := imap.NewSearchCriteria()
	// Since is date-granular in IMAP, so a login just after midnight would otherwise search
	// an empty day. Backing off one day costs nothing and the freshness check below is what
	// actually enforces recency.
	crit.Since = notBefore.Add(-24 * time.Hour)
	crit.Header = map[string][]string{"From": {SenderDomain}}

	ids, err := c.Search(crit)
	if err != nil {
		return "", ErrConnect
	}
	if len(ids) == 0 {
		return "", ErrNoCode
	}
	if len(ids) > maxCandidates {
		ids = ids[len(ids)-maxCandidates:]
	}

	set := new(imap.SeqSet)
	set.AddNum(ids...)

	// Peek, so reading a code never marks the user's mail as read. A support ticket that
	// begins "the app is marking my mail read" is entirely avoidable here.
	section := &imap.BodySectionName{Peek: true}
	messages := make(chan *imap.Message, maxCandidates)
	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- c.Fetch(set, []imap.FetchItem{imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}, messages)
	}()

	// Newest first, so the freshest matching code wins even if several qualify.
	var found []*imap.Message
	for m := range messages {
		found = append(found, m)
	}
	if err := <-fetchErr; err != nil {
		return "", ErrConnect
	}

	for i := len(found) - 1; i >= 0; i-- {
		code, ok := s.codeFromMessage(found[i], section, notBefore)
		if ok {
			return code, nil
		}
	}
	return "", ErrNoCode
}

func (s *imapSource) codeFromMessage(m *imap.Message, section *imap.BodySectionName, notBefore time.Time) (string, bool) {
	if m == nil || m.Envelope == nil {
		return "", false
	}
	if !FromSteam(envelopeFrom(m.Envelope)) {
		return "", false
	}
	sent := m.Envelope.Date
	if sent.IsZero() {
		sent = m.InternalDate
	}
	if !FreshEnough(sent, notBefore) {
		return "", false
	}
	if !AddressedTo(envelopeTo(m.Envelope), s.cfg.Address) {
		return "", false
	}
	body := m.GetBody(section)
	if body == nil {
		return "", false
	}
	text, err := readableText(body)
	if err != nil {
		return "", false
	}
	return ExtractCode(text)
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
func readableText(r io.Reader) (string, error) {
	mr, err := mail.CreateReader(r)
	if err != nil {
		// Not MIME at all: treat the whole thing as text rather than giving up.
		rest, rerr := io.ReadAll(r)
		if rerr != nil {
			return "", rerr
		}
		return string(rest), nil
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
	return sb.String(), nil
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

// Autoconfig tries each candidate host until one accepts the credentials, and returns the
// working config. It is a convenience over Probe, not a separate protocol.
func Autoconfig(ctx context.Context, address, password string) (IMAPConfig, error) {
	for _, host := range AutoconfigCandidates(address) {
		cfg := IMAPConfig{Host: host, Port: 993, User: address, Password: password, UseTLS: true, Address: address}
		src, err := NewIMAP(cfg)
		if err != nil {
			continue
		}
		if err := src.Probe(ctx); err == nil {
			return cfg, nil
		}
		if ctx.Err() != nil {
			return IMAPConfig{}, ctx.Err()
		}
	}
	return IMAPConfig{}, fmt.Errorf("%w", ErrConnect)
}

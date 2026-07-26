package vault

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"steamswitch/internal/vault/mail"
	"steamswitch/internal/vault/totp"
)

// Methods a code can arrive by, reported so the UI can say where it came from — "from your
// authenticator" and "from your inbox" have very different implications if the code is
// rejected.
const (
	MethodTOTP    = "totp"
	MethodMailbox = "mailbox"
)

// CodeResult is a Guard code and its provenance. RemainingSeconds is only meaningful for
// TOTP, where the code expires on a fixed schedule; an emailed code has no such clock.
type CodeResult struct {
	Code             string `json:"code"`
	Method           string `json:"method"`
	RemainingSeconds int    `json:"remainingSeconds,omitempty"`
}

// FetchBudget bounds how long a mailbox is polled before giving up. Long enough for a mail
// that is slow to arrive, short enough that the user is not left staring at a spinner.
const FetchBudget = 90 * time.Second

// sourceFor builds the configured CodeSource for an entry, or ErrNoSource if the user has
// not bound one.
func sourceFor(e Entry) (mail.CodeSource, error) {
	switch emailSourceOf(e) {
	case EmailSourceIMAP:
		if e.Email.IMAP == nil {
			return nil, mail.ErrBadConfig
		}
		return mail.NewIMAP(mail.IMAPConfig{
			Host:          e.Email.IMAP.Host,
			Port:          e.Email.IMAP.Port,
			User:          e.Email.IMAP.User,
			Password:      e.Email.IMAP.Password,
			UseTLS:        e.Email.IMAP.UseTLS,
			Address:       e.Email.Address,
			PurgeConsumed: e.Email.IMAP.PurgeConsumed,
		})
	case EmailSourceMailbox:
		if e.Email.Mailbox == nil {
			return nil, mail.ErrBadConfig
		}
		return mail.NewMailbox(mail.MailboxConfig{
			BaseURL:   e.Email.Mailbox.BaseURL,
			Token:     e.Email.Mailbox.Token,
			MailboxID: e.Email.Mailbox.MailboxID,
		})
	default:
		return nil, mail.ErrNoSource
	}
}

// GuardCode walks the ladder for an account: authenticator first because it is instant and
// offline, mailbox second. Manual entry is the third rung and lives in the UI — this
// function returning an error is what surfaces it.
func GuardCode(ctx context.Context, steamID64 string) (CodeResult, error) {
	e, err := entry(steamID64)
	if err != nil {
		return CodeResult{}, err
	}

	if e.SharedSecret != "" {
		code, remaining, err := totp.Now(e.SharedSecret)
		if err == nil {
			return CodeResult{Code: code, Method: MethodTOTP, RemainingSeconds: int(remaining.Seconds())}, nil
		}
		// A seed that will not decode is a data problem the user must fix, but it is not a
		// reason to skip the inbox they also configured.
		slog.Warn("vault: shared secret did not decode, falling through to the mailbox",
			"steamId64", steamID64)
	}

	// A code already in flight from the pre-warm is the whole point of pre-warming: by the
	// time Steam asks, it is usually sitting here.
	if code, ok := takePrewarmed(steamID64); ok {
		return CodeResult{Code: code, Method: MethodMailbox}, nil
	}

	// The pre-warm did not have a code ready. Cancel any still-running pre-warm before fetching
	// directly: leaving it alive races this fetch for the same single-use code, and a late
	// pre-warm result would be stashed for a *later* request that would then hand over a code
	// this login already spent.
	CancelPrewarm(steamID64)

	src, err := sourceFor(e)
	if err != nil {
		return CodeResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, FetchBudget)
	defer cancel()

	code, err := src.FetchCode(ctx, time.Now())
	if err != nil {
		return CodeResult{}, err
	}
	return CodeResult{Code: code, Method: MethodMailbox}, nil
}

// ProbeEmail validates an entry's email binding without consuming anything.
func ProbeEmail(ctx context.Context, steamID64 string) error {
	e, err := entry(steamID64)
	if err != nil {
		return err
	}
	src, err := sourceFor(e)
	if err != nil {
		return err
	}
	return src.Probe(ctx)
}

// --- pre-warm -------------------------------------------------------------------------
//
// A switch starting is the earliest honest signal that a code will be wanted. Fetching from
// that moment rather than from the moment Steam displays its prompt is most of the
// perceived speed of this feature.

type prewarm struct {
	cancel  context.CancelFunc
	code    string
	done    bool
	startAt time.Time
}

var (
	prewarmMu sync.Mutex
	prewarms  = map[string]*prewarm{}
)

// PrewarmLifetime bounds how long a pre-warmed code is considered usable. Steam codes are
// single-use and short-lived; handing over one fetched ten minutes ago is worse than
// fetching a fresh one, because it fails in a way the user cannot diagnose.
const PrewarmLifetime = 3 * time.Minute

// Prewarm starts fetching a Guard code in the background. It is fire-and-forget by design:
// it runs inside the swap gate, and a switch must never be delayed — let alone failed — by
// a mail server.
func Prewarm(steamID64 string) {
	id := normID(steamID64)
	if id == "" {
		return
	}
	e, err := entry(id)
	if err != nil {
		return
	}
	// An authenticator seed needs no warming: the code is computed on demand in
	// microseconds, and pre-computing one only risks handing over an expired window.
	if e.SharedSecret != "" {
		return
	}
	src, err := sourceFor(e)
	if err != nil {
		return
	}

	prewarmMu.Lock()
	if old := prewarms[id]; old != nil && old.cancel != nil {
		old.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), FetchBudget)
	p := &prewarm{cancel: cancel, startAt: time.Now()}
	prewarms[id] = p
	prewarmMu.Unlock()

	notBefore := time.Now()
	go func() {
		defer cancel()
		code, err := src.FetchCode(ctx, notBefore)
		prewarmMu.Lock()
		defer prewarmMu.Unlock()
		// Only publish into the slot we were started for. A second switch replaces the
		// entry, and a late result from the first must not overwrite it.
		if cur := prewarms[id]; cur == p {
			p.done = true
			if err == nil {
				p.code = code
			}
		}
	}()
}

// CancelPrewarm stops an in-flight fetch. Called when the switch that started it fails, so
// a doomed switch does not leave a mail connection running behind it.
func CancelPrewarm(steamID64 string) {
	id := normID(steamID64)
	prewarmMu.Lock()
	defer prewarmMu.Unlock()
	if p := prewarms[id]; p != nil {
		if p.cancel != nil {
			p.cancel()
		}
		delete(prewarms, id)
	}
}

// takePrewarmed consumes a pre-warmed code. Consuming rather than reading is deliberate:
// the code is single-use, so a second caller must go and fetch a fresh one rather than be
// handed the same dead value.
func takePrewarmed(steamID64 string) (string, bool) {
	id := normID(steamID64)
	prewarmMu.Lock()
	defer prewarmMu.Unlock()
	p := prewarms[id]
	if p == nil || !p.done || p.code == "" {
		return "", false
	}
	delete(prewarms, id)
	if time.Since(p.startAt) > PrewarmLifetime {
		return "", false
	}
	return p.code, true
}

// PrewarmPending reports whether a fetch is running for an account, so the status strip can
// narrate "checking inbox…" rather than showing nothing until the code lands.
func PrewarmPending(steamID64 string) bool {
	prewarmMu.Lock()
	defer prewarmMu.Unlock()
	p := prewarms[normID(steamID64)]
	return p != nil && !p.done
}

// resetPrewarmForTest drops all pre-warm state. Tests only.
func resetPrewarmForTest() {
	prewarmMu.Lock()
	defer prewarmMu.Unlock()
	for _, p := range prewarms {
		if p.cancel != nil {
			p.cancel()
		}
	}
	prewarms = map[string]*prewarm{}
}

// autoconfigIMAP probes the usual hosts for an address and returns the one that accepted
// the credentials. It lives here rather than in the service so the mail package stays the
// only importer of the IMAP client.
func autoconfigIMAP(ctx context.Context, address, password string) (string, error) {
	cfg, err := mail.Autoconfig(ctx, address, password)
	if err != nil {
		return "", err
	}
	return cfg.Host, nil
}

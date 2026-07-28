package vault

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/security"
)

// Batch import — the review-then-commit half of A7.
//
// Two rules shape everything here.
//
// **The parsed roster never leaves Go.** A review table needs to show what will happen to each
// row; it does not need the passwords to do that. So the plaintext sits in a session buffer on
// this side of the bindings and the UI receives presence — "the import carries a password",
// "the vault already has one" — never a value. Shipping the payload to the webview to render a
// table would put every credential in the import into a JavaScript heap, a devtools console
// and any crash dump for the sake of drawing ticks in a column.
//
// **One mutate, or nothing.** The vault is a single re-encrypted blob; N separate Put calls
// would be N re-encryptions and, worse, N chances to stop half way and leave the user with an
// import that partly happened and no record of where it stopped. Every row is applied inside
// one mutate, so a failure leaves the prior blob exactly as it was.

// Conflict modes for a row whose SteamID64 is already in the vault. Frozen: they cross the
// bindings as strings.
const (
	// RowModeDefault is fill-empty-only — the import supplies fields the vault does not have
	// and never replaces one it does. The default because the opposite silently destroys a
	// working credential when the roster is older than the vault, and a batch is exactly the
	// situation where nobody is reading each row closely.
	RowModeDefault = "default"
	// RowModeOverwrite lets every field the import carries win. Per-row, opt-in.
	RowModeOverwrite = "overwrite"
	// RowModeSkip leaves the entry untouched.
	RowModeSkip = "skip"
)

// Row actions, as shown in the review table.
const (
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionSkip    = "skip"
	ActionInvalid = "invalid"
)

// Per-field outcomes.
const (
	// OutcomeFill — the vault has nothing here and the import supplies it.
	OutcomeFill = "fill"
	// OutcomeKeep — both have a value and the vault's is kept (fill-empty-only).
	OutcomeKeep = "keep"
	// OutcomeOverwrite — both have a value and the import's replaces it.
	OutcomeOverwrite = "overwrite"
	// OutcomeNone — the import carries nothing for this field.
	OutcomeNone = "none"
)

// Review-table field keys.
//
// A separate vocabulary from the Field* constants Reveal addresses, and deliberately so: those
// name struct fields for a caller asking for one secret, these name what a person reading a
// table understands. Hence `totp` rather than `sharedSecret`, and one `email` standing for the
// whole binding, because that is the unit the import keeps or replaces.
const (
	ImportFieldAccountName = "accountName"
	ImportFieldLabel       = "label"
	ImportFieldPassword    = "password"
	ImportFieldTOTP        = "totp"
	ImportFieldEmail       = "email"
	ImportFieldSource      = "source"
	ImportFieldNote        = "note"
)

var (
	ErrNoImportSession   = errors.New("Toast_Roster_SessionExpired")
	ErrTooManyImports    = errors.New("Toast_Roster_TooManySessions")
	ErrImportNothingToDo = errors.New("Toast_Roster_NothingToDo")
)

// ImportFieldPlan is one cell of the review table. It says what will happen, never what the
// value is.
type ImportFieldPlan struct {
	Field string `json:"field"`
	// Incoming — the roster carries a value for this field.
	Incoming bool `json:"incoming"`
	// Existing — the vault already holds one.
	Existing bool `json:"existing"`
	Outcome  string `json:"outcome"`
}

// ImportRow is one reviewable row.
type ImportRow struct {
	Index       int    `json:"index"`
	SteamID64   string `json:"steamId64"`
	AccountName string `json:"accountName,omitempty"`
	Label       string `json:"label,omitempty"`
	// Action under the row's current mode: create, update, skip or invalid.
	Action string `json:"action"`
	// Invalid is the plain-language reason the row cannot be imported, "" when it can.
	Invalid string `json:"invalid,omitempty"`
	// DuplicateOfIndex is the earlier row in this same payload that already claimed this
	// SteamID64, or -1. A roster that lists one account twice would otherwise import the
	// second copy over the first with no sign that it happened.
	DuplicateOfIndex int               `json:"duplicateOfIndex"`
	Fields           []ImportFieldPlan `json:"fields"`
}

// ImportPlan is the whole review payload handed to the UI.
type ImportPlan struct {
	SessionID string      `json:"sessionId"`
	Rows      []ImportRow `json:"rows"`
	// TotalRows is what the source contained, so the UI can state coverage rather than
	// implying the rows it drew are all there were.
	TotalRows int `json:"totalRows"`
	// PlaintextPath is set only for the legacy plaintext-file intake, so the confirm step can
	// name the file it will try — and only try — to remove.
	PlaintextPath string `json:"plaintextPath,omitempty"`
	Note          string `json:"note,omitempty"`
}

// RowDecision is the UI's per-row override.
type RowDecision struct {
	Index int    `json:"index"`
	Mode  string `json:"mode"`
}

// ImportSummary accounts for every input row. The brief requires that: a summary that adds up
// to fewer rows than the file had is how a silently dropped account becomes a mystery.
type ImportSummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Invalid int `json:"invalid"`
	Total   int `json:"total"`
	// SkippedReasons lists the invalid rows so the user can fix the source, keyed by index.
	Rejected []RejectedRow `json:"rejected,omitempty"`
	// PlaintextRemoved reports the best-effort removal of a plaintext source file. False here
	// is normal and must never be presented as failure — see removePlaintextBestEffort.
	PlaintextRemoved bool `json:"plaintextRemoved"`
	// PlaintextPath is echoed back so the summary can tell the user which file to go and
	// check for themselves.
	PlaintextPath string `json:"plaintextPath,omitempty"`
}

type RejectedRow struct {
	Index     int    `json:"index"`
	SteamID64 string `json:"steamId64,omitempty"`
	Reason    string `json:"reason"`
}

// importSession is one parsed roster held between review and commit.
type importSession struct {
	id            string
	created       time.Time
	payload       RosterPayload
	plaintextPath string
}

// importSessionTTL bounds how long a buffer full of credentials sits in memory after the user
// has stopped looking at it. Fifteen minutes is long enough to read a 50-row table and short
// enough that an abandoned import does not survive the afternoon.
const importSessionTTL = 15 * time.Minute

// maxImportSessions stops a UI bug — or a user clicking Import repeatedly — from accumulating
// plaintext rosters in memory.
const maxImportSessions = 3

var importSessions = struct {
	sync.Mutex
	m map[string]*importSession
}{m: map[string]*importSession{}}

// DropImportSessions discards every buffered roster. Called when the app locks: the app-lock
// is what gates the vault, and a decrypted roster surviving a lock would route around it.
func DropImportSessions() {
	importSessions.Lock()
	defer importSessions.Unlock()
	importSessions.m = map[string]*importSession{}
}

func pruneImportSessionsLocked(now time.Time) {
	for id, s := range importSessions.m {
		if now.Sub(s.created) > importSessionTTL {
			delete(importSessions.m, id)
		}
	}
}

func putImportSession(s *importSession) error {
	importSessions.Lock()
	defer importSessions.Unlock()
	pruneImportSessionsLocked(time.Now())
	if len(importSessions.m) >= maxImportSessions {
		return ErrTooManyImports
	}
	importSessions.m[s.id] = s
	return nil
}

func takeImportSession(id string) (*importSession, bool) {
	importSessions.Lock()
	defer importSessions.Unlock()
	pruneImportSessionsLocked(time.Now())
	s, ok := importSessions.m[strings.TrimSpace(id)]
	return s, ok
}

// DiscardImport drops one buffered roster. The UI calls it on cancel, so an abandoned review
// does not wait out the TTL holding credentials.
func DiscardImport(sessionID string) {
	importSessions.Lock()
	defer importSessions.Unlock()
	delete(importSessions.m, strings.TrimSpace(sessionID))
}

// PlanImport buffers a parsed roster and returns the reviewable plan.
//
// plaintextPath is the legacy intake's source file, "" for every other route.
func PlanImport(p RosterPayload, plaintextPath string) (ImportPlan, error) {
	if len(p.Accounts) == 0 {
		return ImportPlan{}, ErrRosterEmpty
	}
	id, err := randomToken(bundleIDSize)
	if err != nil {
		return ImportPlan{}, err
	}
	session := &importSession{
		id:            id,
		created:       time.Now(),
		payload:       p,
		plaintextPath: strings.TrimSpace(plaintextPath),
	}
	if err := putImportSession(session); err != nil {
		return ImportPlan{}, err
	}

	rows, err := planRows(p, nil)
	if err != nil {
		DiscardImport(id)
		return ImportPlan{}, err
	}
	return ImportPlan{
		SessionID:     id,
		Rows:          rows,
		TotalRows:     len(p.Accounts),
		PlaintextPath: session.plaintextPath,
		Note:          p.Note,
	}, nil
}

// RepriceImport recomputes the plan under a new set of per-row decisions, so the review table
// can show the consequence of flipping a row to overwrite before anything is committed.
func RepriceImport(sessionID string, decisions []RowDecision) (ImportPlan, error) {
	s, ok := takeImportSession(sessionID)
	if !ok {
		return ImportPlan{}, ErrNoImportSession
	}
	rows, err := planRows(s.payload, decisionMap(decisions))
	if err != nil {
		return ImportPlan{}, err
	}
	return ImportPlan{
		SessionID:     s.id,
		Rows:          rows,
		TotalRows:     len(s.payload.Accounts),
		PlaintextPath: s.plaintextPath,
		Note:          s.payload.Note,
	}, nil
}

func decisionMap(decisions []RowDecision) map[int]string {
	if len(decisions) == 0 {
		return nil
	}
	out := make(map[int]string, len(decisions))
	for _, d := range decisions {
		switch d.Mode {
		case RowModeOverwrite, RowModeSkip, RowModeDefault:
			out[d.Index] = d.Mode
		}
	}
	return out
}

func modeFor(modes map[int]string, index int) string {
	if modes == nil {
		return RowModeDefault
	}
	if m, ok := modes[index]; ok {
		return m
	}
	return RowModeDefault
}

// planRows builds the review model. Reads the vault once, outside any write lock.
func planRows(p RosterPayload, modes map[int]string) ([]ImportRow, error) {
	existing, err := List()
	if err != nil {
		return nil, err
	}
	// Summary carries presence flags rather than secrets, which is exactly what the plan
	// needs — so the planner never has to touch a stored credential to decide what to say.
	bySteamID := make(map[string]Summary, len(existing))
	for _, e := range existing {
		bySteamID[e.SteamID64] = e
	}

	seen := make(map[string]int, len(p.Accounts))
	rows := make([]ImportRow, 0, len(p.Accounts))
	for i, rec := range p.Accounts {
		row := ImportRow{
			Index:            i,
			SteamID64:        rec.SteamID64,
			AccountName:      rec.AccountName,
			Label:            rec.Label,
			DuplicateOfIndex: -1,
		}
		if reason := validateRosterRecord(rec); reason != "" {
			row.Action = ActionInvalid
			row.Invalid = reason
			rows = append(rows, row)
			continue
		}
		if first, dup := seen[rec.SteamID64]; dup {
			row.Action = ActionInvalid
			row.DuplicateOfIndex = first
			row.Invalid = fmt.Sprintf("already listed in row %d", first+1)
			rows = append(rows, row)
			continue
		}
		seen[rec.SteamID64] = i

		prior, isUpdate := bySteamID[rec.SteamID64]
		mode := modeFor(modes, i)
		switch {
		case mode == RowModeSkip:
			row.Action = ActionSkip
		case isUpdate:
			row.Action = ActionUpdate
		default:
			row.Action = ActionCreate
		}
		row.Fields = planFields(rec, prior, isUpdate, mode)
		rows = append(rows, row)
	}
	return rows, nil
}

func planFields(rec RosterRecord, prior Summary, isUpdate bool, mode string) []ImportFieldPlan {
	incomingEmail := rec.Email != nil && (rec.Email.Address != "" || rec.Email.IMAP != nil || rec.Email.Mailbox != nil)
	existingEmail := isUpdate && prior.EmailSource != "" && prior.EmailSource != EmailSourceNone

	specs := []struct {
		field    string
		incoming bool
		existing bool
	}{
		{ImportFieldAccountName, rec.AccountName != "", isUpdate && prior.AccountName != ""},
		{ImportFieldLabel, rec.Label != "", isUpdate && prior.Label != ""},
		{ImportFieldPassword, rec.Password != "", isUpdate && prior.HasPassword},
		{ImportFieldTOTP, rec.SharedSecret != "", isUpdate && prior.HasSharedSecret},
		{ImportFieldEmail, incomingEmail, existingEmail},
		{ImportFieldSource, rec.Source != "", isUpdate && prior.Source != ""},
		{ImportFieldNote, rec.SecretNote != "", isUpdate && prior.SecretNote != ""},
	}

	out := make([]ImportFieldPlan, 0, len(specs))
	for _, s := range specs {
		plan := ImportFieldPlan{Field: s.field, Incoming: s.incoming, Existing: s.existing}
		switch {
		case mode == RowModeSkip || !s.incoming:
			plan.Outcome = OutcomeNone
		case !s.existing:
			plan.Outcome = OutcomeFill
		case mode == RowModeOverwrite:
			plan.Outcome = OutcomeOverwrite
		default:
			plan.Outcome = OutcomeKeep
		}
		out = append(out, plan)
	}
	return out
}

// CommitImport applies the buffered roster in a single vault write.
func CommitImport(sessionID string, decisions []RowDecision) (ImportSummary, error) {
	s, ok := takeImportSession(sessionID)
	if !ok {
		return ImportSummary{}, ErrNoImportSession
	}
	modes := decisionMap(decisions)
	rows, err := planRows(s.payload, modes)
	if err != nil {
		return ImportSummary{}, err
	}

	summary := ImportSummary{Total: len(s.payload.Accounts), PlaintextPath: s.plaintextPath}
	type work struct {
		rec    RosterRecord
		mode   string
		update bool
	}
	var todo []work
	for _, row := range rows {
		switch row.Action {
		case ActionInvalid:
			summary.Invalid++
			summary.Rejected = append(summary.Rejected, RejectedRow{
				Index: row.Index, SteamID64: row.SteamID64, Reason: row.Invalid,
			})
		case ActionSkip:
			summary.Skipped++
		case ActionCreate:
			summary.Created++
			todo = append(todo, work{rec: s.payload.Accounts[row.Index], mode: modeFor(modes, row.Index)})
		case ActionUpdate:
			summary.Updated++
			todo = append(todo, work{rec: s.payload.Accounts[row.Index], mode: modeFor(modes, row.Index), update: true})
		}
	}
	if len(todo) == 0 {
		// Nothing would be written. Saying so beats a success toast for an import that did
		// nothing, which reads as "it worked" and sends the user looking for accounts that
		// were never added.
		DiscardImport(sessionID)
		if summary.Invalid == summary.Total && summary.Total > 0 {
			return summary, ErrRosterNoRecords
		}
		return summary, ErrImportNothingToDo
	}

	now := time.Now().UTC().Format(time.RFC3339)
	err = mutate(func(doc *Doc) error {
		for _, w := range todo {
			id := normID(w.rec.SteamID64)
			e, exists := doc.Entries[id]
			if !exists {
				e = Entry{
					SteamID64: id,
					Email:     EmailBinding{Source: EmailSourceNone},
					// An imported account has never signed in on this machine, so it cannot
					// be on the switcher — that grid is Steam's own loginusers.vdf, not
					// something this app can add a row to. Standalone is the honest state
					// until a real sign-in happens here.
					Standalone: true,
				}
			}
			applyRosterRecord(&e, w.rec, w.mode == RowModeOverwrite)
			e.SteamID64 = id
			e.UpdatedAt = now
			doc.Entries[id] = e
		}
		return nil
	})
	if err != nil {
		// The vault is untouched — mutate saves only after fn returns nil, and it works on a
		// deep copy. The session stays buffered so the user can retry without re-parsing.
		return ImportSummary{}, err
	}

	DiscardImport(sessionID)
	if s.plaintextPath != "" {
		summary.PlaintextRemoved = removePlaintextBestEffort(s.plaintextPath)
	}
	actionlog.Record("vault.roster.import", "", fmt.Sprintf("created=%d updated=%d skipped=%d invalid=%d",
		summary.Created, summary.Updated, summary.Skipped, summary.Invalid), nil)
	return summary, nil
}

// applyRosterRecord merges one record into an entry.
//
// Written against Entry directly rather than routed through Draft/applyDraft because the two
// want opposite things: a Draft's nil means "leave alone" and a set value always wins, which
// is the editor's semantics. Fill-empty-only is a *conditional* set, and expressing it as a
// Draft would mean the caller deciding field by field whether to include a pointer — the same
// logic, further from the rule it implements.
func applyRosterRecord(e *Entry, rec RosterRecord, overwrite bool) {
	set := func(dst *string, incoming string) {
		if incoming == "" {
			return
		}
		if *dst == "" || overwrite {
			*dst = incoming
		}
	}
	set(&e.AccountName, rec.AccountName)
	set(&e.Label, rec.Label)
	set(&e.Password, rec.Password)
	set(&e.SharedSecret, rec.SharedSecret)
	set(&e.IdentitySecret, rec.IdentitySecret)
	set(&e.Source, rec.Source)
	set(&e.AcquiredAt, rec.AcquiredAt)
	set(&e.SecretNote, rec.SecretNote)

	if rec.Email != nil {
		applyRosterEmail(e, *rec.Email, overwrite)
	}
	if e.Email.Source == "" {
		e.Email.Source = EmailSourceNone
	}
}

// applyRosterEmail treats the binding as one unit.
//
// Merging it field by field is how you get an entry pointing at one person's address with
// another's IMAP host — a binding that matches neither side and fails in a way nobody can
// read off the screen. So an existing binding is either kept whole or replaced whole, which is
// also what the review table's single `email` row promises.
func applyRosterEmail(e *Entry, incoming EmailBinding, overwrite bool) {
	hasExisting := e.Email.Source != "" && e.Email.Source != EmailSourceNone
	if hasExisting && !overwrite {
		return
	}
	binding := EmailBinding{
		Address: strings.TrimSpace(incoming.Address),
		Source:  normaliseEmailSource(incoming.Source),
	}
	if incoming.IMAP != nil {
		imap := *incoming.IMAP
		if imap.Port == 0 {
			imap.Port = 993
		}
		if strings.TrimSpace(imap.User) == "" {
			imap.User = binding.Address
		}
		binding.IMAP = &imap
		if binding.Source == EmailSourceNone {
			binding.Source = EmailSourceIMAP
		}
	}
	if incoming.Mailbox != nil {
		mb := *incoming.Mailbox
		binding.Mailbox = &mb
		if binding.Source == EmailSourceNone {
			binding.Source = EmailSourceMailbox
		}
	}
	e.Email = binding
}

// removePlaintextBestEffort overwrites and unlinks a plaintext source file, and reports only
// whether the unlink succeeded.
//
// The name is the contract. On a modern Windows machine this cannot guarantee erasure and the
// UI must not claim it does: SSD wear-levelling remaps the block rather than rewriting it,
// Volume Shadow Copies and OneDrive/Drive version history keep prior revisions, the Search
// index holds extracted content, and whatever produced the file has its own copy. The
// overwrite is still worth doing — it defeats trivial undelete on a spinning disk — but the
// only honest summary line is "we removed the file we could see; check for other copies".
func removePlaintextBestEffort(path string) bool {
	return security.ShredFile(path) == nil
}

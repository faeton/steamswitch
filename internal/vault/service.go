package vault

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"steamswitch/internal/security"
)

// The two values the vault does not read for itself. They are injected from main.go rather
// than exposed as service methods: Wails binds every exported method on a bound service, and
// a frontend-callable "set the API key" is a surface with no reason to exist.
//
// lastUsed comes from the Steam engine so health checks can report idle accounts without
// internal/vault importing internal/steam.
var (
	inputMu  sync.RWMutex
	apiKey   string
	lastUsed = map[string]time.Time{}
)

// SetAPIKey supplies the user's Steam Web API key from settings.
func SetAPIKey(key string) {
	inputMu.Lock()
	apiKey = strings.TrimSpace(key)
	inputMu.Unlock()
}

// SetLastUsed supplies Steam's own record of when each account last logged in.
func SetLastUsed(m map[string]time.Time) {
	next := make(map[string]time.Time, len(m))
	for k, v := range m {
		next[normID(k)] = v
	}
	inputMu.Lock()
	lastUsed = next
	inputMu.Unlock()
}

func checkInput(steamID64 string) QuickCheckInput {
	inputMu.RLock()
	defer inputMu.RUnlock()
	return QuickCheckInput{APIKey: apiKey, LastUsed: lastUsed[normID(steamID64)]}
}

func haveAPIKey() bool {
	inputMu.RLock()
	defer inputMu.RUnlock()
	return apiKey != ""
}

// VaultService is the frontend-facing API. Every method opens with RequireUnlocked: this is
// the one service that *is* the secrets, so the ~98-call-site convention is not optional
// here, and "the caller already checked" is not a reason to skip it.
//
// Only methods the UI actually needs may be exported on this type — Wails binds all of them.
type VaultService struct{}

func NewService() *VaultService { return &VaultService{} }

// Status is what the settings page renders before anything is unlocked.
type Status struct {
	// AppPasswordSet is what the "enable the vault" flow keys off: there is no master key
	// without one, so enabling the vault means setting a password first.
	AppPasswordSet bool `json:"appPasswordSet"`
	Locked         bool `json:"locked"`
	Initialised    bool `json:"initialised"`
	EntryCount     int  `json:"entryCount"`
	RateLimited    bool `json:"rateLimited"`
	HasAPIKey      bool `json:"hasApiKey"`
}

// GetStatus deliberately does not require an unlock — it is what the UI uses to decide
// whether to *offer* an unlock.
func (s *VaultService) GetStatus() (Status, error) {
	st, err := security.GetStatus()
	if err != nil {
		return Status{}, err
	}
	out := Status{
		AppPasswordSet: st.AppPasswordSet,
		Locked:         st.AppLocked,
		Initialised:    Exists(),
		RateLimited:    RateLimited(),
		HasAPIKey:      haveAPIKey(),
	}
	if !out.Locked {
		if list, err := List(); err == nil {
			out.EntryCount = len(list)
		}
	}
	return out, nil
}

func (s *VaultService) ListEntries() ([]Summary, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return nil, ErrLocked
	}
	return List()
}

func (s *VaultService) GetEntry(steamID64 string) (Summary, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return Summary{}, ErrLocked
	}
	return Get(steamID64)
}

func (s *VaultService) SaveEntry(d Draft) error {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ErrLocked
	}
	return Put(d)
}

func (s *VaultService) DeleteEntry(steamID64 string) error {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ErrLocked
	}
	return Delete(steamID64)
}

// RevealField returns exactly one secret, named by the caller. There is no bulk counterpart
// on purpose — see Reveal.
func (s *VaultService) RevealField(steamID64, field string) (string, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return "", ErrLocked
	}
	return Reveal(steamID64, field)
}

// HasEntry backs the tile badge without decrypting anything the caller then has to be
// trusted to drop.
func (s *VaultService) HasEntry(steamID64 string) bool {
	if security.AppLocked() {
		return false
	}
	return Has(steamID64)
}

// GetGuardCode walks the ladder: authenticator, then mailbox. Manual entry is the third
// rung and lives in the UI — an error from here is what surfaces it.
func (s *VaultService) GetGuardCode(steamID64 string) (CodeResult, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return CodeResult{}, ErrLocked
	}
	return GuardCode(context.Background(), steamID64)
}

// BeginLoginAssist declares that the user is about to sign into Steam by hand for this
// account, so a Guard code fetched later in the flow is judged against the moment the flow
// started rather than the moment the user got round to asking. It also starts warming a code.
//
// Returns nothing on success by design: this is an optimisation and a correctness anchor, not
// a step the sign-in depends on, and a UI that failed the flow because the vault was busy
// would be worse than one that just fetches a little slower.
func (s *VaultService) BeginLoginAssist(steamID64 string) error {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ErrLocked
	}
	BeginLoginAssist(steamID64)
	return nil
}

// EndLoginAssist closes the window opened by BeginLoginAssist and cancels any fetch still
// running behind it. Safe to call for an account that never opened one.
func (s *VaultService) EndLoginAssist(steamID64 string) {
	EndLoginAssist(steamID64)
}

// SubmitManualGuardCode hands a user-typed Guard code to a verification login that asked for one
// (an account whose inbox cannot be IMAP-checked). requestID is the id from the
// GuardCodeNeededEvent it answers; ErrNoManualRequest means nothing is waiting for that id (no
// login in flight, or a stale prompt).
func (s *VaultService) SubmitManualGuardCode(steamID64, requestID, code string) error {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ErrLocked
	}
	return SubmitManualGuardCode(steamID64, requestID, code)
}

// TestEmailBinding validates an entry's mailbox configuration without consuming anything.
func (s *VaultService) TestEmailBinding(steamID64 string) error {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ErrLocked
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return ProbeEmail(ctx, steamID64)
}

// RunQuickCheck runs the cheap signals for one account. Safe to call for every account.
func (s *VaultService) RunQuickCheck(steamID64 string) (HealthReport, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return HealthReport{}, ErrLocked
	}
	return QuickCheck(context.Background(), steamID64, checkInput(steamID64))
}

// RunQuickCheckAll checks every entry, one after another.
//
// Serial rather than concurrent: the calls are cheap individually, but a burst of them from
// one IP is what a rate limiter is looking for, and the whole point of the cheap tier is
// that it can be run freely.
func (s *VaultService) RunQuickCheckAll() (map[string]HealthReport, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return nil, ErrLocked
	}
	list, err := List()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(list)+1)*QuickCheckTimeout)
	defer cancel()

	out := make(map[string]HealthReport, len(list))
	for _, e := range list {
		rep, err := QuickCheck(ctx, e.SteamID64, checkInput(e.SteamID64))
		if err != nil {
			continue
		}
		out[e.SteamID64] = rep
		if ctx.Err() != nil {
			break
		}
	}
	return out, nil
}

// RunDeepCheck verifies stored credentials against Steam.
//
// This one logs in for real, which for most accounts sends a Steam Guard email. It is
// explicitly per-account and never batched; there is deliberately no RunDeepCheckAll.
func (s *VaultService) RunDeepCheck(steamID64 string) (HealthReport, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return HealthReport{}, ErrLocked
	}
	return DeepCheck(context.Background(), steamID64, checkInput(steamID64))
}

// RunSessionCheck probes whether an account's stored session token is still live.
//
// It is the middle tier between Check (local signals only) and Verify (a full credential
// login that emails a Guard code): a brief CM sign-in with the saved refresh token, no
// password and no email. It only makes sense for an account that has a stored token; the UI
// disables the action otherwise.
func (s *VaultService) RunSessionCheck(steamID64 string) (HealthReport, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return HealthReport{}, ErrLocked
	}
	return CheckSession(context.Background(), steamID64)
}

// GetTokenDetails backs the login debug panel's Tier 2. The token itself stays behind
// Reveal.
func (s *VaultService) GetTokenDetails(steamID64 string) (TokenDetails, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return TokenDetails{}, ErrLocked
	}
	return TokenInfo(steamID64)
}

// DiscoverIMAP tries the usual hosts for an address so the user does not have to know what
// their provider calls its IMAP server.
func (s *VaultService) DiscoverIMAP(address, password string) (string, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return "", ErrLocked
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, err := autoconfigIMAP(ctx, address, password)
	if err != nil {
		return "", err
	}
	return cfg, nil
}

// IsRateLimited lets the UI hide the deep-check action rather than offering something that
// will be refused.
func (s *VaultService) IsRateLimited() bool { return RateLimited() }

// --- handoff (VAULT.md §9) ---------------------------------------------------------------

// ExportHandoff writes a bundle for another person to import.
//
// The one method here that hands out account access, so it takes the unlock gate like
// everything else and the mode is validated in Export rather than trusted from the caller.
func (s *VaultService) ExportHandoff(req ExportRequest) (ExportResult, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ExportResult{}, ErrLocked
	}
	return Export(req)
}

// ListHandoffBundles enumerates importable files in the handoff folder. It reads only the
// directory — there is nothing readable inside a bundle without its passphrase.
func (s *VaultService) ListHandoffBundles() ([]AvailableBundle, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return nil, ErrLocked
	}
	return ListBundles()
}

// InspectHandoffBundle decrypts a bundle and describes it without writing anything, so the
// import screen can state what is about to be accepted before it is accepted.
//
// name is a filename in the handoff folder, not a path: resolveBundlePath refuses anything
// that would read outside it.
func (s *VaultService) InspectHandoffBundle(name, passphrase string) (BundleInfo, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return BundleInfo{}, ErrLocked
	}
	path, err := resolveBundlePath(name)
	if err != nil {
		return BundleInfo{}, err
	}
	return Inspect(path, passphrase)
}

// AcceptHandoffBundle imports a bundle into this machine's vault.
func (s *VaultService) AcceptHandoffBundle(name, passphrase string) (BundleInfo, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return BundleInfo{}, ErrLocked
	}
	path, err := resolveBundlePath(name)
	if err != nil {
		return BundleInfo{}, err
	}
	return Accept(path, passphrase)
}

// GetHandoffLog returns the local export audit log, newest first. It never left this
// machine and it never will.
func (s *VaultService) GetHandoffLog() ([]ExportRecord, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return nil, ErrLocked
	}
	return ExportLog()
}

// GetHandoffFolder is what the "open the folder" action needs.
func (s *VaultService) GetHandoffFolder() (string, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return "", ErrLocked
	}
	return HandoffDir()
}

// --- bulk import (REDESIGN_BRIEF.md A7) ---------------------------------------------------
//
// Four intakes, one review-and-commit path. Each intake's job is only to turn something into a
// RosterPayload; from PlanImport on, they are indistinguishable.
//
// None of these return the parsed payload. The review model that comes back names fields and
// says whether each is present on either side — it never carries a password, a seed or an
// email credential across the bindings. The plaintext stays in the Go-side session buffer and
// is dropped on commit, on cancel, on a 15-minute timeout, and whenever the app locks.

// PrepareRosterFromBundle is intake A: a passphrase-sealed .ssroster the user picked.
//
// The canonical path, and the one the CLI's --seal-roster produces. Nothing readable exists on
// disk without the passphrase.
func (s *VaultService) PrepareRosterFromBundle(path, passphrase string) (ImportPlan, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ImportPlan{}, ErrLocked
	}
	raw, err := readRosterFile(path)
	if err != nil {
		return ImportPlan{}, err
	}
	payload, err := OpenRoster(raw, passphrase)
	if err != nil {
		return ImportPlan{}, err
	}
	return PlanImport(payload, "")
}

// PrepareRosterFromText is intake B: JSON or CSV pasted into the review buffer.
//
// The human on-ramp, for somebody who has a list in a spreadsheet rather than an agent. The
// text is parsed and buffered here and is never written to disk in any form — but note that on
// Windows the clipboard itself has history (Win+V) and is readable by any process, so the copy
// the user pasted from is not under this app's control. The UI says so.
func (s *VaultService) PrepareRosterFromText(text string) (ImportPlan, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ImportPlan{}, ErrLocked
	}
	payload, err := ParseRosterPlaintext([]byte(text))
	if err != nil {
		return ImportPlan{}, err
	}
	return PlanImport(payload, "")
}

// PrepareRosterFromPlaintextFile is intake D: the legacy escape hatch.
//
// For a user who already has a plaintext file and is not going to be talked out of it. It is
// explicit pick, one shot — there is deliberately no watched folder, because a directory this
// app imports from automatically is a directory the Search index, the backup agent and the
// cloud-sync client all get to first.
//
// The path is carried into the plan so the confirm step can name the file and the summary can
// report what removing it did and did not achieve.
func (s *VaultService) PrepareRosterFromPlaintextFile(path string) (ImportPlan, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ImportPlan{}, ErrLocked
	}
	raw, err := readRosterFile(path)
	if err != nil {
		return ImportPlan{}, err
	}
	payload, err := ParseRosterPlaintext(raw)
	if err != nil {
		return ImportPlan{}, err
	}
	return PlanImport(payload, path)
}

// RepriceRosterImport recomputes the plan after the user changes per-row modes, so the table
// shows the consequence of "overwrite this one" before anything is committed.
func (s *VaultService) RepriceRosterImport(sessionID string, decisions []RowDecision) (ImportPlan, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ImportPlan{}, ErrLocked
	}
	return RepriceImport(sessionID, decisions)
}

// CommitRosterImport writes every accepted row in one vault mutate.
func (s *VaultService) CommitRosterImport(sessionID string, decisions []RowDecision) (ImportSummary, error) {
	if err := security.RequireVaultUnlocked(); err != nil {
		return ImportSummary{}, ErrLocked
	}
	return CommitImport(sessionID, decisions)
}

// DiscardRosterImport drops a buffered roster on cancel, rather than leaving it to time out.
func (s *VaultService) DiscardRosterImport(sessionID string) {
	DiscardImport(sessionID)
}

// GetRosterTemplate returns the documented pre-encryption payload shape.
//
// Published so an agent or script has something to fill in — and so the encrypt step is part
// of the documented format rather than an afterthought a user is expected to infer.
func (s *VaultService) GetRosterTemplate() string { return RosterTemplate() }

// ErrVaultDisabled is returned when a vault operation is attempted with no app password
// set. There is no master key without one, so this is a precondition rather than a failure.
var ErrVaultDisabled = errors.New("Toast_Vault_NeedsAppPassword")

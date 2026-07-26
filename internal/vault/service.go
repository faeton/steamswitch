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
	if err := security.RequireUnlocked(); err != nil {
		return nil, ErrLocked
	}
	return List()
}

func (s *VaultService) GetEntry(steamID64 string) (Summary, error) {
	if err := security.RequireUnlocked(); err != nil {
		return Summary{}, ErrLocked
	}
	return Get(steamID64)
}

func (s *VaultService) SaveEntry(d Draft) error {
	if err := security.RequireUnlocked(); err != nil {
		return ErrLocked
	}
	return Put(d)
}

func (s *VaultService) DeleteEntry(steamID64 string) error {
	if err := security.RequireUnlocked(); err != nil {
		return ErrLocked
	}
	return Delete(steamID64)
}

// RevealField returns exactly one secret, named by the caller. There is no bulk counterpart
// on purpose — see Reveal.
func (s *VaultService) RevealField(steamID64, field string) (string, error) {
	if err := security.RequireUnlocked(); err != nil {
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
	if err := security.RequireUnlocked(); err != nil {
		return CodeResult{}, ErrLocked
	}
	return GuardCode(context.Background(), steamID64)
}

// TestEmailBinding validates an entry's mailbox configuration without consuming anything.
func (s *VaultService) TestEmailBinding(steamID64 string) error {
	if err := security.RequireUnlocked(); err != nil {
		return ErrLocked
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return ProbeEmail(ctx, steamID64)
}

// RunQuickCheck runs the cheap signals for one account. Safe to call for every account.
func (s *VaultService) RunQuickCheck(steamID64 string) (HealthReport, error) {
	if err := security.RequireUnlocked(); err != nil {
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
	if err := security.RequireUnlocked(); err != nil {
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
	if err := security.RequireUnlocked(); err != nil {
		return HealthReport{}, ErrLocked
	}
	return DeepCheck(context.Background(), steamID64, checkInput(steamID64))
}

// GetTokenDetails backs the login debug panel's Tier 2. The token itself stays behind
// Reveal.
func (s *VaultService) GetTokenDetails(steamID64 string) (TokenDetails, error) {
	if err := security.RequireUnlocked(); err != nil {
		return TokenDetails{}, ErrLocked
	}
	return TokenInfo(steamID64)
}

// DiscoverIMAP tries the usual hosts for an address so the user does not have to know what
// their provider calls its IMAP server.
func (s *VaultService) DiscoverIMAP(address, password string) (string, error) {
	if err := security.RequireUnlocked(); err != nil {
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
	if err := security.RequireUnlocked(); err != nil {
		return ExportResult{}, ErrLocked
	}
	return Export(req)
}

// ListHandoffBundles enumerates importable files in the handoff folder. It reads only the
// directory — there is nothing readable inside a bundle without its passphrase.
func (s *VaultService) ListHandoffBundles() ([]AvailableBundle, error) {
	if err := security.RequireUnlocked(); err != nil {
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
	if err := security.RequireUnlocked(); err != nil {
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
	if err := security.RequireUnlocked(); err != nil {
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
	if err := security.RequireUnlocked(); err != nil {
		return nil, ErrLocked
	}
	return ExportLog()
}

// GetHandoffFolder is what the "open the folder" action needs.
func (s *VaultService) GetHandoffFolder() (string, error) {
	if err := security.RequireUnlocked(); err != nil {
		return "", ErrLocked
	}
	return HandoffDir()
}

// ErrVaultDisabled is returned when a vault operation is attempted with no app password
// set. There is no master key without one, so this is a precondition rather than a failure.
var ErrVaultDisabled = errors.New("Toast_Vault_NeedsAppPassword")

// Package vault stores the secrets an account has — password, TOTP seed, email binding,
// session tokens — encrypted under a subkey of the app-lock master key, and records what a
// health check last said about the account.
//
// Design lives in VAULT.md. Three rules run through every file here:
//
//   - Nothing is ever stored in the clear. There is no plaintext fallback; if the vault
//     cannot be opened the feature is unavailable, which is the honest outcome.
//   - A secret leaves this package only through a single-field reveal that the caller asked
//     for by name. List and summary views carry booleans ("has a password") and never values,
//     so a screenshot of the account list cannot leak one.
//   - Secrets never reach slog, actionlog, crash reports or errors. Log field names and
//     outcomes, never values.
package vault

// Version is the plaintext document version. Bump only for a shape change that an older
// build could not read; the sealed envelope carries its own separate version.
const Version = 1

// Doc is the decrypted vault. It is one JSON document rather than one file per account so
// that neither the entry count nor the entry names (which are email addresses as often as
// not) are observable from the filesystem.
type Doc struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"` // keyed by SteamID64

	// Exports is the local handoff audit log (VAULT.md §9.3): what left this machine, under
	// which label, when. It lives inside the sealed blob rather than beside it because the
	// labels are the user's own words about their accounts, and it never leaves the machine
	// that wrote it.
	Exports []ExportRecord `json:"exports,omitempty"`

	// ImportedBundles is how the advisory single-use marker is enforced — bundle id to the
	// time it was accepted. Kept keyed by id rather than by account so that deleting the
	// entry does not make a single-use bundle importable again.
	ImportedBundles map[string]string `json:"importedBundles,omitempty"`
}

// ExportRecord is one line of the handoff audit log. It records that an export happened and
// what shape it had. It does not record the passphrase, and there is nothing in it that
// would let this machine open the bundle it describes.
type ExportRecord struct {
	BundleID   string `json:"bundleId"`
	SteamID64  string `json:"steamId64"`
	Mode       string `json:"mode"`
	Label      string `json:"label,omitempty"`
	ExportedAt string `json:"exportedAt"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
	SingleUse  bool   `json:"singleUse,omitempty"`
	Path       string `json:"path,omitempty"`
}

// Entry is everything the vault knows about one account.
//
// Identity is duplicated from ids.json deliberately: a vault restored next to a fresh data
// directory has to be self-describing, and joining against a file that may not exist yet is
// not a property worth having.
type Entry struct {
	SteamID64   string `json:"steamId64"`
	AccountName string `json:"accountName,omitempty"`
	Label       string `json:"label,omitempty"`

	// Standalone entries are records for accounts that are never signed into on this
	// machine — stored and health-checked only. They are kept out of the account grid.
	Standalone bool `json:"standalone,omitempty"`

	// Credentials.
	Password       string `json:"password,omitempty"`
	SharedSecret   string `json:"sharedSecret,omitempty"`   // TOTP seed
	IdentitySecret string `json:"identitySecret,omitempty"` // trade confirmations

	// Session material — the crown jewels. A client-audience refresh token authenticates
	// from another machine with no password and no Guard challenge, so this field is the
	// reason the blob is encrypted at all.
	RefreshToken   string `json:"refreshToken,omitempty"`
	GuardData      string `json:"guardData,omitempty"`
	TokenExpiresAt string `json:"tokenExpiresAt,omitempty"` // RFC3339, decoded from the JWT

	Email EmailBinding `json:"email"`

	// Provenance. The short note stays in SteamSettings.AccountNotes unencrypted; this is
	// the one that may say where an account came from.
	Source     string `json:"source,omitempty"` // "bought", "main", "gift"…
	AcquiredAt string `json:"acquiredAt,omitempty"`
	SecretNote string `json:"secretNote,omitempty"`

	Health *HealthReport `json:"health,omitempty"`

	// NextEligibleAt is persisted so that quitting the app is not a way to reset the deep
	// check's backoff clock.
	NextEligibleAt string `json:"nextEligibleAt,omitempty"`
	CheckFailures  int    `json:"checkFailures,omitempty"`

	EgressID  string `json:"egressId,omitempty"` // reserved, see TODO.md
	UpdatedAt string `json:"updatedAt"`
}

// EmailSource names which rung of the code ladder an entry uses.
const (
	EmailSourceNone    = "none"
	EmailSourceIMAP    = "imap"
	EmailSourceMailbox = "mailbox-api"
)

type EmailBinding struct {
	Address string      `json:"address,omitempty"`
	Source  string      `json:"source"`
	IMAP    *IMAPCreds  `json:"imap,omitempty"`
	Mailbox *MailboxRef `json:"mailbox,omitempty"`
}

type IMAPCreds struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	UseTLS   bool   `json:"useTls"`
}

// MailboxRef points at a mailbox API the user runs. There is no default host: shipping one
// would make this fork phone somewhere by default, which the fork rules forbid.
type MailboxRef struct {
	BaseURL   string `json:"baseUrl"`
	Token     string `json:"token"`
	MailboxID string `json:"mailboxId"`
}

// Verdict values, worst-wins when a report is summarised.
const (
	VerdictUnknown = "unknown"
	VerdictOK      = "ok"
	VerdictWarn    = "warn"
	VerdictFail    = "fail"
)

type HealthReport struct {
	ProbedAt string   `json:"probedAt"`
	Verdict  string   `json:"verdict"`
	Deep     bool     `json:"deep"` // whether a credential login was part of this report
	Signals  []Signal `json:"signals"`
}

// Signal is one check's outcome. Detail is an i18n key, never a secret and never a raw
// error string that might quote one.
type Signal struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Detail  string            `json:"detail"`
	Params  map[string]string `json:"params,omitempty"`
	Blocker bool              `json:"blocker"`
}

// Signal names.
const (
	SignalBans     = "steam_bans"
	SignalProfile  = "steam_profile"
	SignalToken    = "token"
	SignalTOTP     = "totp"
	SignalLastUsed = "last_used"
	SignalEmail    = "email"
	SignalPassword = "password"
)

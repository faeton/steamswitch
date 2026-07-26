package steam

import (
	"context"
	"errors"
	"strings"
	"sync"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/platform"
	"steamswitch/internal/security"
	"steamswitch/internal/sessionkit"
)

// SessionKitService is the frontend-facing API for switching with kit protection
// (REDESIGN.md §2). It is bound into `serviceList()` and is the only way the UI reaches the
// engine.
//
// # Lock hierarchy
//
// Three locks can meet on these paths, and they are only safe in one order:
//
//	sessionkit.Engine.mu  →  SteamService.mu  →  dotaWriteMu
//
// The engine's lock is outermost because it spans a whole transaction — close Steam,
// snapshot, apply, swap login, launch — while the other two are per-call. Two rules keep
// that order true:
//
//  1. `Lifecycle` never takes `SteamService.mu`. It calls the package-level helpers
//     (`writeLoginUsersAndRegistry`, `buildSteamArgs`, …) directly, so a step running under
//     the engine lock cannot block on a service call that is itself waiting for the engine.
//  2. The manual Dota tools in `dota.go` take `dotaWriteMu` but never the engine lock. They
//     instead *refuse* while a transaction is in flight, via `ErrKitTransactionActive`
//     below — trying to acquire the engine lock from a tool would invert the order.
type SessionKitService struct {
	engine *sessionkit.Engine

	// mu serialises the service's own bookkeeping, not the engine's work. Held only around
	// map access, never across a call into the engine.
	mu sync.Mutex
}

// ErrKitNotReady is returned before the engine has been constructed, which can only happen
// if a UI call races startup.
var ErrKitNotReady = errors.New("Toast_Kit_NotReady")

// kitEngine is process-wide because the tray, the CLI and the window all switch accounts and
// must contend for the same transaction lock. A second Engine would mean a second lock and
// therefore no mutual exclusion at all.
var (
	kitEngineMu sync.RWMutex
	kitEngine   *sessionkit.Engine
)

// InitSessionKit builds the process-wide engine. Called from main.go after
// platform.InitDataPaths, since the store lives under the data root.
func InitSessionKit(svc *SteamService) (*sessionkit.Engine, error) {
	eng, err := sessionkit.New(sessionkit.Options{
		Lifecycle: Lifecycle{},
		Modules:   []sessionkit.Module{DotaModule{}},
		Progress:  kitProgress{},
		Home: func() (sessionkit.AccountRef, error) {
			st, err := LoadSettings()
			if err != nil {
				return sessionkit.AccountRef{}, err
			}
			return sessionkit.AccountRef{SteamID64: strings.TrimSpace(st.HomeSteamID64)}, nil
		},
		IsShared: func(a sessionkit.AccountRef) bool {
			st, err := LoadSettings()
			if err != nil {
				return false
			}
			return st.IsShared(strings.TrimSpace(a.SteamID64))
		},
	})
	if err != nil {
		return nil, err
	}
	kitEngineMu.Lock()
	kitEngine = eng
	kitEngineMu.Unlock()
	return eng, nil
}

// activeKitEngine returns the process-wide engine, if it has been built.
func activeKitEngine() *sessionkit.Engine {
	kitEngineMu.RLock()
	defer kitEngineMu.RUnlock()
	return kitEngine
}

// NewSessionKitService wraps an engine for binding.
func NewSessionKitService(eng *sessionkit.Engine) *SessionKitService {
	return &SessionKitService{engine: eng}
}

// kitProgress forwards engine narration to the status strip. It exists so `sessionkit` need
// not import `platform`, per the import rule in CLAUDE.md.
type kitProgress struct{}

func (kitProgress) Phase(key string, vars map[string]string) {
	if len(vars) == 0 {
		platform.EmitActionBarStatusI18n(key)
		return
	}
	platform.EmitActionBarStatusI18nVars(key, vars)
}

func (s *SessionKitService) eng() (*sessionkit.Engine, error) {
	if s.engine != nil {
		return s.engine, nil
	}
	if e := activeKitEngine(); e != nil {
		return e, nil
	}
	return nil, ErrKitNotReady
}

// KitStatus is what the status strip and the recovery modal render.
type KitStatus struct {
	sessionkit.RecoveryState
	// HomeSteamID64 and SharedIDs are folded in so the UI can render roles and kit state
	// from a single call on startup rather than racing two.
	HomeSteamID64 string   `json:"homeSteamId64"`
	SharedIDs     []string `json:"sharedIds"`
	// CloudRisk reports whether the active kit wrote a cloud-synced part, which the strip
	// must disclose rather than claiming the kit is durable.
	CloudRisk bool `json:"cloudRisk"`
	// Modules carries display names, not ids, since this feeds user-visible copy.
	ModuleNames []string `json:"moduleNames"`
}

// GetKitStatus classifies the current state without changing anything. Safe to call on every
// window focus and on startup while the app is still locked.
func (s *SessionKitService) GetKitStatus() (KitStatus, error) {
	e, err := s.eng()
	if err != nil {
		return KitStatus{}, err
	}
	st, err := e.Status()
	if err != nil {
		return KitStatus{}, err
	}
	out := KitStatus{RecoveryState: st}
	for _, id := range st.Modules {
		out.ModuleNames = append(out.ModuleNames, moduleDisplayName(id))
	}
	settings, err := LoadSettings()
	if err == nil {
		out.HomeSteamID64 = strings.TrimSpace(settings.HomeSteamID64)
		out.SharedIDs = append([]string(nil), settings.SharedSteamIDs...)
	}
	if out.SharedIDs == nil {
		out.SharedIDs = []string{}
	}
	out.CloudRisk = st.Kind == sessionkit.RecoveryKitActive
	return out, nil
}

// moduleDisplayName maps a journal module id to its user-facing name without needing an
// engine lookup, so it stays callable from status paths.
func moduleDisplayName(id string) string {
	if id == DotaModuleID {
		return DotaModule{}.DisplayName()
	}
	return id
}

// SwitchTo is the single entry point the account tiles call.
//
// It replaces a bare `SwapToSteamAccount` on the main path: switching *to* a shared account
// has to carry the kit, and the engine is the only thing that journals that. A plain switch
// still ends up doing exactly what the old path did, minus the short circuit.
func (s *SessionKitService) SwitchTo(steamID64 string, personaState int) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	e, err := s.eng()
	if err != nil {
		return err
	}
	id := strings.TrimSpace(steamID64)
	if id == "" {
		return errRolesInvalidID
	}
	return e.Enter(context.Background(), sessionkit.AccountRef{SteamID64: id}, personaState)
}

// LeaveKit answers the "Restore X's setup?" prompt and switches on to `steamID64`.
//
// `choice` is `restore-theirs` or `keep-mine`. The prompt is asked every time by design
// (REDESIGN.md §2): the default is to put the other person's files back.
func (s *SessionKitService) LeaveKit(steamID64, choice string, personaState int) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	e, err := s.eng()
	if err != nil {
		return err
	}
	c := sessionkit.LeaveChoice(strings.TrimSpace(choice))
	if c != sessionkit.LeaveRestoreTheirs && c != sessionkit.LeaveKeepMine {
		return errors.New("Toast_Kit_UnknownChoice")
	}
	target := sessionkit.AccountRef{SteamID64: strings.TrimSpace(steamID64)}
	return e.Leave(context.Background(), target, c, personaState)
}

// ResolveRecovery applies the user's answer to the recovery modal:
// `restore-theirs`, `keep-current` or `abandon`.
func (s *SessionKitService) ResolveRecovery(action string) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	e, err := s.eng()
	if err != nil {
		return err
	}
	a := sessionkit.RecoveryAction(strings.TrimSpace(action))
	switch a {
	case sessionkit.ActionRestoreTheirs, sessionkit.ActionKeepCurrent, sessionkit.ActionAbandon:
	default:
		return errors.New("Toast_Kit_UnknownChoice")
	}
	actionlog.Record("kit:resolve", string(a), "", nil)
	return e.Resolve(context.Background(), a)
}

// CheckCloudClobber re-reads the cloud-synced parts and reports whether the kit survived.
//
// Read-only, and it must stay that way: re-applying while Steam is up would simply be
// overwritten again. The UI offers a re-apply only once Steam is closed.
func (s *SessionKitService) CheckCloudClobber() (bool, error) {
	e, err := s.eng()
	if err != nil {
		return true, err
	}
	return e.VerifyCloudRisk(context.Background())
}

// ErrKitTransactionActive refuses a manual config operation while a transaction is in
// flight or a kit is live.
//
// REDESIGN.md §2 requires this: the snapshot lab writes the same trees the engine has
// journalled hashes for, so a manual apply during a kit would make the engine's recorded
// state a lie and turn the next restore into an "external change" block — or, worse, a
// silent overwrite of someone else's files.
var ErrKitTransactionActive = errors.New("Toast_Kit_TransactionActive")

// guardManualConfigWrite is called by the manual Dota tools before they mutate anything.
//
// It reads the engine's state rather than taking its lock, which is what keeps the lock
// order in the type comment intact.
func guardManualConfigWrite() error {
	e := activeKitEngine()
	if e == nil {
		return nil
	}
	st, err := e.Status()
	if err != nil {
		// An unreadable journal is itself a reason to refuse: we cannot show that no
		// transaction is outstanding.
		return ErrKitTransactionActive
	}
	switch st.Kind {
	case sessionkit.RecoveryNone:
		return nil
	default:
		return ErrKitTransactionActive
	}
}

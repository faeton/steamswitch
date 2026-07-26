package steam

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/security"
	"steamswitch/internal/sessionkit"
)

// Game modules: the registry, the pause state, and the self-test (REDESIGN.md §5).
//
// A module can stop being safe to run without anything in this app changing — a game patch
// moves its config folders, a library is unmounted, the user uninstalls. `Detection.Fingerprint`
// exists to notice that, and auto-pause is what the engine does about it: a module whose layout
// no longer matches the one last confirmed contributes nothing to a transaction until a
// self-test passes.
//
// Auto-pause is deliberately not "detect and carry on". The alternative is writing a kit into
// a layout this build no longer recognises, which is how a game patch turns into somebody
// losing their settings.

// registeredModules is every module this build knows about, in display order.
//
// Registration is not the same as being enabled. CS2 is here and detects correctly but refuses
// every write; see sessionkit_cs2.go.
func registeredModules() []sessionkit.Module {
	return []sessionkit.Module{DotaModule{}, CS2Module{}}
}

// engineModules is what the engine is constructed with.
//
// Pause state is applied per Detect rather than by filtering this list, because the engine is
// built once at startup and lives for the process: filtering here would mean a module paused
// from Settings kept running until the next launch. `Engine.plan` already skips a module whose
// Detection reports Paused, and a module with no plan cannot appear in a journal, so this is
// the mechanism rather than a shortcut around it.
func engineModules() []sessionkit.Module {
	all := registeredModules()
	out := make([]sessionkit.Module, 0, len(all))
	for _, m := range all {
		out = append(out, pausableModule{Module: m})
	}
	return out
}

// pausableModule folds the persisted pause state into a module's own Detect answer.
//
// Only Detect is overridden. The engine consults it before anything else and skips a paused
// module entirely, so there is no need — and no benefit — to also stub out the write methods:
// doing that would hide a genuine bug (a mutating call that skipped planning) behind a silent
// no-op instead of letting it fail.
type pausableModule struct {
	sessionkit.Module
}

var _ sessionkit.Module = pausableModule{}

func (p pausableModule) Detect(ctx context.Context, req sessionkit.DetectRequest) (sessionkit.Detection, error) {
	det, err := p.Module.Detect(ctx, req)
	if err != nil {
		return det, err
	}
	st, settingsErr := LoadSettings()
	if settingsErr != nil {
		// Unreadable settings must not be read as "nothing is paused". Pause is a safety
		// state, so the unknown answer is the cautious one.
		steamLog.Warn("game modules: settings unreadable, treating as paused",
			"module", p.Module.ID(), "err", settingsErr)
		det.Paused = true
		det.Reason = "Kit_Module_Paused"
		return det, nil
	}
	state := st.ModuleState(p.Module.ID())
	if state.Paused {
		det.Paused = true
		if det.Reason == "" {
			det.Reason = "Kit_Module_Paused"
			if state.PausedByFingerprintChange {
				det.Reason = "Kit_Module_PausedByChange"
			}
		}
	}
	return det, nil
}

// LivePath and ScratchAnchor are optional interfaces the engine type-asserts for. Embedding
// alone would not carry them through the wrapper, and losing either silently downgrades
// recovery: without LivePath a part cannot be rolled back, and without ScratchAnchor the
// engine refuses the transaction outright.
func (p pausableModule) LivePath(steamRoot string, account sessionkit.AccountRef, partID string) (string, bool) {
	r, ok := p.Module.(sessionkit.LivePathResolver)
	if !ok {
		return "", false
	}
	return r.LivePath(steamRoot, account, partID)
}

func (p pausableModule) ScratchAnchor(steamRoot string, account sessionkit.AccountRef) (string, bool) {
	a, ok := p.Module.(sessionkit.ScratchAnchor)
	if !ok {
		return "", false
	}
	return a.ScratchAnchor(steamRoot, account)
}

// GameModuleStatus is one card in Settings → Game modules.
type GameModuleStatus struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Installed   bool   `json:"installed"`
	// Ready is the module's own answer, before pause state is applied.
	Ready bool `json:"ready"`
	// Paused is the persisted state; Active is what actually happens on the next switch.
	Paused                    bool `json:"paused"`
	PausedByFingerprintChange bool `json:"pausedByFingerprintChange"`
	Active                    bool `json:"active"`
	// Reason is an i18n key explaining a non-active state, or empty.
	Reason string `json:"reason,omitempty"`
	// Fingerprint is the layout signature seen right now; KnownGoodFingerprint is the one a
	// self-test last confirmed. Both are shown so a mismatch is legible rather than magic.
	Fingerprint          string   `json:"fingerprint,omitempty"`
	KnownGoodFingerprint string   `json:"knownGoodFingerprint,omitempty"`
	LastSelfTestAt       string   `json:"lastSelfTestAt,omitempty"`
	Parts                []string `json:"parts,omitempty"`
}

// ListGameModules reports every registered module's current state, applying auto-pause as a
// side effect where the fingerprint has moved.
//
// The side effect is the point: this is called on the Settings page and before a switch, which
// is exactly when a layout change should be noticed. Doing it lazily here rather than on a
// timer means the check runs when it can be acted on and never while the user is elsewhere.
func (s *SteamService) ListGameModules() ([]GameModuleStatus, error) {
	if err := security.RequireUnlocked(); err != nil {
		return nil, err
	}
	steamRoot, _, err := Lifecycle{}.resolveRoot()
	if err != nil {
		// No Steam install found is a legitimate state for this page; report the modules
		// as uninstalled rather than failing the whole panel.
		steamRoot = ""
	}

	st, err := LoadSettings()
	if err != nil {
		return nil, err
	}

	out := make([]GameModuleStatus, 0, len(registeredModules()))
	changed := false
	for _, m := range registeredModules() {
		det, detErr := m.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: steamRoot})
		if detErr != nil {
			steamLog.Warn("game module detect failed", "module", m.ID(), "err", detErr)
		}
		state := st.ModuleState(m.ID())

		if autoPauseNeeded(state, det) {
			state.Paused = true
			state.PausedByFingerprintChange = true
			st = withModuleState(st, m.ID(), state)
			changed = true
			actionlog.Record("kit:moduleAutoPaused", m.ID(), det.Fingerprint, nil)
			steamLog.Warn("game module auto-paused: layout fingerprint changed",
				"module", m.ID(), "was", state.KnownGoodFingerprint, "now", det.Fingerprint)
		}

		out = append(out, gameModuleStatus(m, det, state))
	}

	if changed {
		if err := SaveSettings(st); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	return out, nil
}

// autoPauseNeeded reports whether the game's layout has moved away from the one last confirmed.
//
// Only fires when there *is* a known-good fingerprint to compare against and the module is
// currently installed and unpaused. A module that has never passed a self-test is not paused
// for it — it has made no claim to have been verified, and pausing it would make the first run
// of a fresh install look like a fault.
func autoPauseNeeded(state GameModuleState, det sessionkit.Detection) bool {
	if state.Paused || state.KnownGoodFingerprint == "" {
		return false
	}
	if !det.Installed || det.Fingerprint == "" {
		// An uninstalled or unreadable game is reported as such by Detect. Pausing on top
		// would leave the module paused after the user reinstalls, with no obvious cause.
		return false
	}
	return det.Fingerprint != state.KnownGoodFingerprint
}

func gameModuleStatus(m sessionkit.Module, det sessionkit.Detection, state GameModuleState) GameModuleStatus {
	status := GameModuleStatus{
		ID:                        m.ID(),
		DisplayName:               m.DisplayName(),
		Installed:                 det.Installed,
		Ready:                     det.Ready,
		Paused:                    state.Paused,
		PausedByFingerprintChange: state.PausedByFingerprintChange,
		Fingerprint:               det.Fingerprint,
		KnownGoodFingerprint:      state.KnownGoodFingerprint,
		LastSelfTestAt:            state.LastSelfTestAt,
		Reason:                    det.Reason,
	}
	for _, p := range det.Parts {
		status.Parts = append(status.Parts, p.Label)
	}
	status.Active = det.Ready && !state.Paused
	if status.Active {
		status.Reason = ""
	} else if state.Paused && status.Reason == "" {
		status.Reason = "Kit_Module_Paused"
		if state.PausedByFingerprintChange {
			status.Reason = "Kit_Module_PausedByChange"
		}
	}
	return status
}

func withModuleState(st Settings, moduleID string, state GameModuleState) Settings {
	if st.GameModules == nil {
		st.GameModules = map[string]GameModuleState{}
	}
	st.GameModules[strings.TrimSpace(moduleID)] = state
	return st
}

// ErrUnknownGameModule is returned for an id no build knows.
var ErrUnknownGameModule = errors.New("Toast_Kit_UnknownModule")

func moduleByID(id string) (sessionkit.Module, bool) {
	id = strings.TrimSpace(id)
	for _, m := range registeredModules() {
		if m.ID() == id {
			return m, true
		}
	}
	return nil, false
}

// SetGameModulePaused is the manual switch on a module card.
//
// Un-pausing clears the auto-pause flag but deliberately does *not* record a known-good
// fingerprint: the user saying "run this anyway" is not evidence that the layout is
// understood. Only a self-test writes that.
func (s *SteamService) SetGameModulePaused(moduleID string, paused bool) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	if _, ok := moduleByID(moduleID); !ok {
		return ErrUnknownGameModule
	}
	st, err := LoadSettings()
	if err != nil {
		return err
	}
	state := st.ModuleState(moduleID)
	state.Paused = paused
	if !paused {
		state.PausedByFingerprintChange = false
	}
	actionlog.Record("kit:modulePauseSet", moduleID, boolLabel(paused), nil)
	return SaveSettings(withModuleState(st, moduleID, state))
}

func boolLabel(b bool) string {
	if b {
		return "paused"
	}
	return "active"
}

// GameModuleSelfTestResult is what the "Run self-test" button reports back.
type GameModuleSelfTestResult struct {
	Passed bool `json:"passed"`
	// Reason is an i18n key when Passed is false.
	Reason      string   `json:"reason,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Checks      []string `json:"checks,omitempty"`
}

// RunGameModuleSelfTest re-checks a module against the live install and, if it passes, records
// the current layout as known-good and lifts an automatic pause.
//
// Strictly read-only. Every check is a Detect or a Preflight — both are documented as
// non-mutating — so a self-test on a module that turns out to be broken cannot itself break
// anything. That matters because the button exists to be pressed when something is already
// wrong.
func (s *SteamService) RunGameModuleSelfTest(moduleID string) (GameModuleSelfTestResult, error) {
	if err := security.RequireUnlocked(); err != nil {
		return GameModuleSelfTestResult{}, err
	}
	m, ok := moduleByID(moduleID)
	if !ok {
		return GameModuleSelfTestResult{}, ErrUnknownGameModule
	}

	steamRoot, _, err := Lifecycle{}.resolveRoot()
	if err != nil {
		return GameModuleSelfTestResult{Reason: "Toast_Kit_NoSteamRoot"}, nil
	}

	res := GameModuleSelfTestResult{Checks: []string{"Kit_SelfTest_Detect"}}
	det, err := m.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: steamRoot})
	if err != nil {
		res.Reason = err.Error()
		return res, nil
	}
	res.Fingerprint = det.Fingerprint
	if !det.Installed {
		res.Reason = det.Reason
		if res.Reason == "" {
			res.Reason = "Kit_SelfTest_NotInstalled"
		}
		return res, nil
	}
	if !det.Ready {
		res.Reason = det.Reason
		if res.Reason == "" {
			res.Reason = "Kit_SelfTest_NotReady"
		}
		return res, nil
	}
	if det.Fingerprint == "" {
		// Without one there is nothing to record, and auto-pause could never fire again.
		res.Reason = "Kit_SelfTest_NoFingerprint"
		return res, nil
	}

	// Resolve the same plan a real switch would, against the Home account. This is what
	// catches a layout change that Detect alone cannot see — a part whose directory has
	// moved still fingerprints fine if the cfg folder and build id have not changed.
	res.Checks = append(res.Checks, "Kit_SelfTest_Plan")
	st, err := LoadSettings()
	if err != nil {
		return GameModuleSelfTestResult{}, err
	}
	home := sessionkit.AccountRef{SteamID64: strings.TrimSpace(st.HomeSteamID64)}
	if home.IsZero() {
		res.Reason = "Kit_SelfTest_NoHome"
		return res, nil
	}
	plan, err := m.Preflight(context.Background(), sessionkit.PreflightRequest{
		Operation: sessionkit.OperationVerify,
		Source:    home,
		Target:    home,
		SteamRoot: steamRoot,
	})
	if err != nil {
		res.Reason = err.Error()
		return res, nil
	}
	if len(plan.Parts) == 0 {
		res.Reason = "Kit_SelfTest_NoParts"
		return res, nil
	}

	// Every part must resolve to a path. A module that cannot say where a part lives would
	// be journalling a write it cannot perform.
	res.Checks = append(res.Checks, "Kit_SelfTest_Paths")
	resolver, hasResolver := m.(sessionkit.LivePathResolver)
	if !hasResolver {
		res.Reason = "Kit_SelfTest_NoPaths"
		return res, nil
	}
	for _, part := range plan.Parts {
		if _, ok := resolver.LivePath(steamRoot, home, part.ID); !ok {
			res.Reason = "Kit_SelfTest_NoPaths"
			return res, nil
		}
	}

	// Staging and rollback have to land on the same volume as the files being replaced,
	// because ReplacePart installs by renaming. A module without an anchor is refused by the
	// engine at transaction start, so catching it here turns a mid-switch abort into a
	// message on a settings page.
	res.Checks = append(res.Checks, "Kit_SelfTest_Scratch")
	anchor, hasAnchor := m.(sessionkit.ScratchAnchor)
	if !hasAnchor {
		res.Reason = "Kit_SelfTest_NoScratch"
		return res, nil
	}
	if _, ok := anchor.ScratchAnchor(steamRoot, home); !ok {
		res.Reason = "Kit_SelfTest_NoScratch"
		return res, nil
	}

	state := st.ModuleState(moduleID)
	state.KnownGoodFingerprint = det.Fingerprint
	state.LastSelfTestAt = time.Now().UTC().Format(time.RFC3339)
	if state.PausedByFingerprintChange {
		// The layout was re-verified, so the reason for the automatic pause is gone. A pause
		// the *user* set is left alone — they did not ask for it to be undone.
		state.Paused = false
		state.PausedByFingerprintChange = false
	}
	if err := SaveSettings(withModuleState(st, moduleID, state)); err != nil {
		return GameModuleSelfTestResult{}, err
	}

	actionlog.Record("kit:moduleSelfTest", moduleID, det.Fingerprint, nil)
	res.Passed = true
	return res, nil
}

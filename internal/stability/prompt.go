package stability

import "log/slog"

// OnSuccessfulSwitch increments the per-platform switch counter. Local only.
//
// It used to also raise a "Did the switch work?" prompt. Its answers went nowhere: this fork has
// no reporting endpoint, so the prompt thanked people for a rating it threw away.
func OnSuccessfulSwitch(platformKey string) {
	mu.Lock()
	_, err := incrementSwitchLocked(platformKey)
	mu.Unlock()
	if err != nil {
		slog.Warn("stability: increment switch counter", "err", err, "platform", platformKey)
	}
}

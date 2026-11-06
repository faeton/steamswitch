//go:build windows

package winutil

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const protocolScheme = "steamswitch"

// protocolClassesKey is the per-user class root. A new key written through HKCR lands in HKLM,
// which an unelevated process cannot write, so the merged view would refuse every normal user.
var protocolClassesKey = `Software\Classes`

func protocolKey(sub string) string {
	return protocolClassesKey + `\` + protocolScheme + sub
}

// RegisterProtocol registers the scheme for the current user, with shell\open\command pointing at
// exe with "%1".
func RegisterProtocol(exePath string) error {
	exePath = strings.TrimSpace(exePath)
	if exePath == "" {
		return fmt.Errorf("empty exe path")
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, protocolKey(""), registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetStringValue("", "URL:SteamSwitch"); err != nil {
		return err
	}
	// The value's existence is the marker; Windows expects it empty.
	if err := k.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}

	cmdKey, _, err := registry.CreateKey(registry.CURRENT_USER, protocolKey(`\shell\open\command`), registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer cmdKey.Close()

	return cmdKey.SetStringValue("", fmt.Sprintf("\"%s\" \"%%1\"", exePath))
}

// UnregisterProtocol removes this user's registration, children first. Already gone is success. A
// machine-wide registration is not ours to touch.
func UnregisterProtocol() error {
	for _, sub := range []string{`\shell\open\command`, `\shell\open`, `\shell`, ""} {
		if err := registry.DeleteKey(registry.CURRENT_USER, protocolKey(sub)); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
	}
	return nil
}

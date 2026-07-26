package steam

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"steamswitch/internal/fsutil"

	"github.com/Jleagle/steam-go/steamvdf"
)

// registry.vdf — the Unix stand-in for HKCU\Software\Valve\Steam.
//
// On Windows the Steam client keeps AutoLoginUser, RememberPassword and friends in the actual
// registry. On macOS and Linux there is no registry, so it keeps the same key names in a VDF
// file at the root of its data directory, under a tree that mirrors the Windows hive:
//
//	"Registry" { "HKCU" { "Software" { "Valve" { "Steam" { "AutoLoginUser" "someone" ... } } } } }
//
// Everything here is a read-modify-write of that real tree. Rebuilding it from the handful of
// keys a switch owns would drop the rest — language, install paths, OOBE state, whatever the
// current client generation writes — for the same reason loginusers_edit.go exists.
//
// This file has no build tag. The parsing is ordinary VDF work and is worth testing on every
// platform; only the decision to *use* it is OS-specific.

// ErrRegistryVDFShape is returned when registry.vdf does not contain the expected hive.
//
// Refused rather than created: a file that parses but has no HKCU tree is not a Steam
// registry, and writing one into it would be inventing state for a client that never asked
// for it. The one exception is a file that does not exist at all, which is a legitimate
// first-run state and is handled by regVDFEnsurePath.
var ErrRegistryVDFShape = errors.New("registry.vdf: unrecognised structure")

// registryVDFPath is where Steam keeps it, relative to the data root.
func registryVDFPath(steamRoot string) string {
	return filepath.Join(steamRoot, "registry.vdf")
}

// steamHivePath is the key sequence from the file root down to the Steam hive.
var steamHivePath = []string{"HKCU", "Software", "Valve", "Steam"}

// registryVDF is a parsed registry.vdf.
type registryVDF struct {
	root steamvdf.KeyValue
}

// readRegistryVDF parses the file. A missing file yields an empty tree rather than an error,
// so a Steam install that has never been logged into can still be pointed at an account.
func readRegistryVDF(steamRoot string) (registryVDF, error) {
	path := registryVDFPath(steamRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return registryVDF{root: steamvdf.KeyValue{Key: "Registry"}}, nil
		}
		return registryVDF{}, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if len(bytes.TrimSpace(raw)) == 0 {
		return registryVDF{root: steamvdf.KeyValue{Key: "Registry"}}, nil
	}

	// steamvdf.ReadBytes returns only the first top-level block, which for this file is the
	// "Registry" wrapper containing everything else.
	root, err := steamvdf.ReadBytes(raw)
	if err != nil {
		return registryVDF{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(root.Key), "Registry") {
		return registryVDF{}, fmt.Errorf("%w: root key is %q", ErrRegistryVDFShape, root.Key)
	}
	return registryVDF{root: root}, nil
}

// childIdxCI finds a child by key, case-insensitively.
func childIdxCI(kv steamvdf.KeyValue, key string) int {
	for i := range kv.Children {
		if strings.EqualFold(strings.TrimSpace(kv.Children[i].Key), key) {
			return i
		}
	}
	return -1
}

// hive returns a pointer to the Steam hive node, creating the intermediate levels if the file
// is new. Returning a pointer into the tree is what makes the edit in-place and therefore
// lossless: every sibling key stays exactly where Steam left it.
func (r *registryVDF) hive() *steamvdf.KeyValue {
	node := &r.root
	for _, key := range steamHivePath {
		idx := childIdxCI(*node, key)
		if idx < 0 {
			node.Children = append(node.Children, steamvdf.KeyValue{Key: key})
			idx = len(node.Children) - 1
		}
		// A key that Steam wrote as a *value* where we expect a subtree means the file is not
		// shaped the way this code understands. Clearing the value is the only way to descend,
		// and it is safe precisely because the four names above are hive levels, never leaves.
		node.Children[idx].Value = ""
		node = &node.Children[idx]
	}
	return node
}

// Get reads one value out of the Steam hive. An absent key reads as "".
func (r *registryVDF) Get(key string) string {
	node := &r.root
	for _, level := range steamHivePath {
		idx := childIdxCI(*node, level)
		if idx < 0 {
			return ""
		}
		node = &node.Children[idx]
	}
	return childValueCI(*node, key)
}

// Set writes one value into the Steam hive, replacing any existing entry.
func (r *registryVDF) Set(key, value string) {
	setChildValueCI(r.hive(), key, value)
}

// Delete removes one value from the Steam hive. Reports whether it was there.
func (r *registryVDF) Delete(key string) bool {
	node := &r.root
	for _, level := range steamHivePath {
		idx := childIdxCI(*node, level)
		if idx < 0 {
			return false
		}
		node = &node.Children[idx]
	}
	idx := childIdxCI(*node, key)
	if idx < 0 {
		return false
	}
	node.Children = append(node.Children[:idx], node.Children[idx+1:]...)
	return true
}

// Write serialises the tree back over registry.vdf.
//
// Atomic, because Steam reads this file on every launch and a torn write would cost the user
// their whole client configuration, not just the account selection.
func (r *registryVDF) Write(steamRoot string) error {
	return fsutil.WriteFileAtomic(registryVDFPath(steamRoot), KeyValueToText(r.root), 0o644)
}

// steamPIDFromRegistry reads HKLM\...\Steam\SteamPID, which the client sets to its own process
// id while running and zeroes on a clean exit.
//
// Useful as a corroborating signal, never as the only one: a client killed with SIGKILL leaves
// a stale non-zero PID behind, so this can claim Steam is running long after it stopped. Any
// caller must confirm against the live process list.
func steamPIDFromRegistry(steamRoot string) (string, bool) {
	r, err := readRegistryVDF(steamRoot)
	if err != nil {
		return "", false
	}
	node := &r.root
	for _, level := range []string{"HKLM", "Software", "Valve", "Steam"} {
		idx := childIdxCI(*node, level)
		if idx < 0 {
			return "", false
		}
		node = &node.Children[idx]
	}
	pid := strings.TrimSpace(childValueCI(*node, "SteamPID"))
	if pid == "" || pid == "0" {
		return "", false
	}
	return pid, true
}

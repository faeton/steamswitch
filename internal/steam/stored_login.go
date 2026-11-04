package steam

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"os"
	"strings"

	"github.com/Jleagle/steam-go/steamvdf"
)

// Which accounts can sign in without a password.
//
// Steam keeps its login tokens in `local.vdf`, in a ConnectCache block keyed by CRC32(account
// name) + "1". Only the keys are used here. The values are the tokens, so this file keeps none of
// them, logs nothing and hands back booleans.
//
// A key being present does not prove the login still works: Steam can reject a stored token
// (measured: "27 (License expired)"), which only shows up after launch and is what signin_watch.go
// is for. A key being absent does mean a password prompt.

// connectCacheKeys returns the block's keys, lower-cased. ok is false when the file or the block
// cannot be read, which is "unknown" and must never be presented as "no stored login".
func connectCacheKeys(localVDF string) (keys map[string]struct{}, ok bool) {
	if strings.TrimSpace(localVDF) == "" {
		return nil, false
	}
	raw, err := os.ReadFile(localVDF)
	if err != nil {
		return nil, false
	}
	root, err := steamvdf.ReadBytes(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	if err != nil {
		return nil, false
	}
	block, found := findBlockCI(root, "ConnectCache", 8)
	if !found {
		return nil, false
	}
	keys = make(map[string]struct{}, len(block.Children))
	for _, entry := range block.Children {
		keys[strings.ToLower(strings.TrimSpace(entry.Key))] = struct{}{}
	}
	return keys, true
}

func findBlockCI(node steamvdf.KeyValue, name string, depth int) (steamvdf.KeyValue, bool) {
	if depth <= 0 {
		return steamvdf.KeyValue{}, false
	}
	for _, child := range node.Children {
		if strings.EqualFold(child.Key, name) {
			return child, true
		}
		if found, ok := findBlockCI(child, name, depth-1); ok {
			return found, true
		}
	}
	return steamvdf.KeyValue{}, false
}

// connectCacheKeyCandidates accepts the two spellings the hash can take: whether Steam pads a
// CRC with a leading zero could not be observed, as all five measured accounts had none.
func connectCacheKeyCandidates(accountName string) []string {
	sum := crc32.ChecksumIEEE([]byte(strings.ToLower(strings.TrimSpace(accountName))))
	return []string{fmt.Sprintf("%08x1", sum), fmt.Sprintf("%x1", sum)}
}

func hasConnectCacheEntry(keys map[string]struct{}, accountName string) bool {
	for _, k := range connectCacheKeyCandidates(accountName) {
		if _, ok := keys[k]; ok {
			return true
		}
	}
	return false
}

// accountsWithoutStoredLogin lists the login names Steam holds no token for.
//
// It answers only when it can stand behind the answer. If the block has entries and none of them
// match any account, the key scheme is not what this code expects (a client update, say), and
// flagging every account as needing a password would be a confident lie, so it reports nothing.
func accountsWithoutStoredLogin(localVDF string, accountNames []string) map[string]bool {
	out := map[string]bool{}
	keys, ok := connectCacheKeys(localVDF)
	if !ok {
		return out
	}
	matched := 0
	for _, name := range accountNames {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if hasConnectCacheEntry(keys, name) {
			matched++
		} else {
			out[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	if len(keys) > 0 && matched == 0 {
		return map[string]bool{}
	}
	return out
}

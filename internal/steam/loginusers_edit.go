package steam

import (
	"bytes"
	"errors"
	"os"
	"strings"

	"github.com/Jleagle/steam-go/steamvdf"
)

// Lossless editing of loginusers.vdf.
//
// [LoginUsersToKeyValue] rebuilds each account block from the eight fields [LoginUser]
// models, which silently drops anything else Steam wrote there. The set of fields is not
// fixed: it has grown across client generations, and a swap that round-trips the file would
// erase any field this build predates.
//
// These helpers instead parse the real tree and mutate only the keys a switch owns, leaving
// every other key — known or not — exactly where Steam put it. Field order is preserved too,
// since diffing the file is a normal way to debug a bad switch.

// ErrLoginUsersShape is returned when the file does not look like a list of account blocks.
//
// Refusing is the safe answer: the alternative is writing back a structure we did not
// understand, over the file that decides which account Steam logs into.
var ErrLoginUsersShape = errors.New("loginusers.vdf: unrecognised structure")

// loginUsersFile is a parsed loginusers.vdf plus where the account blocks live in it.
//
// `steamvdf.ReadBytes` returns only the first top-level block, so a normal file parses to a
// node keyed "users" whose children are the accounts — the wrapper is the root, not a child
// of it. Both that shape and a root that merely contains a "users" child are handled, because
// the reader has historically tolerated both and the writer must not silently restructure the
// file as a side effect of a switch.
type loginUsersFile struct {
	root steamvdf.KeyValue
	// usersIdx is the index of the "users" child, or -1 when root.Children are the blocks.
	usersIdx int
	// rootIsWrapper is true when root itself is the "users" node, so rendering emits it as-is.
	rootIsWrapper bool
}

func readLoginUsersTree(path string) (loginUsersFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return loginUsersFile{}, err
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	root, err := steamvdf.ReadBytes(raw)
	if err != nil {
		return loginUsersFile{}, err
	}

	// The common case: the root node is the "users" wrapper.
	if strings.EqualFold(strings.TrimSpace(root.Key), "users") {
		return loginUsersFile{root: root, usersIdx: -1, rootIsWrapper: true}, nil
	}
	// A root that contains a "users" child.
	for i, ch := range root.Children {
		if strings.EqualFold(strings.TrimSpace(ch.Key), "users") {
			return loginUsersFile{root: root, usersIdx: i}, nil
		}
	}
	// No wrapper, but the children are clearly account blocks.
	if len(root.Children) > 0 && looksLikeSteamID64(root.Children[0].Key) {
		return loginUsersFile{root: root, usersIdx: -1}, nil
	}
	return loginUsersFile{}, ErrLoginUsersShape
}

// blocks returns the per-account children, aliasing the tree so callers mutate in place.
func (f *loginUsersFile) blocks() []steamvdf.KeyValue {
	if f.usersIdx >= 0 {
		return f.root.Children[f.usersIdx].Children
	}
	return f.root.Children
}

func (f *loginUsersFile) setBlocks(b []steamvdf.KeyValue) {
	if f.usersIdx >= 0 {
		f.root.Children[f.usersIdx].Children = b
		return
	}
	f.root.Children = b
}

// render serializes back to text in the same shape it was read in.
func (f *loginUsersFile) render() []byte {
	if f.usersIdx >= 0 || f.rootIsWrapper {
		return KeyValueToText(f.root)
	}
	return KeyValueToText(steamvdf.KeyValue{Key: "users", Children: f.root.Children})
}

// setChildValueCI sets a key case-insensitively, appending it if absent.
//
// Appending rather than inserting at a fixed position matters: Steam writes these keys in a
// consistent order, and a file that has never had `MostRecent` should gain it at the end
// rather than have the rest of the block shuffled around it.
func setChildValueCI(block *steamvdf.KeyValue, key, value string) {
	for i := range block.Children {
		if strings.EqualFold(block.Children[i].Key, key) {
			block.Children[i].Value = value
			block.Children[i].Children = nil
			return
		}
	}
	block.Children = append(block.Children, steamvdf.KeyValue{Key: key, Value: value})
}

func childValueCI(block steamvdf.KeyValue, key string) string {
	for _, ch := range block.Children {
		if strings.EqualFold(ch.Key, key) {
			return ch.Value
		}
	}
	return ""
}

// applyLoginSelection points the file at one account, or at none when selectedID64 is empty.
//
// Returns the account name to write into the auto-login selector. An empty selection clears
// every marker, which is what "Add New" wants: Steam then shows the account chooser.
// rememberPassword controls whether the target account is left signed in. False is the
// public-machine case: Steam asks for the password on every launch instead.
func (f *loginUsersFile) applyLoginSelection(selectedID64 string, rememberPassword bool) string {
	selected := strings.TrimSpace(selectedID64)
	blocks := f.blocks()
	fields := f.activeMarkerFields()
	var autoUser string

	for i := range blocks {
		sid := strings.TrimSpace(blocks[i].Key)
		if sid == "" {
			continue
		}
		on := selected != "" && sid == selected
		for _, field := range fields {
			if on {
				// RememberPassword is the one marker the user gets a say in. The active-account
				// markers still have to be set, or the switch would not switch.
				if field == "RememberPassword" && !rememberPassword {
					setChildValueCI(&blocks[i], field, "0")
					continue
				}
				setChildValueCI(&blocks[i], field, "1")
				continue
			}
			// A switch owns the *target* account's "keep me signed in" flag and nobody
			// else's. Upstream cleared it on every other account too; the Steam client
			// itself does not — a real install with five remembered accounts has
			// RememberPassword=1 on all five. Clearing it made those accounts differ from
			// how Steam left them for no gain, in the direction of the failure mode that
			// matters most here: a switch that looks like a logout.
			if field == "RememberPassword" {
				continue
			}
			// Only clear a field the block already has. Adding "MostRecent" "0" to every
			// block of a file that never carried it would be a diff with no meaning.
			if childValueCI(blocks[i], field) != "" {
				setChildValueCI(&blocks[i], field, "0")
			}
		}
		if on {
			autoUser = childValueCI(blocks[i], "AccountName")
		}
	}

	f.setBlocks(blocks)
	return autoUser
}

// activeMarkerFields picks which "this is the live account" keys to write.
//
// Steam has used two markers over time and [ActiveSessionSteamID64] reads AutoLogin in
// preference to MostRecent. Writing both unconditionally would stamp a legacy MostRecent key
// onto modern files that have never carried one — a field this build invented, which is the
// same class of change this file exists to avoid. So mirror whatever the file already uses,
// and fall back to the modern marker for a file that uses neither.
func (f *loginUsersFile) activeMarkerFields() []string {
	var usesMostRecent, usesAutoLogin bool
	for _, b := range f.blocks() {
		if childValueCI(b, "MostRecent") != "" {
			usesMostRecent = true
		}
		if childValueCI(b, "AutoLogin") != "" {
			usesAutoLogin = true
		}
	}

	// RememberPassword is not an active-account marker, but the switch has always owned it:
	// the target account must be set to remember, so Steam does not prompt on next launch.
	fields := []string{"RememberPassword"}
	if usesAutoLogin || !usesMostRecent {
		fields = append(fields, "AutoLogin")
	}
	if usesMostRecent {
		fields = append(fields, "MostRecent")
	}
	return fields
}

// removeAccount drops one account, preserving every remaining block verbatim.
func (f *loginUsersFile) removeAccount(steamID64 string) {
	target := strings.TrimSpace(steamID64)
	blocks := f.blocks()
	kept := make([]steamvdf.KeyValue, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b.Key) == target {
			continue
		}
		kept = append(kept, b)
	}
	f.setBlocks(kept)
}

package sessionkit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Content manifests — the basis for every safety claim this package makes.
//
// Sizes alone are not enough: an edited config very often has the same length as the one it
// replaced, so `DotaSnapshot.SizeBytes` cannot detect a change. Every regular file therefore
// gets its own SHA-256, and a part is summarised by a digest over the canonical record
// stream so the journal can stay small while `manifest.json` keeps the per-file detail the
// "what changed" view needs.

// PartState distinguishes the three states a part can be in. Absent and empty are *not*
// the same thing: restoring a part that was absent must delete the live directory, while
// restoring one that was empty must leave an empty directory behind.
type PartState string

const (
	PartAbsent PartState = "absent"
	PartEmpty  PartState = "empty"
	PartFilled PartState = "filled"
)

// FileRecord is one regular file inside a part.
type FileRecord struct {
	// Path is relative to the part root, always with forward slashes.
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// PartManifest is the hashed content of a single part.
type PartManifest struct {
	PartID string       `json:"partId"`
	State  PartState    `json:"state"`
	Files  []FileRecord `json:"files"`
	// Digest is a SHA-256 over the canonical record stream; it is what the journal stores.
	Digest string `json:"digest"`
}

// Manifest is every part of one module, keyed by part id.
type Manifest struct {
	ModuleID string                  `json:"moduleId"`
	Parts    map[string]PartManifest `json:"parts"`
}

// FileDiff is one entry in a "what changed" comparison.
type FileDiff struct {
	PartID string `json:"partId"`
	Path   string `json:"path"`
	// Kind is "added", "removed" or "changed", from the perspective of `current` versus
	// `expected`.
	Kind string `json:"kind"`
}

// ErrUnsupportedEntry is returned when a part contains something that cannot be hashed
// meaningfully — a symlink, junction or device node. Rather than silently skipping it (and
// so declaring a tree unchanged when it is not), the manifest refuses to be built.
var ErrUnsupportedEntry = errors.New("Toast_Kit_UnsupportedEntry")

// ErrNonUTF8Path is returned for a path that cannot be recorded canonically.
var ErrNonUTF8Path = errors.New("Toast_Kit_BadPath")

// HashPart walks one part directory and produces its manifest.
//
// A missing directory yields PartAbsent rather than an error: "this account has no Dota
// config yet" is a normal state that restore has to be able to reproduce.
func HashPart(partID, root string) (PartManifest, error) {
	pm := PartManifest{PartID: partID, State: PartAbsent, Files: []FileRecord{}}

	info, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			pm.Digest = digestOf(pm)
			return pm, nil
		}
		return PartManifest{}, err
	}
	if !info.IsDir() {
		return PartManifest{}, fmt.Errorf("%w: %s is not a directory", ErrUnsupportedEntry, root)
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// WalkDir hands us the lstat-equivalent type, so a symlink shows up as
		// ModeSymlink rather than as its target. Reject it: following it would hash
		// content living outside the part, and ignoring it would under-report changes.
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return fmt.Errorf("%w: %s", ErrUnsupportedEntry, path)
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !utf8.ValidString(rel) {
			return fmt.Errorf("%w: %s", ErrNonUTF8Path, path)
		}

		sum, size, err := hashFile(path)
		if err != nil {
			return err
		}
		pm.Files = append(pm.Files, FileRecord{Path: rel, Size: size, SHA256: sum})
		return nil
	})
	if walkErr != nil {
		return PartManifest{}, walkErr
	}

	sort.Slice(pm.Files, func(i, j int) bool { return pm.Files[i].Path < pm.Files[j].Path })
	if len(pm.Files) == 0 {
		pm.State = PartEmpty
	} else {
		pm.State = PartFilled
	}
	pm.Digest = digestOf(pm)
	return pm, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// digestOf hashes the canonical record stream:
//
//	state "\0" relativePath "\0" decimalSize "\0" fileSHA256 "\n"
//
// The state is included so that an absent part and an empty part get different digests.
func digestOf(pm PartManifest) string {
	h := sha256.New()
	var b strings.Builder
	b.WriteString(string(pm.State))
	b.WriteByte(0)
	h.Write([]byte(b.String()))
	for _, f := range pm.Files {
		fmt.Fprintf(h, "%s\x00%d\x00%s\n", f.Path, f.Size, f.SHA256)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// HashParts builds a manifest for a whole module given a resolver from part id to path.
// A resolver returning ok=false marks the part absent, which is how an uninstalled game or
// a machine-wide part that does not apply is represented.
func HashParts(moduleID string, parts []Part, resolve func(partID string) (string, bool)) (Manifest, error) {
	m := Manifest{ModuleID: moduleID, Parts: map[string]PartManifest{}}
	for _, p := range parts {
		path, ok := resolve(p.ID)
		if !ok {
			absent := PartManifest{PartID: p.ID, State: PartAbsent, Files: []FileRecord{}}
			absent.Digest = digestOf(absent)
			m.Parts[p.ID] = absent
			continue
		}
		pm, err := HashPart(p.ID, path)
		if err != nil {
			return Manifest{}, err
		}
		m.Parts[p.ID] = pm
	}
	return m, nil
}

// Digests flattens a manifest to the compact `partID -> digest` map the journal stores.
func (m Manifest) Digests() map[string]string {
	out := make(map[string]string, len(m.Parts))
	for id, pm := range m.Parts {
		out[id] = pm.Digest
	}
	return out
}

// Equal reports whether two manifests describe identical content.
func (m Manifest) Equal(other Manifest) bool {
	if len(m.Parts) != len(other.Parts) {
		return false
	}
	for id, pm := range m.Parts {
		op, ok := other.Parts[id]
		if !ok || op.Digest != pm.Digest {
			return false
		}
	}
	return true
}

// Diff lists every file-level difference between an expected manifest and the current one.
//
// Only parts present in `expected` are compared: the engine always builds both manifests
// from the same plan, so a part missing from one side is a bug rather than a user-visible
// change, and reporting it as a diff would be noise.
func (expected Manifest) Diff(current Manifest) []FileDiff {
	var diffs []FileDiff
	partIDs := make([]string, 0, len(expected.Parts))
	for id := range expected.Parts {
		partIDs = append(partIDs, id)
	}
	sort.Strings(partIDs)

	for _, id := range partIDs {
		want := expected.Parts[id]
		got, ok := current.Parts[id]
		if !ok {
			diffs = append(diffs, FileDiff{PartID: id, Path: "", Kind: "removed"})
			continue
		}
		if want.Digest == got.Digest {
			continue
		}
		// A part that appeared or vanished wholesale is one diff, not one per file.
		if want.State == PartAbsent || got.State == PartAbsent {
			kind := "added"
			if got.State == PartAbsent {
				kind = "removed"
			}
			diffs = append(diffs, FileDiff{PartID: id, Path: "", Kind: kind})
			continue
		}
		diffs = append(diffs, diffFiles(id, want.Files, got.Files)...)
	}
	return diffs
}

func diffFiles(partID string, want, got []FileRecord) []FileDiff {
	wantBy := make(map[string]FileRecord, len(want))
	for _, f := range want {
		wantBy[f.Path] = f
	}
	gotBy := make(map[string]FileRecord, len(got))
	for _, f := range got {
		gotBy[f.Path] = f
	}

	var diffs []FileDiff
	for _, f := range want {
		g, ok := gotBy[f.Path]
		if !ok {
			diffs = append(diffs, FileDiff{PartID: partID, Path: f.Path, Kind: "removed"})
			continue
		}
		if g.SHA256 != f.SHA256 {
			diffs = append(diffs, FileDiff{PartID: partID, Path: f.Path, Kind: "changed"})
		}
	}
	for _, f := range got {
		if _, ok := wantBy[f.Path]; !ok {
			diffs = append(diffs, FileDiff{PartID: partID, Path: f.Path, Kind: "added"})
		}
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs
}

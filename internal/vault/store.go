package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"steamswitch/internal/paths"
	"steamswitch/internal/security"
)

const (
	// Frozen wire-format constants. Both are baked into every blob ever written; renaming
	// either makes existing vaults undecryptable. New kinds of sealed data get new strings
	// rather than reusing these.
	subKeyInfo = "steamswitch-secrets-v1"
	blobAAD    = "steamswitch-secrets-v1"

	blobVersion = 1
	dirName     = "Vault"
	fileName    = "secrets.ssvault"
)

// Errors are i18n keys, per the convention in internal/steam: the frontend prefixes them
// with "i18n:" and the tray runs them through the translator. Wrapping one with %w and a
// second clause would put an untranslated tail in front of the user, so failures that carry
// detail carry it in a log line instead.
var (
	ErrLocked       = errors.New("Toast_Vault_Locked")
	ErrNotFound     = errors.New("Toast_Vault_EntryNotFound")
	ErrNoSteamID    = errors.New("Toast_Vault_SteamIDRequired")
	ErrCorrupt      = errors.New("Toast_Vault_Corrupt")
	ErrUnknownField = errors.New("Toast_Vault_UnknownField")
)

// sealedBlob is the on-disk envelope. UpdatedAt is the only cleartext metadata; everything
// else about the vault, including how many accounts are in it, is only inferable from the
// ciphertext length.
type sealedBlob struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	KDFInfo    string `json:"kdfInfo"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	UpdatedAt  string `json:"updatedAt"`
}

// The decrypted document is cached in memory and dropped when the app locks. Loaded is not
// the same as "the file exists": a vault that has never been written loads as an empty
// document so the first Put does not have to special-case creation.
var (
	cacheMu sync.RWMutex
	cached  *Doc

	// writeMu serialises read-modify-write of the whole blob. The frontend's busy flag is
	// not a lock — the tray and the CLI reach these paths too.
	writeMu sync.Mutex
)

func vaultDir() (string, error) {
	root, err := paths.DataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, dirName), nil
}

func vaultPath() (string, error) {
	dir, err := vaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Exists reports whether a vault file is present, without needing the app unlocked. The
// settings UI uses it to decide between "enable the vault" and "the vault is locked".
func Exists() bool {
	p, err := vaultPath()
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Size() > 0
}

// DropCache forgets the decrypted document. Wired to the security status hook so locking
// the app actually removes the secrets from memory rather than merely refusing to render
// them.
func DropCache() {
	cacheMu.Lock()
	cached = nil
	cacheMu.Unlock()
}

// load returns the decrypted document, reading and decrypting it on first use after unlock.
func load() (*Doc, error) {
	cacheMu.RLock()
	d := cached
	cacheMu.RUnlock()
	if d != nil {
		return d, nil
	}

	if err := security.RequireUnlocked(); err != nil {
		return nil, ErrLocked
	}
	key, err := security.SubKey(subKeyInfo)
	if err != nil {
		return nil, ErrLocked
	}

	p, err := vaultPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		fresh := &Doc{Version: Version, Entries: map[string]Entry{}}
		cacheMu.Lock()
		cached = fresh
		cacheMu.Unlock()
		return fresh, nil
	}
	if err != nil {
		return nil, err
	}

	doc, err := decodeBlob(raw, key)
	if err != nil {
		return nil, err
	}
	cacheMu.Lock()
	cached = doc
	cacheMu.Unlock()
	return doc, nil
}

func decodeBlob(raw, key []byte) (*Doc, error) {
	var blob sealedBlob
	if err := json.Unmarshal(raw, &blob); err != nil {
		return nil, ErrCorrupt
	}
	if blob.Version != blobVersion {
		return nil, ErrCorrupt
	}
	nonce, err := base64.StdEncoding.DecodeString(blob.Nonce)
	if err != nil {
		return nil, ErrCorrupt
	}
	ct, err := base64.StdEncoding.DecodeString(blob.Ciphertext)
	if err != nil {
		return nil, ErrCorrupt
	}
	// A wrong key, a truncated file and a tampered ciphertext are one indistinguishable
	// failure here, which is what AEAD is for. It is reported as corruption rather than as
	// a decrypt error so no oracle is offered.
	plain, err := security.Open(key, nonce, ct, []byte(blobAAD))
	if err != nil {
		return nil, ErrCorrupt
	}
	var doc Doc
	if err := json.Unmarshal(plain, &doc); err != nil {
		return nil, ErrCorrupt
	}
	if doc.Entries == nil {
		doc.Entries = map[string]Entry{}
	}
	return &doc, nil
}

// save seals and writes the whole document. Callers hold writeMu.
func save(doc *Doc) error {
	if err := security.RequireUnlocked(); err != nil {
		return ErrLocked
	}
	key, err := security.SubKey(subKeyInfo)
	if err != nil {
		return ErrLocked
	}
	doc.Version = Version
	if doc.Entries == nil {
		doc.Entries = map[string]Entry{}
	}
	plain, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	nonce, ct, err := security.Seal(key, plain, []byte(blobAAD))
	if err != nil {
		return err
	}
	blob := sealedBlob{
		Version:    blobVersion,
		Algorithm:  "AES-256-GCM",
		KDFInfo:    subKeyInfo,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	out, err := json.MarshalIndent(blob, "", "  ")
	if err != nil {
		return err
	}
	dir, err := vaultDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p := filepath.Join(dir, fileName)
	if err := security.WriteSecretFile(p, out); err != nil {
		return err
	}
	cacheMu.Lock()
	cached = doc
	cacheMu.Unlock()
	return nil
}

// mutate runs fn against the live document under the write lock and persists the result.
// Every write goes through here, so there is exactly one place that can forget to save.
func mutate(fn func(doc *Doc) error) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	doc, err := load()
	if err != nil {
		return err
	}
	// Copy before mutating: a fn that fails halfway must not leave the cached document
	// holding changes that were never written.
	//
	// Every field has to be copied, not just Entries. This once copied Version and Entries
	// alone, which meant any write — saving an entry, recording a health check — silently
	// dropped whatever else the document held. Adding a field to Doc and forgetting a line
	// here does not fail to compile and does not fail to save; it just quietly erases data
	// on the next unrelated write.
	next := &Doc{
		Version:         doc.Version,
		Entries:         make(map[string]Entry, len(doc.Entries)),
		Exports:         append([]ExportRecord(nil), doc.Exports...),
		ImportedBundles: make(map[string]string, len(doc.ImportedBundles)),
	}
	for k, v := range doc.Entries {
		next.Entries[k] = v
	}
	for k, v := range doc.ImportedBundles {
		next.ImportedBundles[k] = v
	}
	if err := fn(next); err != nil {
		return err
	}
	return save(next)
}

func normID(id string) string { return strings.TrimSpace(id) }

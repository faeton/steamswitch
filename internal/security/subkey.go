package security

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
)

// This file is the seam other packages use to store their own secrets. It exists so
// `internal/vault` never reimplements AES-GCM, never reaches for the master key directly,
// and never invents its own atomic-write story — the three places a secrets store goes
// wrong quietly.

// ErrEmptySubKeyInfo guards the one way SubKey can be misused: an empty label would make
// every caller share one key, which is the whole thing separate subkeys exist to prevent.
var ErrEmptySubKeyInfo = errors.New("subkey info label must not be empty")

// SubKey derives a purpose-scoped 32-byte key from the unlocked master key.
//
// The label is the HKDF `info` string and must be unique per purpose and stable forever —
// changing it makes everything sealed under the old one undecryptable. Callers own their
// label as a frozen constant, exactly as the AAD strings above are frozen.
//
// Deriving rather than reusing the master key means a future export or re-key of one
// subsystem cannot expose another's data.
func SubKey(info string) ([]byte, error) {
	if strings.TrimSpace(info) == "" {
		return nil, ErrEmptySubKeyInfo
	}
	// The vault gate, not just the app gate. When saved-account encryption is on, a vault lock
	// cannot drop the master key (account blobs are sealed under it), so this check is the
	// only thing standing between a locked vault and its own secrets. Checking it here rather
	// than in each of internal/vault's callers means a new caller cannot forget it.
	if err := defaultManager.requireVaultUnlocked(); err != nil {
		return nil, err
	}
	master, err := defaultManager.unlockedMasterKey()
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, master, nil, info, vaultKeyBytes)
}

// Seal encrypts with AES-256-GCM under a caller-supplied key, returning the random nonce
// separately so callers can store the two fields as they please.
func Seal(key, plaintext, aad []byte) (nonce, ciphertext []byte, err error) {
	return sealWithKey(key, plaintext, aad)
}

// Open reverses Seal. A wrong key, a tampered ciphertext and a mismatched AAD are all one
// indistinguishable failure, which is the point.
func Open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	return openWithKey(key, nonce, ciphertext, aad)
}

// WriteSecretFile writes owner-only and fsyncs the containing directory, so a crash between
// the rename and the directory flush cannot leave a secrets file that exists in the page
// cache but not on disk. Plain fsutil.WriteFileAtomic does not do the directory flush.
func WriteSecretFile(path string, data []byte) error {
	return writeFileAtomicDurable(path, data, 0o600)
}

// DeriveFromPassphrase derives a key from a passphrase that is *not* the app password —
// today that means a handoff bundle, which has to open on a machine whose Security/ dir
// knows nothing about this one. It calibrates fresh and returns the parameters so they can
// be stored alongside the ciphertext; the recipient re-derives with DeriveWithParams.
func DeriveFromPassphrase(passphrase string, salt []byte) (KDFParams, []byte) {
	return calibrateAndDeriveKey(passphrase, salt)
}

// DeriveWithParams re-derives a passphrase key using parameters that travelled with the
// data. Parameters are normalised first: a bundle claiming absurd cost must not be able to
// hang the importer.
func DeriveWithParams(passphrase string, salt []byte, p KDFParams) []byte {
	return deriveKey(passphrase, salt, p)
}

// RandomBytes exposes the same CSPRNG helper the package uses internally, so callers that
// need a salt or a nonce do not each pick their own source.
func RandomBytes(n int) ([]byte, error) {
	return randomBytes(n)
}

// SecretFilePerm is the mode every file holding a secret is written with. Exported so tests
// elsewhere can assert it rather than hard-coding an octal.
const SecretFilePerm os.FileMode = 0o600

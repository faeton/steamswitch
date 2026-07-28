package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"steamswitch/internal/paths"

	"golang.org/x/crypto/argon2"
)

const (
	securityVersion = 1
	vaultKeyBytes   = 32

	securityDirName  = "Security"
	securityFileName = "security.json"

	// Frozen wire-format constants: these AAD strings are baked into every
	// encrypted vault created since the first release. Renaming them would make
	// existing user data undecryptable, so they keep the legacy "tcno" prefix.
	securityVerifierAAD = "tcno-security-verifier-v1"
	wrappedKeyAAD       = "tcno-security-vault-key-v1"

	kdfTargetMillis = 300
	kdfMaxTime      = 8

	// Ceilings for parameters that may have arrived from another machine — see
	// normalizeKDFParams. 1 GB and 8 threads are far above anything calibrate produces and
	// far below anything that takes a desktop down.
	kdfMaxMemoryKB = 1024 * 1024
	kdfMaxThreads  = 8
)

var (
	ErrLocked             = errors.New("app is locked")
	ErrPasswordNotSet     = errors.New("app password is not set")
	ErrInvalidPassword    = errors.New("invalid app password")
	ErrPasswordAlreadySet = errors.New("app password is already set")
	ErrOperationBusy      = errors.New("a security operation is in progress")
)

type Status struct {
	AppPasswordSet            bool `json:"appPasswordSet"`
	AppLocked                 bool `json:"appLocked"`
	SavedAccountDataEncrypted bool `json:"savedAccountDataEncrypted"`
	OperationBusy             bool `json:"operationBusy"`
	QuarantineCount           int  `json:"quarantineCount"`
	InterruptedRestorePending bool `json:"interruptedRestorePending"`
}

type KDFParams struct {
	Algorithm      string `json:"algorithm"`
	Time           uint32 `json:"time"`
	MemoryKB       uint32 `json:"memoryKb"`
	Threads        uint8  `json:"threads"`
	KeyLen         uint32 `json:"keyLen"`
	TargetMillis   uint32 `json:"targetMillis,omitempty"`
	MeasuredMillis uint32 `json:"measuredMillis,omitempty"`
}

type securityFile struct {
	Version                   int       `json:"version"`
	KDF                       KDFParams `json:"kdf"`
	Salt                      string    `json:"salt"`
	VerifierNonce             string    `json:"verifierNonce"`
	VerifierCiphertext        string    `json:"verifierCiphertext"`
	WrappedVaultKeyNonce      string    `json:"wrappedVaultKeyNonce"`
	WrappedVaultKeyCiphertext string    `json:"wrappedVaultKeyCiphertext"`
	SavedAccountDataEncrypted bool      `json:"savedAccountDataEncrypted"`
}

type manager struct {
	mu            sync.Mutex
	masterKey     []byte
	operationBusy bool
}

var (
	defaultManager = &manager{}
	statusHookMu   sync.Mutex
	statusHook     func()
)

func SetStatusChangedHook(fn func()) {
	statusHookMu.Lock()
	statusHook = fn
	statusHookMu.Unlock()
}

func emitStatusChanged() {
	statusHookMu.Lock()
	fn := statusHook
	statusHookMu.Unlock()
	if fn != nil {
		fn()
	}
}

func defaultKDFParams() KDFParams {
	return KDFParams{
		Algorithm:    "argon2id",
		Time:         2,
		MemoryKB:     64 * 1024,
		Threads:      1,
		KeyLen:       vaultKeyBytes,
		TargetMillis: kdfTargetMillis,
	}
}

func GetStatus() (Status, error) {
	return defaultManager.status()
}

func SetAppPassword(password string) error {
	return defaultManager.setAppPassword(password)
}

func UnlockApp(password string) error {
	return defaultManager.unlockApp(password)
}

func LockApp() error {
	return defaultManager.lockApp()
}

func RemoveAppPassword(password string) error {
	return defaultManager.removeAppPassword(password)
}

func RequireUnlocked() error {
	return defaultManager.requireUnlocked()
}

func AppLocked() bool {
	st, err := defaultManager.status()
	return err == nil && st.AppLocked
}

func SavedAccountDataEncrypted() bool {
	st, err := defaultManager.status()
	return err == nil && st.SavedAccountDataEncrypted
}

func securityDir() (string, error) {
	root, err := paths.DataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, securityDirName), nil
}

func securityPath() (string, error) {
	dir, err := securityDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, securityFileName), nil
}

func (m *manager) status() (Status, error) {
	sf, ok, err := loadSecurityFile()
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{
		AppPasswordSet:            ok,
		AppLocked:                 ok && len(m.masterKey) == 0,
		OperationBusy:             m.operationBusy,
		QuarantineCount:           countQuarantines(),
		InterruptedRestorePending: hasInterruptedRestoreJournal(),
	}
	if ok {
		st.SavedAccountDataEncrypted = sf.SavedAccountDataEncrypted
	}
	return st, nil
}

func (m *manager) setAppPassword(password string) error {
	if _, ok, err := loadSecurityFile(); err != nil {
		return err
	} else if ok {
		return ErrPasswordAlreadySet
	}
	salt, err := randomBytes(16)
	if err != nil {
		return err
	}
	kdf, derived := calibrateAndDeriveKey(password, salt)
	master, err := randomBytes(vaultKeyBytes)
	if err != nil {
		return err
	}
	verifierNonce, verifierCipher, err := sealWithKey(derived, []byte("tcno-security-ok"), []byte(securityVerifierAAD))
	if err != nil {
		return err
	}
	wrapNonce, wrapped, err := sealWithKey(derived, master, []byte(wrappedKeyAAD))
	if err != nil {
		return err
	}
	sf := securityFile{
		Version:                   securityVersion,
		KDF:                       kdf,
		Salt:                      encode(salt),
		VerifierNonce:             encode(verifierNonce),
		VerifierCiphertext:        encode(verifierCipher),
		WrappedVaultKeyNonce:      encode(wrapNonce),
		WrappedVaultKeyCiphertext: encode(wrapped),
	}
	if err := saveSecurityFile(sf); err != nil {
		return err
	}
	m.mu.Lock()
	m.masterKey = append([]byte(nil), master...)
	m.mu.Unlock()
	emitStatusChanged()
	return nil
}

func (m *manager) unlockApp(password string) error {
	key, err := unlockWithPassword(password)
	if err != nil {
		return err
	}
	m.mu.Lock()
	// A copy, not the caller's slice. `lockApp` zeroes the backing array it holds, so sharing
	// one array between the manager and a caller means locking can scribble over a key that
	// caller is still using. Cheap insurance for a 32-byte slice.
	m.masterKey = append([]byte(nil), key...)
	m.mu.Unlock()
	emitStatusChanged()
	return nil
}

// adoptKeyForOperation installs `key` and claims the busy latch in a single critical section,
// for the long-running encrypt/decrypt paths.
//
// Doing it in two steps had a real hole: between installing the key and setting the latch,
// `lockApp` could observe an idle manager, zero the master key — and because the two shared
// one backing array, zero the operation's own `key` with it. The operation then carried on and
// wrote account blobs sealed under 32 zero bytes.
//
// Returns ErrOperationBusy if another operation already holds the latch. Callers must pair
// this with `finishOperation`.
func (m *manager) adoptKeyForOperation(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.operationBusy {
		return ErrOperationBusy
	}
	m.masterKey = append([]byte(nil), key...)
	m.operationBusy = true
	return nil
}

func (m *manager) finishOperation() {
	m.setBusy(false)
}

// lockApp drops the in-memory master key, putting the app back behind the unlock gate
// without a restart. The exact inverse of unlockApp: the key is the only unlocked state
// there is, and status() already derives AppLocked from its length.
//
// Refuses while an operation is in flight. Encrypting or decrypting the saved-account cache
// holds the key for the length of the run, and pulling it out from under a half-finished
// re-encrypt is how a cache ends up unreadable. Callers (auto-lock timers, "Lock now") treat
// the refusal as "try again shortly", not as an error worth showing.
//
// No password is required. Locking removes access; only unlocking grants it.
func (m *manager) lockApp() error {
	_, ok, err := loadSecurityFile()
	if err != nil {
		return err
	}
	if !ok {
		return ErrPasswordNotSet
	}

	m.mu.Lock()
	if m.operationBusy {
		m.mu.Unlock()
		return ErrOperationBusy
	}
	// Zero before dropping the reference: the backing array may outlive the slice header in
	// a heap the GC has not swept, and this key unwraps the vault.
	for i := range m.masterKey {
		m.masterKey[i] = 0
	}
	m.masterKey = nil
	m.mu.Unlock()

	emitStatusChanged()
	return nil
}

func (m *manager) removeAppPassword(password string) error {
	sf, ok, err := loadSecurityFile()
	if err != nil {
		return err
	}
	if !ok {
		return ErrPasswordNotSet
	}
	key, err := unlockWithPassword(password)
	if err != nil {
		return err
	}
	journal, err := writeJournal("remove-password", map[string]any{
		"encrypted": sf.SavedAccountDataEncrypted,
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.masterKey = key
	m.mu.Unlock()
	if sf.SavedAccountDataEncrypted {
		if err := disableSavedAccountEncryptionWithKey(password, key, true); err != nil {
			_ = os.Remove(journal)
			return err
		}
	}
	p, err := securityPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(journal)
		return err
	}
	_ = os.Remove(journal)
	m.mu.Lock()
	m.masterKey = nil
	m.mu.Unlock()
	emitStatusChanged()
	return nil
}

func (m *manager) requireUnlocked() error {
	st, err := m.status()
	if err != nil {
		return err
	}
	if st.AppLocked {
		return ErrLocked
	}
	return nil
}

func (m *manager) unlockedMasterKey() ([]byte, error) {
	if err := m.requireUnlocked(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.masterKey) == 0 {
		return nil, ErrLocked
	}
	return append([]byte(nil), m.masterKey...), nil
}

func (m *manager) setBusy(v bool) {
	m.mu.Lock()
	m.operationBusy = v
	m.mu.Unlock()
	emitStatusChanged()
}

func unlockWithPassword(password string) ([]byte, error) {
	sf, ok, err := loadSecurityFile()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrPasswordNotSet
	}
	salt, err := decode(sf.Salt)
	if err != nil {
		return nil, err
	}
	derived := deriveKey(password, salt, sf.KDF)
	verifierNonce, err := decode(sf.VerifierNonce)
	if err != nil {
		return nil, err
	}
	verifierCipher, err := decode(sf.VerifierCiphertext)
	if err != nil {
		return nil, err
	}
	if _, err := openWithKey(derived, verifierNonce, verifierCipher, []byte(securityVerifierAAD)); err != nil {
		return nil, ErrInvalidPassword
	}
	wrapNonce, err := decode(sf.WrappedVaultKeyNonce)
	if err != nil {
		return nil, err
	}
	wrapped, err := decode(sf.WrappedVaultKeyCiphertext)
	if err != nil {
		return nil, err
	}
	master, err := openWithKey(derived, wrapNonce, wrapped, []byte(wrappedKeyAAD))
	if err != nil {
		return nil, ErrInvalidPassword
	}
	if len(master) != vaultKeyBytes {
		return nil, fmt.Errorf("invalid vault key length")
	}
	return master, nil
}

func deriveKey(password string, salt []byte, p KDFParams) []byte {
	p = normalizeKDFParams(p)
	return argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKB, p.Threads, p.KeyLen)
}

func normalizeKDFParams(p KDFParams) KDFParams {
	def := defaultKDFParams()
	if p.Algorithm == "" {
		p.Algorithm = def.Algorithm
	}
	if p.Time == 0 {
		p.Time = def.Time
	}
	if p.MemoryKB == 0 {
		p.MemoryKB = def.MemoryKB
	}
	if p.Threads == 0 {
		p.Threads = def.Threads
	}
	if p.KeyLen == 0 {
		p.KeyLen = def.KeyLen
	}
	if p.TargetMillis == 0 {
		p.TargetMillis = def.TargetMillis
	}

	// Upper bounds, not just defaults for zeros. These parameters do not always come from
	// this machine: a handoff bundle carries its own so the recipient can re-derive, and a
	// bundle is a file that arrived from somebody else. Argon2 does exactly what it is told,
	// so a bundle claiming 4 TB of memory and a million passes is a way to hang or OOM the
	// importer before they have agreed to anything.
	//
	// Clamping down rather than rejecting is deliberate: the wrong parameters simply derive
	// the wrong key, and a wrong key is already an indistinguishable failure. The bound only
	// has to stop the machine falling over.
	if p.Time > kdfMaxTime {
		p.Time = kdfMaxTime
	}
	if p.MemoryKB > kdfMaxMemoryKB {
		p.MemoryKB = kdfMaxMemoryKB
	}
	if p.Threads > kdfMaxThreads {
		p.Threads = kdfMaxThreads
	}
	if p.KeyLen != vaultKeyBytes {
		// Every consumer of this key wants 32 bytes for AES-256. A bundle asking for
		// anything else is either corrupt or probing, and honouring it produces a key
		// nothing can use.
		p.KeyLen = vaultKeyBytes
	}
	return p
}

func calibrateAndDeriveKey(password string, salt []byte) (KDFParams, []byte) {
	p := normalizeKDFParams(defaultKDFParams())
	p.Time = 1
	start := time.Now()
	key := deriveKey(password, salt, p)
	singleMillis := elapsedMillis(start)
	target := p.TargetMillis
	if target == 0 {
		target = kdfTargetMillis
	}
	nextTime := uint32(1)
	if singleMillis > 0 {
		nextTime = (target + singleMillis - 1) / singleMillis
	}
	if nextTime < 1 {
		nextTime = 1
	}
	if nextTime > kdfMaxTime {
		nextTime = kdfMaxTime
	}
	p.Time = nextTime
	if nextTime == 1 {
		p.MeasuredMillis = singleMillis
		return p, key
	}
	start = time.Now()
	key = deriveKey(password, salt, p)
	p.MeasuredMillis = elapsedMillis(start)
	return p, key
}

func elapsedMillis(start time.Time) uint32 {
	ms := time.Since(start).Milliseconds()
	if ms < 1 {
		return 1
	}
	if ms > int64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(ms)
}

func loadSecurityFile() (securityFile, bool, error) {
	p, err := securityPath()
	if err != nil {
		return securityFile{}, false, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return securityFile{}, false, nil
		}
		return securityFile{}, false, err
	}
	var sf securityFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return securityFile{}, false, err
	}
	if sf.Version != securityVersion {
		return securityFile{}, false, fmt.Errorf("unsupported security file version %d", sf.Version)
	}
	return sf, true, nil
}

func saveSecurityFile(sf securityFile) error {
	p, err := securityPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(p, append(data, '\n'), 0o600)
}

func updateSecurityFile(fn func(*securityFile) error) error {
	sf, ok, err := loadSecurityFile()
	if err != nil {
		return err
	}
	if !ok {
		return ErrPasswordNotSet
	}
	if err := fn(&sf); err != nil {
		return err
	}
	return saveSecurityFile(sf)
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func sealWithKey(key, plaintext, aad []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce, err := randomBytes(aead.NonceSize())
	if err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}

func openWithKey(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

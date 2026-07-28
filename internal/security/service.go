package security

type SecurityService struct{}

func NewService() *SecurityService {
	return &SecurityService{}
}

func (s *SecurityService) GetSecurityStatus() (Status, error) {
	return GetStatus()
}

func (s *SecurityService) SetAppPassword(password string) error {
	return SetAppPassword(password)
}

func (s *SecurityService) UnlockApp(password string) error {
	return UnlockApp(password)
}

// LockApp puts the *whole app* back behind the unlock gate without a restart. Takes no
// password: locking removes access.
//
// Not what the "lock vault" control calls — see LockVault. This seals the account paths too,
// so the user cannot switch until they type the password again.
func (s *SecurityService) LockApp() error {
	return LockApp()
}

// LockVault seals the credential vault and leaves the rest of the app usable, which is what
// REDESIGN_BRIEF.md means by the global "lock vault" action (A10, and the chrome list under A6).
//
// Takes no password, for the same reason LockApp does not.
func (s *SecurityService) LockVault() error {
	return LockVault()
}

// UnlockVault reopens the vault. Requires the app password even when the master key never left
// memory: a lock the user can walk past without it is not a lock.
func (s *SecurityService) UnlockVault(password string) error {
	return UnlockVault(password)
}

func (s *SecurityService) RemoveAppPassword(password string) error {
	return RemoveAppPassword(password)
}

func (s *SecurityService) EnableSavedAccountEncryption(password string) error {
	return EnableSavedAccountEncryption(password)
}

func (s *SecurityService) DisableSavedAccountEncryption(password string) error {
	return DisableSavedAccountEncryption(password)
}

func (s *SecurityService) ListQuarantines() ([]QuarantineInfo, error) {
	return ListQuarantines()
}

func (s *SecurityService) DeleteQuarantine(id string) error {
	return DeleteQuarantine(id)
}

func (s *SecurityService) RetryQuarantineImport(id, password string) error {
	return RetryQuarantineImport(id, password)
}

func (s *SecurityService) ListInterruptedRestores() ([]InterruptedRestoreInfo, error) {
	return ListInterruptedRestores()
}

func (s *SecurityService) RepairInterruptedRestore() error {
	return RepairInterruptedRestore()
}

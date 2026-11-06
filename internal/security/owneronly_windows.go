//go:build windows

package security

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows a file's mode bits do not restrict anyone: os.Chmod only sets the read-only flag, and
// a new file takes its folder's ACL. That is harmless in a normal %AppData%, and not at all in a
// portable folder on a drive other accounts can read, so owner-only has to be an explicit DACL.

func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

// ownerOnlyDescriptor grants the current user everything and inherits nothing from the folder.
func ownerOnlyDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid.String() + ")")
}

// writeOwnerOnlyFile sets the DACL when the temp file is created, so the bytes are never readable
// under the folder's ACL. Windows keeps the temp file's DACL through the rename onto the target. A
// filesystem without ACLs, such as exFAT, ignores the descriptor: the contents are encrypted, so a
// portable folder there keeps working rather than refusing to save.
func writeOwnerOnlyFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sd, err := ownerOnlyDescriptor()
	if err != nil {
		return fmt.Errorf("owner-only permissions: %w", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".atomic-"+hex.EncodeToString(suffix[:])+".tmp")
	tmpPtr, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	h, err := windows.CreateFile(tmpPtr, windows.GENERIC_WRITE, 0, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), tmp)
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// tightenOwnerOnlyFile is one file's worth of TightenOwnerOnlyDir.
func tightenOwnerOnlyFile(path string, dacl *windows.ACL) error {
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}

// TightenOwnerOnlyDir applies the owner-only DACL to the files already under dir, which were
// written before writes did it themselves. A missing dir is fine, and a file that cannot be changed
// does not stop the rest.
func TightenOwnerOnlyDir(dir string) error {
	sd, err := ownerOnlyDescriptor()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	var failed []error
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				failed = append(failed, err)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := tightenOwnerOnlyFile(p, dacl); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", d.Name(), err))
		}
		return nil
	})
	return errors.Join(append(failed, walkErr)...)
}

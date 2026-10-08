//go:build !unix && !windows

package persistence

import "os"

// tryLockFile cannot take a kernel lock on this platform, which mrman is not
// released for; it grants every request, so writers are not excluded.
func tryLockFile(*os.File) (bool, error) { return true, nil }

// unlockFile is the no-op counterpart of tryLockFile.
func unlockFile(*os.File) error { return nil }

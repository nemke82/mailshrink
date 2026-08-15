package maildir

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lock represents an advisory file lock on a Maildir directory.
// This prevents concurrent modification by Dovecot or other processes
// during compression operations.
type Lock struct {
	fd   int
	path string
}

// DefaultLockTimeout is the maximum time to wait for a lock.
const DefaultLockTimeout = 30 * time.Second

// AcquireLock obtains an exclusive advisory lock (flock) on the Maildir
// directory by locking the dovecot-uidlist.lock file. This is compatible
// with Dovecot's own locking mechanism.
//
// The lock is non-blocking with retry: it attempts to acquire the lock
// up to the given timeout, sleeping briefly between attempts.
func AcquireLock(maildirPath string, timeout time.Duration) (*Lock, error) {
	lockFile := filepath.Join(maildirPath, "dovecot-uidlist.lock")

	// Create the lock file if it doesn't exist.
	fd, err := syscall.Open(lockFile, syscall.O_CREAT|syscall.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockFile, err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Lock{fd: fd, path: lockFile}, nil
		}

		if time.Now().After(deadline) {
			syscall.Close(fd)
			return nil, fmt.Errorf("timeout acquiring lock on %s after %v", maildirPath, timeout)
		}

		// Sleep briefly before retrying.
		time.Sleep(100 * time.Millisecond)
	}
}

// Release releases the advisory lock and closes the file descriptor.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}

	// Unlock first, then close.
	unlockErr := syscall.Flock(l.fd, syscall.LOCK_UN)
	closeErr := syscall.Close(l.fd)

	// Clean up the lock file (best-effort).
	os.Remove(l.path)

	if unlockErr != nil {
		return fmt.Errorf("unlock %s: %w", l.path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close lock fd %s: %w", l.path, closeErr)
	}

	return nil
}

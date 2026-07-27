//go:build !windows

package lock

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type Lock struct {
	path string
	fd   *os.File
}

func New(path string) (*Lock, error) {
	fd, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	if err := unix.Flock(int(fd.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeErr := fd.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("failed to acquire lock and close fd: %w (close error: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("lock already held by another process: %w", err)
	}

	return &Lock{path: path, fd: fd}, nil
}

func (l *Lock) Release() error {
	if l.fd == nil {
		return nil
	}
	if err := unix.Flock(int(l.fd.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("failed to unlock: %w", err)
	}
	if err := l.fd.Close(); err != nil {
		return fmt.Errorf("failed to close lock file: %w", err)
	}
	return nil
}

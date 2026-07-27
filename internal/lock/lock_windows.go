//go:build windows

package lock

import (
	"os"
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
	return &Lock{path: path, fd: fd}, nil
}

func (l *Lock) Release() error {
	if l.fd == nil {
		return nil
	}
	defer func() {
		if err := l.fd.Close(); err != nil {
			// Ignore close error
		}
	}()
	return nil
}

package utils

import (
	"os"
	"os/exec"
)

// ChownPath changes ownership of a path to the dayz user
func ChownPath(path string) error {
	cmd := exec.Command("chown", "dayz:dayz", path)
	return cmd.Run()
}

// EnsureDir ensures a directory exists and is owned by dayz
func EnsureDir(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	return ChownPath(path)
}

// ChownSymlink changes ownership of a symlink only (not the target)
func ChownSymlink(path string) error {
	cmd := exec.Command("chown", "-h", "dayz:dayz", path)
	return cmd.Run()
}

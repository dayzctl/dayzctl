package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kabroxiko/dayzctl/internal/logger"
)

// Backup handles server backups
type Backup struct {
	SourceDir string
	DestDir   string
}

// New creates a new Backup instance
func New(sourceDir, destDir string) *Backup {
	return &Backup{
		SourceDir: sourceDir,
		DestDir:   destDir,
	}
}

// Create creates a new backup
func (b *Backup) Create() (string, error) {
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	filename := fmt.Sprintf("dayz-backup-%s.tar.gz", timestamp)
	destPath := filepath.Join(b.DestDir, filename)

	if err := os.MkdirAll(b.DestDir, 0755); err != nil {
		return "", err
	}

	file, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Warn("Failed to close backup file", "error", err)
		}
	}()

	gw := gzip.NewWriter(file)
	defer func() {
		if err := gw.Close(); err != nil {
			logger.Warn("Failed to close gzip writer", "error", err)
		}
	}()

	tw := tar.NewWriter(gw)
	defer func() {
		if err := tw.Close(); err != nil {
			logger.Warn("Failed to close tar writer", "error", err)
		}
	}()

	if err := filepath.Walk(b.SourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(b.SourceDir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := file.Close(); err != nil {
				logger.Warn("Failed to close file", "path", path, "error", err)
			}
		}()

		if _, err := io.Copy(tw, file); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return "", err
	}

	logger.Info("Backup created", "file", filename)
	return destPath, nil
}

// RestoreLatest restores the latest backup
func (b *Backup) RestoreLatest() error {
	backups, err := b.listBackups()
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		return fmt.Errorf("no backups found")
	}

	latest := backups[0]
	logger.Info("Restoring latest backup", "file", latest)
	return b.Restore(latest)
}

// Restore restores a specific backup
func (b *Backup) Restore(backupPath string) error {
	file, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			logger.Warn("Failed to close backup file", "error", err)
		}
	}()

	gr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() {
		if err := gr.Close(); err != nil {
			logger.Warn("Failed to close gzip reader", "error", err)
		}
	}()

	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(b.SourceDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				if closeErr := f.Close(); closeErr != nil {
					return fmt.Errorf("copy failed: %w (close error: %v)", err, closeErr)
				}
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}

	logger.Info("Backup restored", "file", backupPath)
	return nil
}

// Prune removes old backups keeping only the most recent 'keep' count
func (b *Backup) Prune(keep int) error {
	backups, err := b.listBackups()
	if err != nil {
		return err
	}

	if len(backups) <= keep {
		return nil
	}

	for i := keep; i < len(backups); i++ {
		if err := os.Remove(backups[i]); err != nil {
			return err
		}
		logger.Info("Pruned old backup", "file", filepath.Base(backups[i]))
	}

	return nil
}

// listBackups lists all backups sorted by name (newest first)
func (b *Backup) listBackups() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(b.DestDir, "dayz-backup-*.tar.gz"))
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i] > files[j]
	})

	return files, nil
}

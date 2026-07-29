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

	"github.com/dayzctl/dayzctl/internal/logger"
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

	// prepare destination file
	f, closer, err := prepareDest(destPath)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := closer(); cerr != nil {
			logger.Warn("Failed to close destination file", "error", cerr)
		}
	}()

	// create gzip + tar writers
	gw, tw, wcloser, err := makeTarWriters(f)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := wcloser(); cerr != nil {
			logger.Warn("Failed to close tar/gzip writers", "error", cerr)
		}
	}()

	// walk source directory and write entries
	if err := walkAndWrite(b.SourceDir, tw); err != nil {
		return "", err
	}

	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gw.Close(); err != nil {
		return "", err
	}

	logger.Info("Backup created", "file", filename)
	return destPath, nil
}

// prepareDest creates destination directory and file and returns a closer.
func prepareDest(destPath string) (*os.File, func() error, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return nil, nil, err
	}
	file, err := os.Create(destPath)
	if err != nil {
		return nil, nil, err
	}
	closer := func() error {
		if err := file.Close(); err != nil {
			logger.Warn("Failed to close backup file", "error", err)
			return err
		}
		return nil
	}
	return file, closer, nil
}

// makeTarWriters wraps the provided file in gzip and tar writers and returns
// a closer that will close both writers.
func makeTarWriters(file *os.File) (*gzip.Writer, *tar.Writer, func() error, error) {
	gw := gzip.NewWriter(file)
	tw := tar.NewWriter(gw)
	closer := func() error {
		if err := tw.Close(); err != nil {
			logger.Warn("Failed to close tar writer", "error", err)
			return err
		}
		if err := gw.Close(); err != nil {
			logger.Warn("Failed to close gzip writer", "error", err)
			return err
		}
		return nil
	}
	return gw, tw, closer, nil
}

// walkAndWrite walks the source directory and writes files/dirs into the tar writer.
func walkAndWrite(src string, tw *tar.Writer) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		return processPath(relPath, path, info, tw)
	})
}

// processPath creates the tar header for the given path and writes the
// entry to the tar writer. Directories are written as headers only; regular
// files have their contents copied into the tar writer.
func processPath(relPath, fullPath string, info os.FileInfo, tw *tar.Writer) error {
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

	return writeFileToTar(fullPath, tw)
}

// writeFileToTar opens the file at path and copies its contents into the
// provided tar writer. It ensures the file is closed and logs any close
// errors, matching previous behavior.
func writeFileToTar(path string, tw *tar.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			logger.Warn("Failed to close file", "path", path, "error", cerr)
		}
	}()

	if _, err := io.Copy(tw, f); err != nil {
		return err
	}
	return nil
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
	file, _, tr, closer, err := openBackupReaders(backupPath)
	if err != nil {
		return err
	}
	// ensure resources are closed
	defer func() {
		if cerr := closer(); cerr != nil {
			logger.Warn("Failed to close backup readers", "error", cerr)
		}
	}()

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if err := extractEntry(b, header, tr); err != nil {
			return err
		}
	}

	logger.Info("Backup restored", "file", backupPath)
	_ = file // file closed by closer
	return nil
}

// openBackupReaders opens the file, gzip reader and tar reader for a backup
// and returns a closer that will close all allocated resources.
func openBackupReaders(backupPath string) (*os.File, *gzip.Reader, *tar.Reader, func() error, error) {
	file, err := os.Open(backupPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	gr, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, nil, nil, nil, err
	}

	tr := tar.NewReader(gr)

	closer := func() error {
		var firstErr error
		if err := gr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	return file, gr, tr, closer, nil
}

// extractEntry writes a single tar header entry to the filesystem under
// the backup's SourceDir. It preserves the original behavior and returns
// an error on failure.
func extractEntry(b *Backup, header *tar.Header, tr *tar.Reader) error {
	target := filepath.Join(b.SourceDir, header.Name)

	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, 0755); err != nil {
			return err
		}
		return nil
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		f, err := os.Create(target)
		if err != nil {
			return err
		}
		// Ensure file is closed and warn on close failure
		defer func() {
			if cerr := f.Close(); cerr != nil {
				logger.Warn("Failed to close file", "path", target, "error", cerr)
			}
		}()

		if _, err := io.Copy(f, tr); err != nil {
			if closeErr := f.Close(); closeErr != nil {
				return fmt.Errorf("copy failed: %w (close error: %v)", err, closeErr)
			}
			return err
		}
		return nil
	default:
		// Ignore other types (symlinks, etc.) to match previous behavior
		return nil
	}
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

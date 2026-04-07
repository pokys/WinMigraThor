package engine

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CompressProgress is sent during compression.
type CompressProgress struct {
	CurrentFile string
	Done        bool
	Err         error
}

// Compress creates a ZIP file of the given source directory.
// If progressCh is non-nil, it receives progress updates.
func Compress(srcDir, zipPath string, progressCh chan<- CompressProgress) error {
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return fmt.Errorf("create zip dir: %w", err)
	}

	zf, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("create zip file: %w", err)
	}
	defer zf.Close()

	w := zip.NewWriter(zf)
	defer w.Close()

	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			return nil
		}

		relPath := strings.TrimPrefix(path, srcDir)
		relPath = strings.TrimPrefix(relPath, string(os.PathSeparator))
		relPath = filepath.ToSlash(relPath)

		if progressCh != nil {
			progressCh <- CompressProgress{CurrentFile: relPath}
		}

		fw, err := w.Create(relPath)
		if err != nil {
			return fmt.Errorf("create zip entry %s: %w", relPath, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return nil // skip locked/inaccessible files
		}
		defer f.Close()

		_, err = io.Copy(fw, f)
		return err
	})

	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}

	if progressCh != nil {
		progressCh <- CompressProgress{Done: true}
	}
	return nil
}

// EstimateSize returns the total size of all files in a directory.
func EstimateSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

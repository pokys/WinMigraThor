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

// Decompress extracts a ZIP archive into destDir.
func Decompress(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)

		// Prevent zip-slip
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create dir for %s: %w", f.Name, err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}

		wf, err := os.Create(target)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create file %s: %w", f.Name, err)
		}

		_, err = io.Copy(wf, rc)
		rc.Close()
		wf.Close()
		if err != nil {
			return fmt.Errorf("extract %s: %w", f.Name, err)
		}
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

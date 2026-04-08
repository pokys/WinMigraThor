package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// UpdateURL is the download URL for the latest release binary.
const UpdateURL = "https://github.com/pokys/WinMigraThor/releases/download/latest/migrathor.exe"

// UpdateProgress reports download progress to the TUI.
type UpdateProgress struct {
	Downloaded int64
	Total      int64 // -1 if unknown
	Done       bool
	Err        error
}

// RunUpdate downloads the latest migrathor.exe and replaces the running executable.
// Progress is sent on progressCh; RunUpdate closes the channel when done.
//
// Replace strategy (Windows allows renaming a running exe):
//   1. Download new binary → <exe>.new
//   2. Rename <exe> → <exe>.old
//   3. Rename <exe>.new → <exe>
//   4. On any failure, attempt rollback and send Err.
func RunUpdate(progressCh chan<- UpdateProgress) {
	defer close(progressCh)

	send := func(p UpdateProgress) { progressCh <- p }

	exePath, err := os.Executable()
	if err != nil {
		send(UpdateProgress{Err: fmt.Errorf("zjištění cesty exe: %w", err)})
		return
	}

	tmpPath := exePath + ".new"
	oldPath := exePath + ".old"

	// Clean up leftovers from a previous failed update
	_ = os.Remove(tmpPath)

	// Download
	resp, err := http.Get(UpdateURL) //nolint:gosec // URL is a constant defined above
	if err != nil {
		send(UpdateProgress{Err: fmt.Errorf("stažení selhalo: %w", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		send(UpdateProgress{Err: fmt.Errorf("server vrátil HTTP %d", resp.StatusCode)})
		return
	}

	total := resp.ContentLength // -1 if server did not send Content-Length

	f, err := os.Create(tmpPath)
	if err != nil {
		send(UpdateProgress{Err: fmt.Errorf("vytvoření dočasného souboru: %w", err)})
		return
	}

	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				os.Remove(tmpPath)
				send(UpdateProgress{Err: fmt.Errorf("zápis souboru: %w", writeErr)})
				return
			}
			downloaded += int64(n)
			send(UpdateProgress{Downloaded: downloaded, Total: total})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			os.Remove(tmpPath)
			send(UpdateProgress{Err: fmt.Errorf("čtení odpovědi: %w", readErr)})
			return
		}
	}
	f.Close()

	// Replace running exe
	_ = os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		os.Remove(tmpPath)
		send(UpdateProgress{Err: fmt.Errorf("přejmenování aktuální verze: %w", err)})
		return
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		_ = os.Rename(oldPath, exePath) // rollback
		send(UpdateProgress{Err: fmt.Errorf("přejmenování nové verze: %w", err)})
		return
	}

	send(UpdateProgress{Downloaded: downloaded, Total: downloaded, Done: true})
}

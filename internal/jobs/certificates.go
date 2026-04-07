//go:build windows

package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CertificatesJob exports valid personal certificates with private keys.
type CertificatesJob struct{}

func (j *CertificatesJob) Name() string        { return "certificates" }
func (j *CertificatesJob) Description() string { return "Personal certificates (valid, with private key)" }

// certPSListScript returns a PowerShell script that lists exportable personal certs.
const certPSListScript = `
Get-ChildItem Cert:\CurrentUser\My |
  Where-Object { $_.HasPrivateKey -and $_.NotAfter -gt (Get-Date) } |
  Select-Object Thumbprint, Subject, NotAfter, FriendlyName |
  ConvertTo-Json -Compress
`

// certInfo represents a certificate from PowerShell output.
type certInfo struct {
	Thumbprint   string `json:"Thumbprint"`
	Subject      string `json:"Subject"`
	NotAfter     string `json:"NotAfter"`
	FriendlyName string `json:"FriendlyName"`
}

func (j *CertificatesJob) Scan(userPath string) (ScanResult, error) {
	certs, err := listExportableCerts()
	if err != nil {
		return ScanResult{}, err
	}
	var items []ScanItem
	for _, c := range certs {
		label := c.Subject
		if c.FriendlyName != "" {
			label = c.FriendlyName + " (" + c.Subject + ")"
		}
		items = append(items, ScanItem{
			Label:    label,
			Details:  fmt.Sprintf("Expires: %s", c.NotAfter),
			Selected: true,
		})
	}
	return ScanResult{Items: items}, nil
}

func listExportableCerts() ([]certInfo, error) {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", certPSListScript).Output()
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", err)
	}

	output := strings.TrimSpace(string(out))
	if output == "" || output == "null" {
		return nil, nil
	}

	var certs []certInfo
	// PowerShell may return a single object or array
	if err := readJSONBytes([]byte(output), &certs); err != nil {
		// Try single cert
		var single certInfo
		if err2 := readJSONBytes([]byte(output), &single); err2 != nil {
			return nil, fmt.Errorf("parse cert list: %w", err)
		}
		certs = []certInfo{single}
	}
	return certs, nil
}

func (j *CertificatesJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	certDst := filepath.Join(target, "certificates")
	if err := os.MkdirAll(certDst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	certs, err := listExportableCerts()
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	if len(certs) == 0 {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no exportable personal certificates found")
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	if opts.DryRun {
		for _, c := range certs {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("[dry-run] would export cert: %s (thumbprint: %s)", c.Subject, c.Thumbprint))
		}
		result.Status = "success"
		return result, nil
	}

	// Export each certificate as PFX with a random password, save password to file
	var exported int
	for _, c := range certs {
		safeName := sanitizeName(c.Thumbprint)
		pfxPath := filepath.Join(certDst, safeName+".pfx")
		passwordPath := filepath.Join(certDst, safeName+".password.txt")

		// Generate a random password for the PFX
		password := generateCertPassword()

		// PowerShell export command
		psScript := fmt.Sprintf(
			`$pwd = ConvertTo-SecureString -String '%s' -Force -AsPlainText; `+
				`Get-ChildItem Cert:\CurrentUser\My\%s | Export-PfxCertificate -FilePath '%s' -Password $pwd`,
			password, c.Thumbprint, pfxPath)

		out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", psScript).CombinedOutput()
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("export %s failed: %v (%s)", c.Thumbprint, err, strings.TrimSpace(string(out))))
			continue
		}

		// Save password file
		os.WriteFile(passwordPath, []byte(fmt.Sprintf(
			"Certificate: %s\nThumbprint:  %s\nExpires:     %s\nPFX Password: %s\n",
			c.Subject, c.Thumbprint, c.NotAfter, password)), 0o600)

		info, err := os.Stat(pfxPath)
		if err == nil {
			result.SizeBytes += info.Size()
		}
		exported++
	}

	// Also save a summary JSON
	summaryPath := filepath.Join(certDst, "certificates.json")
	writeJSON(summaryPath, certs)

	result.FilesCount = exported
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *CertificatesJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	certSrc := filepath.Join(source, "certificates")
	if _, err := os.Stat(certSrc); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no certificate backup found")
		return result, nil
	}

	entries, err := os.ReadDir(certSrc)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	var imported int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pfx") {
			continue
		}

		pfxPath := filepath.Join(certSrc, e.Name())
		baseName := strings.TrimSuffix(e.Name(), ".pfx")
		passwordPath := filepath.Join(certSrc, baseName+".password.txt")

		// Read password from file
		pwData, err := os.ReadFile(passwordPath)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("no password file for %s, skipping", e.Name()))
			continue
		}

		password := extractPassword(string(pwData))

		if opts.DryRun {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("[dry-run] would import cert: %s", e.Name()))
			continue
		}

		// Import PFX via PowerShell
		psScript := fmt.Sprintf(
			`$pwd = ConvertTo-SecureString -String '%s' -Force -AsPlainText; `+
				`Import-PfxCertificate -FilePath '%s' -CertStoreLocation Cert:\CurrentUser\My -Password $pwd`,
			password, pfxPath)

		out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", psScript).CombinedOutput()
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("import %s failed: %v (%s)", e.Name(), err, strings.TrimSpace(string(out))))
			continue
		}
		imported++
	}

	result.FilesCount = imported
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

// generateCertPassword creates a simple random password for PFX export.
func generateCertPassword() string {
	// Use PowerShell to generate a random password
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command",
		`-join ((48..57) + (65..90) + (97..122) | Get-Random -Count 24 | ForEach-Object {[char]$_})`).Output()
	if err != nil {
		// Fallback: timestamp-based
		return fmt.Sprintf("MigraThor_%d", time.Now().UnixNano())
	}
	return strings.TrimSpace(string(out))
}

// extractPassword extracts the password value from a password file.
func extractPassword(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "PFX Password: ") {
			return strings.TrimPrefix(line, "PFX Password: ")
		}
	}
	return ""
}

// readJSONBytes is a helper to unmarshal JSON from bytes.
func readJSONBytes(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

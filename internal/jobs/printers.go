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

// PrintersJob handles backup/restore of printers.
// Network printers are exported as JSON and re-added via Add-Printer.
// Local printers are handled via PrintBRM (best-effort, may not be available on Home editions).
type PrintersJob struct{}

func (j *PrintersJob) Name() string        { return "printers" }
func (j *PrintersJob) Description() string { return "Printers (network + local via PrintBRM)" }

type printerInfo struct {
	Name         string `json:"name"`
	ShareName    string `json:"share_name,omitempty"`
	PortName     string `json:"port_name"`
	DriverName   string `json:"driver_name"`
	Type         string `json:"type"` // "Connection" = network, "Local" = local
	IsDefault    bool   `json:"is_default"`
	Location     string `json:"location,omitempty"`
	Comment      string `json:"comment,omitempty"`
}

func (j *PrintersJob) Scan(userPath string) (ScanResult, error) {
	printers, err := listPrinters()
	if err != nil {
		return ScanResult{}, err
	}

	var items []ScanItem
	for _, p := range printers {
		detail := p.Type
		if p.IsDefault {
			detail += ", default"
		}
		items = append(items, ScanItem{
			Label:    p.Name,
			Details:  detail,
			Selected: true,
		})
	}

	return ScanResult{Items: items, TotalSizeBytes: int64(len(items) * 4096)}, nil
}

func (j *PrintersJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	printerDir := filepath.Join(target, "printers")
	if err := os.MkdirAll(printerDir, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would export printers")
		result.Status = "success"
		return result, nil
	}

	printers, err := listPrinters()
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "list printers: "+err.Error())
		return result, err
	}

	// Save printer list as JSON
	jsonPath := filepath.Join(printerDir, "printers.json")
	data, _ := json.MarshalIndent(printers, "", "  ")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		result.Errors = append(result.Errors, "write printers.json: "+err.Error())
	}

	result.FilesCount = 1
	result.SizeBytes = int64(len(data))

	// Try PrintBRM for full backup (drivers, queues, ports)
	printbrm := findPrintBRM()
	if printbrm != "" {
		exportPath := filepath.Join(printerDir, "printers.printerExport")
		cmd := exec.Command(printbrm, "-b", "-f", exportPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("PrintBRM backup failed (local printers may not restore): %v", err))
			if len(out) > 0 {
				result.Warnings = append(result.Warnings, strings.TrimSpace(string(out)))
			}
		} else {
			if info, err := os.Stat(exportPath); err == nil {
				result.SizeBytes += info.Size()
				result.FilesCount++
			}
		}
	} else {
		hasLocal := false
		for _, p := range printers {
			if p.Type != "Connection" {
				hasLocal = true
				break
			}
		}
		if hasLocal {
			result.Warnings = append(result.Warnings,
				"PrintBRM not available — local printers saved as info only (network printers will restore normally)")
		}
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)
	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *PrintersJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	printerDir := filepath.Join(source, "printers")
	if _, err := os.Stat(printerDir); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no printers backup found")
		return result, nil
	}

	// Load printer list
	jsonPath := filepath.Join(printerDir, "printers.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "read printers.json: "+err.Error())
		return result, err
	}

	var printers []printerInfo
	if err := json.Unmarshal(data, &printers); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "parse printers.json: "+err.Error())
		return result, err
	}

	if opts.DryRun {
		for _, p := range printers {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would restore: %s (%s)", p.Name, p.Type))
		}
		result.Status = "success"
		return result, nil
	}

	// Try PrintBRM restore first for local printers
	exportPath := filepath.Join(printerDir, "printers.printerExport")
	if _, err := os.Stat(exportPath); err == nil {
		printbrm := findPrintBRM()
		if printbrm != "" {
			cmd := exec.Command(printbrm, "-r", "-f", exportPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("PrintBRM restore failed: %v", err))
				if len(out) > 0 {
					result.Warnings = append(result.Warnings, strings.TrimSpace(string(out)))
				}
			} else {
				result.FilesCount++
			}
		} else {
			result.Warnings = append(result.Warnings,
				"PrintBRM not available — skipping local printer restore")
		}
	}

	// Restore network printers via Add-Printer
	var restored int
	for _, p := range printers {
		if p.Type != "Connection" {
			continue
		}
		// Network printer — re-add by connection name
		ps := fmt.Sprintf(`Add-Printer -ConnectionName '%s'`, escapeSingleQuote(p.Name))
		cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", ps)
		out, err := cmd.CombinedOutput()
		if err != nil {
			outStr := strings.TrimSpace(string(out))
			// Already exists is not an error
			if strings.Contains(outStr, "already exists") {
				restored++
			} else {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("add printer %s: %s", p.Name, outStr))
			}
		} else {
			restored++
		}
	}

	// Restore default printer
	for _, p := range printers {
		if !p.IsDefault {
			continue
		}
		ps := fmt.Sprintf(`
$p = Get-CimInstance -ClassName Win32_Printer -Filter "Name='%s'" -ErrorAction SilentlyContinue
if ($p) { Invoke-CimMethod -InputObject $p -MethodName SetDefaultPrinter | Out-Null }`,
			escapeSingleQuote(p.Name))
		exec.Command("powershell.exe", "-NoProfile", "-Command", ps).Run()
		break
	}

	result.FilesCount = restored
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)
	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

// listPrinters uses PowerShell Get-Printer to enumerate all printers.
func listPrinters() ([]printerInfo, error) {
	ps := `Get-Printer | Select-Object Name,ShareName,PortName,DriverName,Type | ConvertTo-Json -Compress`
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", ps).Output()
	if err != nil {
		return nil, fmt.Errorf("Get-Printer: %w", err)
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return nil, nil
	}

	// Get default printer
	defOut, _ := exec.Command("powershell.exe", "-NoProfile", "-Command",
		`(Get-CimInstance -ClassName Win32_Printer -Filter "Default=True").Name`).Output()
	defaultName := strings.TrimSpace(string(defOut))

	// PowerShell returns object (not array) when single item
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(outStr), &raw); err != nil {
		return nil, fmt.Errorf("parse Get-Printer output: %w", err)
	}

	type psPrinter struct {
		Name       string `json:"Name"`
		ShareName  string `json:"ShareName"`
		PortName   string `json:"PortName"`
		DriverName string `json:"DriverName"`
		Type       int    `json:"Type"` // 0=Local, 4=Connection
	}

	var psPrinters []psPrinter
	if outStr[0] == '[' {
		json.Unmarshal(raw, &psPrinters)
	} else {
		var single psPrinter
		json.Unmarshal(raw, &single)
		psPrinters = []psPrinter{single}
	}

	var result []printerInfo
	for _, p := range psPrinters {
		pType := "Local"
		if p.Type == 4 {
			pType = "Connection"
		}
		result = append(result, printerInfo{
			Name:       p.Name,
			ShareName:  p.ShareName,
			PortName:   p.PortName,
			DriverName: p.DriverName,
			Type:       pType,
			IsDefault:  p.Name == defaultName,
		})
	}
	return result, nil
}

// findPrintBRM looks for PrintBrm.exe in known locations.
func findPrintBRM() string {
	candidates := []string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "spool", "tools", "PrintBrm.exe"),
		filepath.Join(os.Getenv("SystemRoot"), "System32", "PrintBrmEngine.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func escapeSingleQuote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

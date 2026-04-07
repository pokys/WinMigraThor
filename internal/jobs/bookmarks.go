//go:build windows

package jobs

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BookmarksJob exports browser bookmarks as HTML files.
type BookmarksJob struct{}

func (j *BookmarksJob) Name() string        { return "bookmarks" }
func (j *BookmarksJob) Description() string { return "Browser bookmarks (HTML export)" }

func (j *BookmarksJob) Scan(userPath string) (ScanResult, error) {
	browsers := detectBrowsers(userPath)
	var items []ScanItem
	for _, b := range browsers {
		items = append(items, ScanItem{
			Label:    b.Name + " bookmarks",
			Details:  fmt.Sprintf("%d profiles", len(b.Profiles)),
			Selected: true,
		})
	}
	return ScanResult{Items: items}, nil
}

func (j *BookmarksJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	bmDst := filepath.Join(target, "bookmarks")
	if err := os.MkdirAll(bmDst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	browsers := detectBrowsers(userPath)

	// Filter by selected browsers if specified
	if len(opts.SelectedBrowsers) > 0 {
		selected := make(map[string]bool)
		for _, name := range opts.SelectedBrowsers {
			selected[name] = true
		}
		var filtered []Browser
		for _, b := range browsers {
			if selected[b.Name] {
				filtered = append(filtered, b)
			}
		}
		browsers = filtered
	}

	if len(browsers) == 0 {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no browsers detected for bookmark export")
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	var exported int

	for _, b := range browsers {
		for _, profile := range b.Profiles {
			if opts.DryRun {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("[dry-run] would export %s/%s bookmarks", b.Name, profile))
				continue
			}

			var htmlContent string
			var err error

			if b.Name == "Firefox" {
				htmlContent, err = exportFirefoxBookmarks(filepath.Join(b.ProfileDir, profile))
			} else {
				// Chrome / Edge use the same Bookmarks JSON format
				htmlContent, err = exportChromiumBookmarks(filepath.Join(b.ProfileDir, profile))
			}

			if err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("%s/%s: %v", b.Name, profile, err))
				continue
			}

			if htmlContent == "" {
				continue
			}

			outName := fmt.Sprintf("%s_%s_bookmarks.html",
				strings.ToLower(b.Name), sanitizeName(profile))
			outPath := filepath.Join(bmDst, outName)
			if err := os.WriteFile(outPath, []byte(htmlContent), 0o644); err != nil {
				result.Errors = append(result.Errors,
					fmt.Sprintf("write %s: %v", outName, err))
				continue
			}

			info, _ := os.Stat(outPath)
			if info != nil {
				result.SizeBytes += info.Size()
			}
			exported++
		}
	}

	result.FilesCount = exported
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *BookmarksJob) Restore(source, userPath string, opts Options) (Result, error) {
	result := Result{JobName: j.Name(), Status: "skipped"}
	result.Warnings = append(result.Warnings,
		"Bookmark HTML files are in the backup folder — import them manually via your browser's bookmark manager")
	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

// ── Chromium bookmarks (Chrome, Edge) ───────────────────────────────────────

type chromiumBookmarks struct {
	Roots map[string]chromiumNode `json:"roots"`
}

type chromiumNode struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	URL      string         `json:"url"`
	Children []chromiumNode `json:"children"`
}

func exportChromiumBookmarks(profileDir string) (string, error) {
	bmPath := filepath.Join(profileDir, "Bookmarks")
	data, err := os.ReadFile(bmPath)
	if err != nil {
		return "", fmt.Errorf("read bookmarks: %w", err)
	}

	var bm chromiumBookmarks
	if err := json.Unmarshal(data, &bm); err != nil {
		return "", fmt.Errorf("parse bookmarks: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE NETSCAPE-Bookmark-file-1>
<!-- This is an automatically generated file by MigraThor. -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
`)

	for _, root := range bm.Roots {
		writeChromiumNode(&sb, root, 1)
	}

	sb.WriteString("</DL><p>\n")
	return sb.String(), nil
}

func writeChromiumNode(sb *strings.Builder, node chromiumNode, depth int) {
	indent := strings.Repeat("    ", depth)
	if node.Type == "folder" {
		sb.WriteString(fmt.Sprintf("%s<DT><H3>%s</H3>\n", indent, html.EscapeString(node.Name)))
		sb.WriteString(fmt.Sprintf("%s<DL><p>\n", indent))
		for _, child := range node.Children {
			writeChromiumNode(sb, child, depth+1)
		}
		sb.WriteString(fmt.Sprintf("%s</DL><p>\n", indent))
	} else if node.Type == "url" && node.URL != "" {
		sb.WriteString(fmt.Sprintf("%s<DT><A HREF=\"%s\">%s</A>\n",
			indent, html.EscapeString(node.URL), html.EscapeString(node.Name)))
	}
}

// ── Firefox bookmarks ───────────────────────────────────────────────────────

func exportFirefoxBookmarks(profileDir string) (string, error) {
	// Firefox stores bookmarks in places.sqlite
	// We look for bookmarkbackups/*.jsonlz4 or try to read places.sqlite
	// Simplest approach: look for the latest bookmark backup JSON
	backupDir := filepath.Join(profileDir, "bookmarkbackups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		// No bookmark backups available
		return "", fmt.Errorf("no Firefox bookmark backups found in %s", profileDir)
	}

	// Find the latest .jsonlz4 or .json file
	var latestFile string
	for i := len(entries) - 1; i >= 0; i-- {
		name := entries[i].Name()
		if strings.HasSuffix(name, ".json") {
			latestFile = filepath.Join(backupDir, name)
			break
		}
	}

	if latestFile == "" {
		return "", fmt.Errorf("no JSON bookmark backup found")
	}

	data, err := os.ReadFile(latestFile)
	if err != nil {
		return "", fmt.Errorf("read firefox bookmarks: %w", err)
	}

	var root firefoxNode
	if err := json.Unmarshal(data, &root); err != nil {
		return "", fmt.Errorf("parse firefox bookmarks: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE NETSCAPE-Bookmark-file-1>
<!-- This is an automatically generated file by MigraThor. -->
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
`)

	for _, child := range root.Children {
		writeFirefoxNode(&sb, child, 1)
	}

	sb.WriteString("</DL><p>\n")
	return sb.String(), nil
}

type firefoxNode struct {
	Type     string        `json:"type"`
	Title    string        `json:"title"`
	URI      string        `json:"uri"`
	Children []firefoxNode `json:"children"`
}

func writeFirefoxNode(sb *strings.Builder, node firefoxNode, depth int) {
	indent := strings.Repeat("    ", depth)
	switch node.Type {
	case "text/x-moz-place-container":
		sb.WriteString(fmt.Sprintf("%s<DT><H3>%s</H3>\n", indent, html.EscapeString(node.Title)))
		sb.WriteString(fmt.Sprintf("%s<DL><p>\n", indent))
		for _, child := range node.Children {
			writeFirefoxNode(sb, child, depth+1)
		}
		sb.WriteString(fmt.Sprintf("%s</DL><p>\n", indent))
	case "text/x-moz-place":
		if node.URI != "" {
			sb.WriteString(fmt.Sprintf("%s<DT><A HREF=\"%s\">%s</A>\n",
				indent, html.EscapeString(node.URI), html.EscapeString(node.Title)))
		}
	}
}

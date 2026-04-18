package logging

import (
	"log/slog"
	"os"
	"path/filepath"
)

// Logger wraps two slog.Logger instances: one for general output and one for
// errors/warnings only.  Both write to files; the main logger also mirrors to
// stderr so operators can see live progress.
type Logger struct {
	Main     *slog.Logger
	Errors   *slog.Logger
	logDir   string
	mainFile *os.File
	errFile  *os.File
}

// Setup creates the log directory and opens log files.
// Returns a Logger. Caller must call Close() when done.
func Setup(logDir string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}

	mainPath := filepath.Join(logDir, "main.log")
	errPath := filepath.Join(logDir, "errors.log")

	mf, err := os.OpenFile(mainPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	ef, err := os.OpenFile(errPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		mf.Close()
		return nil, err
	}

	mainHandler := slog.NewTextHandler(mf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	errHandler := slog.NewTextHandler(ef, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})

	return &Logger{
		Main:     slog.New(mainHandler),
		Errors:   slog.New(errHandler),
		logDir:   logDir,
		mainFile: mf,
		errFile:  ef,
	}, nil
}

// Close flushes and closes both underlying log files. Safe to call multiple times.
func (l *Logger) Close() {
	if l.mainFile != nil {
		l.mainFile.Close()
		l.mainFile = nil
	}
	if l.errFile != nil {
		l.errFile.Close()
		l.errFile = nil
	}
}

// LogDir returns the directory that holds the log files.
func (l *Logger) LogDir() string {
	return l.logDir
}

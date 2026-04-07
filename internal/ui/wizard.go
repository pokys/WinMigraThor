package ui

// WizardMode is backup or restore.
type WizardMode int

const (
	ModeBackup WizardMode = iota
	ModeRestore
)

// BackupStep represents a step in the backup wizard.
type BackupStep int

const (
	BackupStepUsers BackupStep = iota
	BackupStepData
	BackupStepOptions
	BackupStepTarget
	BackupStepSummary
	BackupStepRunning
	BackupStepDone
)

// RestoreStep represents a step in the restore wizard.
type RestoreStep int

const (
	RestoreStepSource RestoreStep = iota
	RestoreStepData
	RestoreStepMapping
	RestoreStepConflict
	RestoreStepRunning
	RestoreStepApps
	RestoreStepDone
)

// Step labels for breadcrumb rendering.
var BackupStepLabels = []string{
	"Select Users",
	"Select Data",
	"Options",
	"Select Target",
	"Summary",
	"Running",
	"Done",
}

var RestoreStepLabels = []string{
	"Select Backup",
	"Select Data",
	"User Mapping",
	"Conflict Handling",
	"Running",
	"App Reinstall",
	"Done",
}

// Breadcrumb returns the breadcrumb string for a backup step.
func BackupBreadcrumb(step BackupStep) string {
	total := len(BackupStepLabels) - 1 // Don't count Done in total
	if int(step) >= len(BackupStepLabels) {
		return "Backup"
	}
	label := BackupStepLabels[step]
	if step == BackupStepDone {
		return "Backup Complete"
	}
	return "Backup › Step " + itoa(int(step)+1) + "/" + itoa(total) + " › " + label
}

// Breadcrumb returns the breadcrumb string for a restore step.
func RestoreBreadcrumb(step RestoreStep) string {
	total := len(RestoreStepLabels) - 1
	if int(step) >= len(RestoreStepLabels) {
		return "Restore"
	}
	label := RestoreStepLabels[step]
	if step == RestoreStepDone {
		return "Restore Complete"
	}
	return "Restore › Step " + itoa(int(step)+1) + "/" + itoa(total) + " › " + label
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

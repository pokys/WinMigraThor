//go:build windows

package jobs

func init() {
	allJobs = []Job{
		&UserDataJob{},
		&BrowsersJob{},
		&EmailJob{},
		&WiFiJob{},
		&DevEnvJob{},
		&AppsJob{},
		&AppConfigJob{},
	}
}

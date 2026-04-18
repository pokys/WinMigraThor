//go:build windows

package jobs

func init() {
	allJobs = []Job{
		&UserDataJob{},
		&BrowsersJob{},
		&BookmarksJob{},
		&EmailJob{},
		&WiFiJob{},
		&VPNJob{},
		&CredentialsJob{},
		&CertificatesJob{},
		&AppsJob{},
		&DevEnvJob{},
		&AppConfigJob{},
		&PrintersJob{},
	}
}

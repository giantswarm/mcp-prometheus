package project

import "testing"

func TestVersionFallback(t *testing.T) {
	const (
		tag     = "v1.2.3"
		sha     = "abc1234"
		noBuild = ""
	)
	tests := []struct {
		name      string
		version   string
		buildInfo string
		gitSHA    string
		want      string
	}{
		{"nothing available", dev, noBuild, dev, dev},
		{"explicit version ldflag wins", tag, "v0.9.0", sha, tag},
		{"empty version ldflag falls through", "", tag, sha, tag},
		{"build info supplies version", dev, tag, sha, tag},
		{"build info absent; sha fallback", dev, noBuild, sha, sha},
		{"build info beats sha", dev, tag, sha, tag},
	}

	origVersion, origSHA, origBuildInfo := version, gitSHA, buildInfoVersion
	t.Cleanup(func() { version, gitSHA, buildInfoVersion = origVersion, origSHA, origBuildInfo })

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version = tc.version
			gitSHA = tc.gitSHA
			buildInfoVersion = func() string { return tc.buildInfo }
			if got := Version(); got != tc.want {
				t.Errorf("Version() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAccessors(t *testing.T) {
	if GitSHA() == "" {
		t.Error("GitSHA must not be empty")
	}
	if BuildTimestamp() == "" {
		t.Error("BuildTimestamp must not be empty")
	}
}

package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/updater"
)

func TestPlanUpdateReportsAndRefuses(t *testing.T) {
	behind := updater.Status{
		State:   updater.StateBehind,
		Current: "0.2.0",
		Release: updater.Release{Tag: "v0.3.0"},
		Archive: updater.Asset{Name: "aegis_0.3.0_linux_arm64.tar.gz"},
	}
	ahead := updater.Status{
		State:   updater.StateAhead,
		Current: "0.4.0",
		Release: updater.Release{Tag: "v0.3.0"},
	}
	unversioned := updater.Status{
		State:   updater.StateUnversioned,
		Current: "dev",
		Release: updater.Release{Tag: "v0.3.0"},
	}
	behindNoAsset := behind
	behindNoAsset.Archive = updater.Asset{}
	upToDate := updater.Status{
		State:   updater.StateUpToDate,
		Current: "0.3.0",
		Release: updater.Release{Tag: "v0.3.0"},
	}

	cases := []struct {
		name        string
		status      updater.Status
		checkOnly   bool
		force       bool
		wantInstall bool
		wantReport  string
		wantErr     string
	}{
		{
			name:       "up to date stops with the release name",
			status:     upToDate,
			wantReport: "aegis v0.3.0 is the newest release.",
		},
		{
			name:        "behind installs the release it names",
			status:      behind,
			wantInstall: true,
			wantReport:  "aegis 0.2.0 can update to v0.3.0 (aegis_0.3.0_linux_arm64.tar.gz).",
		},
		{
			name:      "behind checked only reports and points at the install",
			status:    behind,
			checkOnly: true,
			wantReport: "aegis 0.2.0 can update to v0.3.0 (aegis_0.3.0_linux_arm64.tar.gz).\n" +
				"Run aegis update to install it.",
		},
		{
			name:        "behind with no asset for this platform still installs",
			status:      behindNoAsset,
			wantInstall: true,
			wantReport:  "aegis 0.2.0 can update to v0.3.0.",
		},
		{
			name:       "ahead checked only reports the overshoot",
			status:     ahead,
			checkOnly:  true,
			wantReport: "aegis 0.4.0 is past the newest release v0.3.0.",
		},
		{
			name:    "ahead refuses to move backwards without force",
			status:  ahead,
			wantErr: "move backwards",
		},
		{
			name:        "ahead installs when forced",
			status:      ahead,
			force:       true,
			wantInstall: true,
			wantReport:  "aegis 0.4.0 is past the newest release v0.3.0.",
		},
		{
			name:       "unversioned checked only reports both versions",
			status:     unversioned,
			checkOnly:  true,
			wantReport: "This build carries no release version. The newest release is v0.3.0.",
		},
		{
			name:    "unversioned refuses to install without force",
			status:  unversioned,
			wantErr: "--force",
		},
		{
			name:        "unversioned installs when forced",
			status:      unversioned,
			force:       true,
			wantInstall: true,
			wantReport:  "This build carries no release version. The newest release is v0.3.0.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planUpdate(tc.status, tc.checkOnly, tc.force)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantInstall, plan.install)
			require.Equal(t, tc.wantReport, plan.report)
		})
	}
}

func TestPlanRestartNamesTheRunningUnit(t *testing.T) {
	cases := []struct {
		name        string
		system      bool
		user        bool
		wantCommand []string
		wantHint    string
	}{
		{
			name:        "the system unit runs the service",
			system:      true,
			wantCommand: []string{"systemctl", "restart", "aegis"},
			wantHint:    "Restart it with sudo systemctl restart aegis.",
		},
		{
			name:        "a user unit runs the service",
			user:        true,
			wantCommand: []string{"systemctl", "--user", "restart", "aegis"},
			wantHint:    "Restart it with systemctl --user restart aegis.",
		},
		{
			name:     "nothing runs the service",
			wantHint: "Restart aegis to run the new version.",
		},
		{
			name:        "both units and the system one wins",
			system:      true,
			user:        true,
			wantCommand: []string{"systemctl", "restart", "aegis"},
			wantHint:    "Restart it with sudo systemctl restart aegis.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planRestart(tc.system, tc.user)
			require.Equal(t, tc.wantCommand, plan.command)
			require.Equal(t, tc.wantHint, plan.hint)
		})
	}
}

func TestUnderDir(t *testing.T) {
	cases := []struct {
		path string
		dir  string
		want bool
	}{
		{path: "/tmp/go-build123/aegis", dir: "/tmp", want: true},
		{path: "/tmp/aegis", dir: "/tmp", want: true},
		{path: "/usr/local/bin/aegis", dir: "/tmp", want: false},
		{path: "/tmpfoo/aegis", dir: "/tmp", want: false},
		{path: "/tmp", dir: "/tmp", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			require.Equal(t, tc.want, underDir(tc.path, tc.dir))
		})
	}
}

func TestReleasesEndpoint(t *testing.T) {
	got, err := releasesEndpoint("McCune1224/aegis")
	require.NoError(t, err)
	require.Equal(t, "https://api.github.com/repos/McCune1224/aegis/releases/latest", got)

	for _, bad := range []string{"", "aegis", "a/b/c", "owner name/aegis", "/aegis", "owner/", "../evil", "owner/repo?x"} {
		_, err := releasesEndpoint(bad)
		require.ErrorContains(t, err, "owner/name", "rejecting %q", bad)
	}
}

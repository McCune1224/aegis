package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"aegis/internal/updater"
)

// defaultReleasesRepo is where a release cut from this repository lands.
const defaultReleasesRepo = "McCune1224/aegis"

// defaultGOARM is the toolchain's GOARM for linux/arm. Every arm build
// records GOARM in its build settings, so this only covers a binary with no
// build info left at all.
const defaultGOARM = "7"

func newUpdateCmd() *cobra.Command {
	var checkOnly, force bool
	var repo string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Install the newest published release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd, checkOnly, force, repo)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "report a newer release without installing it")
	cmd.Flags().BoolVar(&force, "force", false, "install even when this build cannot be ordered against the release")
	cmd.Flags().StringVar(&repo, "repo", defaultReleasesRepo, "repository that publishes the releases, as owner/name")
	return cmd
}

func runUpdate(cmd *cobra.Command, checkOnly, force bool, repo string) error {
	endpoint, err := releasesEndpoint(repo)
	if err != nil {
		return err
	}
	target, err := runningBinary()
	if err != nil {
		return err
	}

	checker := updater.New(updater.Config{
		Endpoint:  endpoint,
		UserAgent: fmt.Sprintf("aegis/%s", version),
		Platform: updater.Platform{
			GOOS:   runtime.GOOS,
			GOARCH: runtime.GOARCH,
			GOARM:  goarm(),
		},
	})
	status, err := checker.Check(cmd.Context(), version)
	if err != nil {
		return err
	}

	plan, err := planUpdate(status, checkOnly, force)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if plan.report != "" {
		if _, err := fmt.Fprintln(out, plan.report); err != nil {
			return err
		}
	}
	if !plan.install {
		return nil
	}
	if underDir(target, os.TempDir()) {
		return fmt.Errorf("this build runs from %s, which a reboot clears, so there is nothing to update in place. Install a release binary first", target)
	}
	if err := checker.Install(cmd.Context(), status, target); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(out, "Installed %s (%s) over %s.\n",
		status.Release.Tag, status.Archive.Name, target); err != nil {
		return err
	}
	return restartService(cmd.Context(), out)
}

// updatePlan is what one comparison asks the command to do.
type updatePlan struct {
	report  string
	install bool
}

// planUpdate turns a status into the line to print and whether to install.
// A build that cannot be ordered against the release, and a build past it,
// install only when the operator says so.
func planUpdate(status updater.Status, checkOnly, force bool) (updatePlan, error) {
	switch status.State {
	case updater.StateUpToDate:
		return updatePlan{report: fmt.Sprintf("aegis %s is the newest release.", status.Release.Tag)}, nil

	case updater.StateBehind:
		report := fmt.Sprintf("aegis %s can update to %s.", status.Current, status.Release.Tag)
		if status.Archive.Name != "" {
			report = fmt.Sprintf("aegis %s can update to %s (%s).",
				status.Current, status.Release.Tag, status.Archive.Name)
		}
		if checkOnly {
			return updatePlan{report: report + "\nRun aegis update to install it."}, nil
		}
		return updatePlan{report: report, install: true}, nil

	case updater.StateAhead:
		report := fmt.Sprintf("aegis %s is past the newest release %s.", status.Current, status.Release.Tag)
		if checkOnly {
			return updatePlan{report: report}, nil
		}
		if !force {
			return updatePlan{}, fmt.Errorf("%s, so updating would move backwards. Re-run with --force to install it anyway", report)
		}
		return updatePlan{report: report, install: true}, nil

	default:
		report := fmt.Sprintf("This build carries no release version. The newest release is %s.", status.Release.Tag)
		if checkOnly {
			return updatePlan{report: report}, nil
		}
		if !force {
			return updatePlan{}, fmt.Errorf("this build carries no release version, so it cannot be ordered against %s. Re-run with --force to install it", status.Release.Tag)
		}
		return updatePlan{report: report, install: true}, nil
	}
}

// restartPlan names the systemd command that picks up the new binary, and
// what to say when the command is not ours to run.
type restartPlan struct {
	command []string
	hint    string
}

// planRestart prefers the system unit, which is what the documented install
// enables, over a user unit running beside it.
func planRestart(systemActive, userActive bool) restartPlan {
	switch {
	case systemActive:
		return restartPlan{
			command: []string{"systemctl", "restart", "aegis"},
			hint:    "Restart it with sudo systemctl restart aegis.",
		}
	case userActive:
		return restartPlan{
			command: []string{"systemctl", "--user", "restart", "aegis"},
			hint:    "Restart it with systemctl --user restart aegis.",
		}
	default:
		return restartPlan{hint: "Restart aegis to run the new version."}
	}
}

// restartService picks the running unit up on the new binary, and falls back
// to its hint when systemd is absent or declines the restart.
func restartService(ctx context.Context, out io.Writer) error {
	plan := planRestart(unitActive(ctx, ""), unitActive(ctx, "--user"))
	if plan.command == nil {
		_, err := fmt.Fprintln(out, plan.hint)
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	restart := exec.CommandContext(ctx, plan.command[0], plan.command[1:]...)
	if err := restart.Run(); err != nil {
		_, hintErr := fmt.Fprintln(out, plan.hint)
		return hintErr
	}
	_, err := fmt.Fprintln(out, "Restarted aegis.service.")
	return err
}

// unitActive reports whether systemd is already running the aegis unit in
// one scope: "" for the system manager, "--user" for the caller's.
func unitActive(ctx context.Context, scope string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"is-active", "--quiet", "aegis"}
	if scope != "" {
		args = append([]string{scope}, args...)
	}
	return exec.CommandContext(ctx, "systemctl", args...).Run() == nil
}

// runningBinary is the file this build executes from, with any symlink along
// the way resolved, because that file is what an install replaces.
func runningBinary() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the running binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	return path, nil
}

// underDir reports whether path sits inside dir.
func underDir(path, dir string) bool {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// releasesEndpoint is where the releases/latest metadata for a repository
// lives. Only owner/name names a repository, and only a name GitHub would
// accept names one, so a typo stops here rather than at a surprising URL.
func releasesEndpoint(repo string) (string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || !validRepoPart(parts[0]) || !validRepoPart(parts[1]) {
		return "", fmt.Errorf("--repo must be owner/name, not %q", repo)
	}
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo), nil
}

// validRepoPart is one half of an owner/name: letters, digits, and the
// punctuation a repository name allows, and never a path element that means
// somewhere else.
func validRepoPart(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// goarm is the GOARM this build carries, which decides between the armv6
// and the armv7 release asset. Every arm build records it in its settings.
func goarm() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return defaultGOARM
	}
	for _, setting := range info.Settings {
		if setting.Key == "GOARM" {
			return setting.Value
		}
	}
	return defaultGOARM
}

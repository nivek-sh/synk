package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

const synkReleases = "https://github.com/nivek-sh/synk/releases"

var releaseVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update Synk using its installation method",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			executable, err := os.Executable()
			if err != nil {
				return err
			}
			executable, err = filepath.EvalSymlinks(executable)
			if err != nil {
				return err
			}
			return updateSynk(cmd, executable)
		},
	}
}

func updateSynk(cmd *cobra.Command, executable string) error {
	ctx := cmd.Context()
	if strings.HasPrefix(executable, "/nix/store/") {
		return updateNixProfile(cmd, executable)
	}

	var debArchitecture string
	dpkgOwner, _ := exec.CommandContext(ctx, "dpkg-query", "-S", "--", executable).Output()
	pacmanOwner, _ := exec.CommandContext(ctx, "pacman", "-Qoq", "--", executable).Output()
	switch {
	case strings.TrimSpace(string(pacmanOwner)) == "synk":
		for _, helper := range []string{"paru", "yay"} {
			if _, err := exec.LookPath(helper); err == nil {
				return runUpdateCommand(cmd, helper, "-S", "--needed", "synk")
			}
		}
		return fmt.Errorf("Synk is installed from AUR. Update the synk package with your AUR helper or rebuild it with makepkg")
	case strings.HasPrefix(string(dpkgOwner), "synk:"):
		output, err := exec.CommandContext(ctx, "dpkg", "--print-architecture").Output()
		if err != nil {
			return fmt.Errorf("detect Debian package architecture: %w", err)
		}
		debArchitecture = strings.TrimSpace(string(output))
		if debArchitecture != "amd64" && debArchitecture != "arm64" {
			return fmt.Errorf("unsupported Debian package architecture %q", debArchitecture)
		}
	case len(dpkgOwner) != 0 || len(pacmanOwner) != 0 || executable == "/usr/bin/synk" || executable == "/bin/synk":
		return fmt.Errorf("update this copy of Synk using the package manager that installed %s", executable)
	}

	if !releaseVersionPattern.MatchString(Version) || filepath.Base(executable) != "synk" {
		return fmt.Errorf("automatic updates require an official Synk release. For a source build, reinstall from the updated checkout")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	body, err := getUpdateAsset(ctx, client, "https://api.github.com/repos/nivek-sh/synk/releases/latest")
	if err != nil {
		return err
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	err = json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&release)
	body.Close()
	if err != nil {
		return fmt.Errorf("read GitHub release metadata: %w", err)
	}
	latest := strings.TrimPrefix(release.Tag, "v")
	if release.Tag != "v"+latest || !releaseVersionPattern.MatchString(latest) {
		return fmt.Errorf("unexpected GitHub release tag %q", release.Tag)
	}
	if !newerRelease(latest, Version) {
		fmt.Fprintf(cmd.OutOrStdout(), "Synk %s is up to date (latest release: %s).\n", Version, latest)
		return nil
	}

	baseURL := synkReleases + "/download/" + release.Tag
	var asset, archiveRoot string
	if debArchitecture != "" {
		asset = fmt.Sprintf("synk_%s_%s.deb", latest, debArchitecture)
	} else {
		platform := runtime.GOOS
		if platform == "darwin" {
			platform = "macos"
		}
		arch := runtime.GOARCH
		if arch == "amd64" {
			arch = "x86_64"
		}
		if (platform != "linux" && platform != "macos") || (arch != "x86_64" && arch != "arm64") {
			return fmt.Errorf("no Synk release archive for %s/%s", runtime.GOOS, runtime.GOARCH)
		}
		archiveRoot = fmt.Sprintf("synk-%s-%s-%s", latest, platform, arch)
		asset = archiveRoot + ".tar.gz"
	}

	dir, err := os.MkdirTemp("", "synk-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	fmt.Fprintf(cmd.OutOrStdout(), "Updating Synk %s to %s using %s.\n", Version, latest, asset)
	download := filepath.Join(dir, asset)
	if err := downloadSynkUpdate(ctx, client, baseURL, asset, download); err != nil {
		return err
	}
	if debArchitecture != "" {
		// Let the APT sandbox read the verified package.
		if err := os.Chmod(dir, 0o755); err != nil {
			return err
		}
		if err := os.Chmod(download, 0o644); err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return runUpdateCommand(cmd, "sudo", "apt-get", "install", download)
		}
		return runUpdateCommand(cmd, "apt-get", "install", download)
	}

	if err := runUpdateCommand(cmd, "tar", "-xzf", download, "-C", dir, archiveRoot+"/synk", archiveRoot+"/install.sh"); err != nil {
		return err
	}
	installer := filepath.Join(dir, archiveRoot, "install.sh")
	destination := filepath.Dir(executable)
	if destination == "/usr/local/bin" && unix.Access(destination, unix.W_OK) != nil && os.Geteuid() != 0 {
		return runUpdateCommand(cmd, "sudo", "bash", installer, "--dir", destination, "--no-bw")
	}
	return runUpdateCommand(cmd, "bash", installer, "--dir", destination, "--no-bw")
}

func updateNixProfile(cmd *cobra.Command, executable string) error {
	output, err := exec.CommandContext(cmd.Context(), "nix", "profile", "list", "--json").Output()
	if err != nil {
		return fmt.Errorf("read Nix profile: %w. Update Synk using its Nix configuration or original profile", err)
	}
	var profile struct {
		Elements map[string]struct {
			OriginalURL string   `json:"originalUrl"`
			StorePaths  []string `json:"storePaths"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(output, &profile); err != nil {
		return fmt.Errorf("read Nix profile: %w", err)
	}
	var matches []string
	for name, element := range profile.Elements {
		for _, path := range element.StorePaths {
			if strings.HasPrefix(executable, path+"/") {
				original, _, _ := strings.Cut(element.OriginalURL, "#")
				if version, ok := strings.CutPrefix(original, "github:nivek-sh/synk/v"); ok && releaseVersionPattern.MatchString(version) {
					return fmt.Errorf("Nix profile entry %q is pinned to %s. Update the version in its Nix install reference", name, original)
				}
				matches = append(matches, name)
				break
			}
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("could not identify a unique Synk entry in the current Nix profile. Update Synk using its Nix configuration or original profile")
	}
	return runUpdateCommand(cmd, "nix", "profile", "upgrade", "--", matches[0])
}

func runUpdateCommand(cmd *cobra.Command, name string, args ...string) error {
	command := exec.CommandContext(cmd.Context(), name, args...)
	command.Stdin = cmd.InOrStdin()
	command.Stdout = cmd.OutOrStdout()
	command.Stderr = cmd.ErrOrStderr()
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

func getUpdateAsset(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, response.Status)
	}
	return response.Body, nil
}

func downloadSynkUpdate(ctx context.Context, client *http.Client, baseURL, asset, destination string) error {
	body, err := getUpdateAsset(ctx, client, baseURL+"/SHA256SUMS")
	if err != nil {
		return err
	}
	manifest, err := io.ReadAll(io.LimitReader(body, 1<<20))
	body.Close()
	if err != nil {
		return err
	}
	var expected string
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			if expected != "" {
				return fmt.Errorf("duplicate checksum for %s", asset)
			}
			expected = fields[0]
		}
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("no valid checksum found for %s", asset)
	}

	body, err = getUpdateAsset(ctx, client, baseURL+"/"+asset)
	if err != nil {
		return err
	}
	defer body.Close()
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if actual := fmt.Sprintf("%x", hash.Sum(nil)); actual != expected {
		return fmt.Errorf("checksum verification failed for %s", asset)
	}
	return nil
}

// Both versions have already been validated as canonical MAJOR.MINOR.PATCH.
func newerRelease(latest, current string) bool {
	currentParts := strings.Split(current, ".")
	for i, part := range strings.Split(latest, ".") {
		installed := currentParts[i]
		if len(part) != len(installed) {
			return len(part) > len(installed)
		}
		if part != installed {
			return part > installed
		}
	}
	return false
}

//go:build linux && !nosupraseal

package supraffi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/hugepageutil"
)

var spdkLog = logging.Logger("supraffi-spdk")

const (
	spdkVersion = "v24.05"
	spdkRepo    = "https://github.com/spdk/spdk"
	spdkDir     = "spdk-v24.05"
)

// downloadSPDK downloads the SPDK repository if it doesn't exist.
// It performs a shallow clone to save time and space since we only need the setup script.
func downloadSPDK(spdkPath string) error {
	// Check if SPDK directory already exists
	if _, err := os.Stat(spdkPath); err == nil {
		// Check if setup script exists
		setupScript := filepath.Join(spdkPath, "scripts", "setup.sh")
		if _, err := os.Stat(setupScript); err == nil {
			spdkLog.Infow("SPDK already exists", "path", spdkPath)
			return nil
		}
	}

	spdkLog.Infow("downloading SPDK", "version", spdkVersion, "path", spdkPath)

	// Create parent directory if it doesn't exist
	parentDir := filepath.Dir(spdkPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return xerrors.Errorf("creating SPDK parent directory: %w", err)
	}

	// Remove existing directory if it's incomplete
	if _, err := os.Stat(spdkPath); err == nil {
		spdkLog.Warnw("removing incomplete SPDK directory", "path", spdkPath)
		if err := os.RemoveAll(spdkPath); err != nil {
			return xerrors.Errorf("removing incomplete SPDK directory: %w", err)
		}
	}

	// Clone SPDK repository (shallow clone to save time/space)
	// We use --depth 1 to only get the latest commit, and --branch to get the specific version
	cmd := exec.Command("git", "clone", "--branch", spdkVersion, "--depth", "1", "--recursive", spdkRepo, spdkPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	spdkLog.Info("cloning SPDK repository (this may take a moment)...")
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("cloning SPDK repository: %w. Make sure git is installed", err)
	}

	// Verify setup script exists
	setupScript := filepath.Join(spdkPath, "scripts", "setup.sh")
	if _, err := os.Stat(setupScript); err != nil {
		return xerrors.Errorf("SPDK setup script not found after clone: %w", err)
	}

	spdkLog.Info("SPDK downloaded successfully")
	return nil
}

// findOrDownloadSPDK finds the SPDK directory or downloads it if missing.
// Returns the path to the SPDK directory.
func findOrDownloadSPDK() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", xerrors.Errorf("getting executable path: %w", err)
	}

	// Try different possible locations for SPDK
	possiblePaths := []string{
		filepath.Join(filepath.Dir(execPath), "extern/supraseal/deps", spdkDir),
		filepath.Join(filepath.Dir(execPath), "../extern/supraseal/deps", spdkDir),
		filepath.Join(filepath.Dir(execPath), "../../extern/supraseal/deps", spdkDir),
		"/usr/local/share/curio/extern/supraseal/deps/" + spdkDir,
		filepath.Join(os.Getenv("HOME"), ".curio/spdk", spdkDir),
		filepath.Join(os.TempDir(), "curio-spdk", spdkDir),
	}

	// First, try to find existing SPDK
	for _, path := range possiblePaths {
		setupScript := filepath.Join(path, "scripts", "setup.sh")
		if _, err := os.Stat(setupScript); err == nil {
			spdkLog.Infow("found existing SPDK", "path", path)
			return path, nil
		}
	}

	// SPDK not found, download it to the most appropriate location
	// Prefer a location relative to the executable, fallback to user's home or temp
	var downloadPath string
	for _, path := range possiblePaths {
		parentDir := filepath.Dir(path)
		if _, err := os.Stat(parentDir); err == nil || os.IsNotExist(err) {
			// Try to create parent directory to see if we have write access
			if err := os.MkdirAll(parentDir, 0755); err == nil {
				downloadPath = path
				break
			}
		}
	}

	if downloadPath == "" {
		// Fallback to temp directory
		downloadPath = filepath.Join(os.TempDir(), "curio-spdk", spdkDir)
	}

	if err := downloadSPDK(downloadPath); err != nil {
		return "", err
	}

	return downloadPath, nil
}

// SetupSPDK runs the SPDK setup script to configure NVMe devices for use with SupraSeal.
// This binds NVMe devices to the SPDK driver and allocates hugepages.
// It requires root privileges and will use sudo if not already running as root.
func SetupSPDK(nrHuge int) error {
	// Find or download SPDK
	spdkPath, err := findOrDownloadSPDK()
	if err != nil {
		return xerrors.Errorf("finding/downloading SPDK: %w", err)
	}

	setupScript := filepath.Join(spdkPath, "scripts", "setup.sh")

	spdkLog.Infow("running SPDK setup", "script", setupScript, "hugepages", nrHuge)

	// Check if we need sudo
	needsSudo := os.Geteuid() != 0

	var cmd *exec.Cmd
	if needsSudo {
		// Run with sudo
		cmd = exec.Command("sudo", "env", fmt.Sprintf("NRHUGE=%d", nrHuge), setupScript)
	} else {
		// Already root, run directly
		cmd = exec.Command("env", fmt.Sprintf("NRHUGE=%d", nrHuge), setupScript)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	spdkLog.Info("running SPDK setup script (this may take a moment and require sudo)...")

	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("running SPDK setup script: %w", err)
	}

	spdkLog.Info("SPDK setup completed successfully")
	return nil
}

// CheckAndSetupSPDK checks if SPDK is set up, and if not, runs the setup script.
// This is a convenience function that combines checking and setup.
// It ensures SPDK is downloaded and available, then runs the setup script if hugepages aren't configured.
func CheckAndSetupSPDK(nrHuge int, minPages int) error {
	// First ensure SPDK is available (download if needed)
	spdkPath, err := findOrDownloadSPDK()
	if err != nil {
		return xerrors.Errorf("ensuring SPDK is available: %w", err)
	}
	_ = spdkPath // SPDK is now available

	// Check if hugepages are configured
	if err := hugepageutil.CheckHugePages(minPages); err != nil {
		spdkLog.Warnw("hugepages not configured, attempting SPDK setup", "err", err)
		// Try to set up SPDK which also configures hugepages
		if setupErr := SetupSPDK(nrHuge); setupErr != nil {
			return xerrors.Errorf("SPDK setup failed: %w (original hugepage check error: %v)", setupErr, err)
		}

		// Verify hugepages are now configured
		if err := hugepageutil.CheckHugePages(minPages); err != nil {
			return xerrors.Errorf("hugepages still not configured after SPDK setup: %w", err)
		}
	}

	return nil
}

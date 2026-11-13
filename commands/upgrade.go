package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Seams for tests.
var (
	latestReleaseFn = client.LatestRelease
	downloadFn      = download
	goarch          = runtime.GOARCH
)

const (
	maxArchiveBytes  = 100 << 20
	maxChecksumBytes = 1 << 20
)

// maxBinaryBytes caps the executable taken from an archive; a variable for tests.
var maxBinaryBytes int64 = 200 << 20

// writeFileFn writes the unpacked binary; a seam for tests.
var writeFileFn = os.WriteFile

// download fetches url into memory, refusing more than max bytes. Only
// https URLs are fetched (a release asset is never plain http).
func download(url string, max int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1") {
		return nil, fmt.Errorf("refusing to download from %q", url)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("download is larger than %d bytes", max)
	}
	return data, nil
}

// checksumFor finds the SHA-256 of file in a checksums.txt ("<hex>  <name>").
func checksumFor(checksums, file string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == file && len(f[0]) == 64 {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// extractBinary returns the ts-cli executable from a .tar.gz archive. Only a
// regular file named ts-cli is taken, wherever it sits; nothing is written.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive is not a valid .tar.gz: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("the archive does not contain ts-cli")
		}
		if err != nil {
			return nil, fmt.Errorf("the archive is damaged: %w", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != "ts-cli" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxBinaryBytes+1))
		if err != nil {
			return nil, fmt.Errorf("the archive is damaged: %w", err)
		}
		if int64(len(data)) > maxBinaryBytes {
			return nil, fmt.Errorf("the binary in the archive is too large")
		}
		return data, nil
	}
}

// installRelease downloads, verifies and installs the release over the
// running executable.
func installRelease(rel client.Release, out io.Writer) error {
	if goos == "windows" {
		return fmt.Errorf("a running ts-cli.exe cannot replace itself: download the .zip from %s and replace it by hand", rel.Page)
	}
	asset, ok := rel.AssetFor(goos, goarch)
	if !ok {
		return fmt.Errorf("release %s has no archive for %s/%s; see %s", rel.Tag, goos, goarch, rel.Page)
	}
	sums, ok := rel.Asset("checksums.txt")
	if !ok {
		return fmt.Errorf("release %s has no checksums.txt; refusing to install an unverified binary", rel.Tag)
	}
	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("cannot find the running executable: %w", err)
	}
	if resolved, err := evalSymlinks(exe); err == nil {
		exe = resolved
	}

	fmt.Fprintf(out, "Downloading %s…\n", asset.Name)
	archive, err := downloadFn(asset.URL, maxArchiveBytes)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	listing, err := downloadFn(sums.URL, maxChecksumBytes)
	if err != nil {
		return fmt.Errorf("download of checksums.txt failed: %w", err)
	}
	want, ok := checksumFor(string(listing), asset.Name)
	if !ok {
		return fmt.Errorf("checksums.txt has no entry for %s; refusing to install", asset.Name)
	}
	if sum := sha256.Sum256(archive); hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("checksum mismatch for %s; nothing was installed", asset.Name)
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "ts-cli-upgrade-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	staged := filepath.Join(dir, "ts-cli")
	if err := writeFileFn(staged, binary, 0o755); err != nil {
		return err
	}
	if err := copyFile(staged, exe); err != nil {
		return fmt.Errorf("could not replace %s: %w (if it needs administrator rights, run: sudo ts-cli upgrade --install)", exe, err)
	}
	fmt.Fprintf(out, "✓ Installed ts-cli %s at %s (checksum verified)\n", rel.Tag, exe)
	return nil
}

// NewUpgradeCommand creates the upgrade command.
func NewUpgradeCommand() *cobra.Command {
	var install bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Check GitHub Releases for a newer ts-cli (and install it)",
		Long: `Ask GitHub for the latest release and compare it with this version. The only
request is an unauthenticated GET of the public release information; nothing
about you or your tailnet is sent.

  --install  download the archive for this system, verify its SHA-256 against
             the release's checksums.txt, and replace the running binary

Without --install nothing is changed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			rel, err := latestReleaseFn()
			if err != nil {
				return err
			}
			if client.CompareVersions(Version, rel.Tag) >= 0 {
				fmt.Fprintf(out, "ts-cli %s is up to date (latest release %s)\n", Version, rel.Tag)
				return nil
			}
			fmt.Fprintf(out, "A new version is available: %s → %s\n%s\n", Version, rel.Tag, rel.Page)
			if !install {
				fmt.Fprintln(out, "Run `ts-cli upgrade --install` to install it.")
				return nil
			}
			return installRelease(rel, out)
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "Download, verify and install the latest release")
	return cmd
}

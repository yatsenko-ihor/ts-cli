package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Release checks against GitHub Releases. Nothing here sends credentials or
// any information about the user: it is one unauthenticated GET of the public
// "latest release" document.

// ReleasesURLEnv overrides the "latest release" URL (tests, a mirror).
const ReleasesURLEnv = "TS_CLI_RELEASES_URL"

const defaultReleasesURL = "https://api.github.com/repos/yatsenko-ihor/ts-cli/releases/latest"

// releasesURL is the URL asked for the latest release.
var releasesURL = apiRoot2(os.Getenv(ReleasesURLEnv), defaultReleasesURL)

// apiRoot2 returns v when it is an http(s) URL, else def.
func apiRoot2(v, def string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://") {
		return v
	}
	return def
}

// maxReleaseBytes caps the release document.
const maxReleaseBytes = 1 << 20

// ReleaseAsset is one downloadable file of a release.
type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is the part of a GitHub release ts-cli uses.
type Release struct {
	Tag        string         `json:"tag_name"`
	Page       string         `json:"html_url"`
	Prerelease bool           `json:"prerelease"`
	Draft      bool           `json:"draft"`
	Assets     []ReleaseAsset `json:"assets"`
}

// LatestRelease fetches the latest published release.
func LatestRelease() (Release, error) {
	req, err := http.NewRequest(http.MethodGet, releasesURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ts-cli")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseBytes)).Decode(&rel); err != nil {
		return Release{}, fmt.Errorf("unexpected answer from GitHub: %w", err)
	}
	if rel.Tag == "" {
		return Release{}, fmt.Errorf("the release has no tag")
	}
	return rel, nil
}

// parseVersion reads "v1.2.3" or "1.2.3-rc1" into numbers and a pre-release part.
func parseVersion(v string) (nums [3]int, pre string, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nums, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nums, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

// CompareVersions returns -1, 0 or 1 when a is older than, equal to or newer
// than b. A release is newer than its own pre-releases; a version that cannot
// be read counts as older than any that can (a development build).
func CompareVersions(a, b string) int {
	an, ap, aok := parseVersion(a)
	bn, bp, bok := parseVersion(b)
	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return -1
	case !bok:
		return 1
	}
	for i := range an {
		if an[i] != bn[i] {
			if an[i] < bn[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	case ap < bp:
		return -1
	}
	return 1
}

// AssetFor picks the archive for an operating system and architecture:
// ts-cli_<version>_<goos>_<goarch>.tar.gz (.zip on Windows).
func (r Release) AssetFor(goos, goarch string) (ReleaseAsset, bool) {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	suffix := "_" + goos + "_" + goarch + ext
	for _, a := range r.Assets {
		if strings.HasPrefix(a.Name, "ts-cli_") && strings.HasSuffix(a.Name, suffix) {
			return a, true
		}
	}
	return ReleaseAsset{}, false
}

// Asset finds a release file by exact name (checksums.txt).
func (r Release) Asset(name string) (ReleaseAsset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return ReleaseAsset{}, false
}

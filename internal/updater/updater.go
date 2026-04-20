// Copyright (c) 2026 Aristarh Ucolov.
//
// Lightweight "is a newer release available?" probe. We deliberately do NOT
// auto-download or self-replace — the server admin is the one who decides
// when to upgrade. The manager just surfaces the information.
//
// The endpoint hits GitHub's public releases API. If the request fails (no
// internet, rate limit, repo renamed, etc.) we swallow the error and report
// "unknown" — an update checker should never take the panel down.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	githubAPI = "https://api.github.com/repos/AristarhUcolov/DayZ-Server-Manager/releases/latest"
	userAgent = "DayZ-Server-Manager-updater"
)

type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type CheckResult struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	DownloadURL     string `json:"downloadUrl,omitempty"`
	Error           string `json:"error,omitempty"`
}

func Check(ctx context.Context, currentVersion string) CheckResult {
	res := CheckResult{Current: currentVersion}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPI, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("github returned %s", resp.Status)
		return res
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		res.Error = err.Error()
		return res
	}
	res.Latest = strings.TrimPrefix(rel.TagName, "v")
	res.ReleaseURL = rel.HTMLURL
	for _, a := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(a.Name), ".exe") {
			res.DownloadURL = a.DownloadURL
			break
		}
	}
	res.UpdateAvailable = compareVersions(res.Latest, strings.TrimPrefix(currentVersion, "v")) > 0
	return res
}

// compareVersions returns 1 if a > b, -1 if a < b, 0 if equal. Only compares
// the dot-separated numeric prefix — anything after a hyphen is ignored so
// "0.2.0" and "0.2.0-rc1" compare as equal. Good enough for stable release
// tags; we don't need semver rigor here.
func compareVersions(a, b string) int {
	if i := strings.Index(a, "-"); i >= 0 {
		a = a[:i]
	}
	if i := strings.Index(b, "-"); i >= 0 {
		b = b[:i]
	}
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(ap) {
			fmt.Sscanf(ap[i], "%d", &ai)
		}
		if i < len(bp) {
			fmt.Sscanf(bp[i], "%d", &bi)
		}
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return 0
}

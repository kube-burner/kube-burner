// Copyright 2020 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/version"
	log "github.com/sirupsen/logrus"
)

// GitHubRelease represents a GitHub release response
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

// CheckLatestVersion fetches the latest release from GitHub and compares with current version
func CheckLatestVersion() error {
	currentVersion := version.Version
	if currentVersion == "" || currentVersion == "latest" {
		// Skip version check for development versions
		return nil
	}

	client := &http.Client{
		Timeout: 5 * time.Second, // Shorter timeout for automatic checks
	}

	resp, err := client.Get("https://api.github.com/repos/kube-burner/kube-burner/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check for latest version: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch latest release: HTTP %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to parse release information: %v", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersionClean := strings.TrimPrefix(currentVersion, "v")

	if latestVersion != currentVersionClean {
		log.Warnf("⚠️  A newer kube-burner version (%s) is available! Current: %s", release.TagName, currentVersion)
		log.Warnf("   Download: %s", release.HTMLURL)
	}
	// Don't show "up to date" message for automatic checks to avoid noise

	return nil
}

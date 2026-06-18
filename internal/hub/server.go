package hub

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/henrygd/beszel"
	"github.com/henrygd/beszel/internal/hub/utils"
)

// PublicAppInfo defines the structure of the public app information that will be injected into the HTML
type PublicAppInfo struct {
	BASE_PATH           string
	HUB_VERSION         string
	HUB_URL             string
	AGENT_IMAGE         string
	AGENT_REPO          string
	AGENT_MIRROR        string
	OAUTH_DISABLE_POPUP bool `json:"OAUTH_DISABLE_POPUP,omitempty"`
}

// modifyIndexHTML injects the public app information into the index.html content
func modifyIndexHTML(hub *Hub, html []byte) string {
	info := getPublicAppInfo(hub)
	content, err := json.Marshal(info)
	if err != nil {
		return string(html)
	}
	htmlContent := strings.ReplaceAll(string(html), "./", info.BASE_PATH)
	return strings.Replace(htmlContent, "\"{info}\"", string(content), 1)
}

func getPublicAppInfo(hub *Hub) PublicAppInfo {
	parsedURL, _ := url.Parse(hub.appURL)
	info := PublicAppInfo{
		BASE_PATH:   strings.TrimSuffix(parsedURL.Path, "/") + "/",
		HUB_VERSION: beszel.Version,
		HUB_URL:     hub.appURL,
		// AGENT_IMAGE overrides the Docker image shown in the add-system install
		// snippets (defaults to the upstream image). Forks that publish their own
		// agent image set this env var so new systems install the fork's agent.
		AGENT_IMAGE: "henrygd/beszel-agent",
		// AGENT_REPO (owner/repo) makes the binary install snippets and the
		// agent's self-update pull from a fork's GitHub releases instead of the
		// upstream henrygd/beszel. Empty for upstream (no behavior change).
		AGENT_REPO: "",
		// AGENT_MIRROR prefixes github.com for release-asset downloads in the
		// binary install snippet (passed as install-agent.sh --mirror). Needed by
		// forks whose release assets (release-assets.githubusercontent.com) are
		// blocked in some regions — upstream uses its own gh.beszel.dev proxy via
		// the script's --china-mirrors flag, which only serves upstream repos.
		// Empty for upstream (no behavior change).
		AGENT_MIRROR: "",
	}
	if val, _ := utils.GetEnv("AGENT_IMAGE"); val != "" {
		info.AGENT_IMAGE = val
	}
	if val, _ := utils.GetEnv("AGENT_REPO"); val != "" {
		info.AGENT_REPO = val
	}
	if val, _ := utils.GetEnv("AGENT_MIRROR"); val != "" {
		info.AGENT_MIRROR = val
	}
	if val, _ := utils.GetEnv("OAUTH_DISABLE_POPUP"); val == "true" {
		info.OAUTH_DISABLE_POPUP = true
	}
	return info
}

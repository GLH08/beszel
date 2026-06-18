//go:build testing

package hub

import (
	"os"
	"testing"
)

// TestUpdateApiURL verifies the hub update badge checks the fork's releases
// when AGENT_REPO is set, and falls back to the upstream default (empty URL)
// otherwise. An empty return lets ghupdate.FetchLatestRelease use its built-in
// upstream default, so upstream behavior is unchanged.
func TestUpdateApiURL(t *testing.T) {
	// Save and restore AGENT_REPO so the test is hermetic.
	orig := os.Getenv("AGENT_REPO")
	t.Cleanup(func() { os.Setenv("AGENT_REPO", orig) })

	t.Run("unset returns empty (upstream default)", func(t *testing.T) {
		os.Unsetenv("AGENT_REPO")
		if got := updateApiURL(); got != "" {
			t.Fatalf("expected empty URL for unset AGENT_REPO, got %q", got)
		}
	})

	t.Run("malformed returns empty (upstream default)", func(t *testing.T) {
		os.Setenv("AGENT_REPO", "no-slash-here")
		if got := updateApiURL(); got != "" {
			t.Fatalf("expected empty URL for malformed AGENT_REPO, got %q", got)
		}
	})

	t.Run("fork returns fork API URL", func(t *testing.T) {
		os.Setenv("AGENT_REPO", "glh08/beszel")
		got := updateApiURL()
		want := "https://api.github.com/repos/glh08/beszel/releases/latest"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}

// TestAgentRepoMalformed reports whether AGENT_REPO is set but has no owner/repo
// slash — in which case the resolvers silently fall back to upstream.
func TestAgentRepoMalformed(t *testing.T) {
	orig := os.Getenv("AGENT_REPO")
	t.Cleanup(func() { os.Setenv("AGENT_REPO", orig) })

	os.Unsetenv("AGENT_REPO")
	if agentRepoMalformed() {
		t.Fatal("unset AGENT_REPO should not be malformed")
	}

	os.Setenv("AGENT_REPO", "glh08")
	if !agentRepoMalformed() {
		t.Fatal("AGENT_REPO without slash should be malformed")
	}

	os.Setenv("AGENT_REPO", "glh08/beszel")
	if agentRepoMalformed() {
		t.Fatal("AGENT_REPO with slash should not be malformed")
	}
}

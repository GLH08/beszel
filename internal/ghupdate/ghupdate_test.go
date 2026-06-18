package ghupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseFindAssetBySuffix(t *testing.T) {
	r := release{
		Assets: []*releaseAsset{
			{Name: "test1.zip", Id: 1},
			{Name: "test2.zip", Id: 2},
			{Name: "test22.zip", Id: 22},
			{Name: "test3.zip", Id: 3},
		},
	}

	asset, err := r.findAssetBySuffix("2.zip")
	if err != nil {
		t.Fatalf("Expected nil, got err: %v", err)
	}

	if asset.Id != 2 {
		t.Fatalf("Expected asset with id %d, got %v", 2, asset)
	}
}

func TestShouldUseMirror(t *testing.T) {
	cases := []struct {
		name      string
		useMirror bool
		owner     string
		repo      string
		want      bool
	}{
		{"upstream with mirror", true, "henrygd", "beszel", true},
		{"upstream without mirror", false, "henrygd", "beszel", false},
		// gh.beszel.dev mirrors only henrygd repos; a fork must not use it.
		{"fork with mirror flag defused", true, "glh08", "beszel", false},
		{"fork without mirror flag", false, "glh08", "beszel", false},
		{"fork owner only defused", true, "glh08", "beszel", false},
		{"empty owner defused (falls back to upstream default in update, but guard is conservative)", true, "", "beszel", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldUseMirror(c.useMirror, c.owner, c.repo); got != c.want {
				t.Fatalf("shouldUseMirror(%v, %q, %q) = %v, want %v", c.useMirror, c.owner, c.repo, got, c.want)
			}
		})
	}
}

func TestExtractFailure(t *testing.T) {
	testDir := t.TempDir()

	// Test with missing zip file
	missingZipPath := filepath.Join(testDir, "missing_test.zip")
	extractedPath := filepath.Join(testDir, "zip_extract")

	if err := extract(missingZipPath, extractedPath); err == nil {
		t.Fatal("Expected Extract to fail due to missing zip file")
	}

	// Test with missing tar.gz file
	missingTarPath := filepath.Join(testDir, "missing_test.tar.gz")

	if err := extract(missingTarPath, extractedPath); err == nil {
		t.Fatal("Expected Extract to fail due to missing tar.gz file")
	}
}

// TestExtractTarGzRejectsPathTraversal builds a tar.gz with a "../escape"
// entry and asserts extractTarGz rejects it (Tar Slip). A clean tar.gz must
// still extract.
func TestExtractTarGzRejectsPathTraversal(t *testing.T) {
	writeTar := func(name string) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len("x")), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("x"))
		_ = tw.Close()
		_ = gz.Close()
		return buf.Bytes()
	}

	t.Run("rejects traversal entry", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "bad.tar.gz")
		if err := os.WriteFile(src, writeTar("../escape.txt"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := extractTarGz(src, filepath.Join(dir, "out")); err == nil {
			t.Fatal("expected error for path-traversal entry, got nil")
		}
	})

	t.Run("clean tar extracts", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "good.tar.gz")
		if err := os.WriteFile(src, writeTar("good.txt"), 0644); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out")
		if err := extractTarGz(src, out); err != nil {
			t.Fatalf("expected clean extract, got %v", err)
		}
		if _, err := os.Stat(filepath.Join(out, "good.txt")); err != nil {
			t.Fatalf("expected good.txt to exist: %v", err)
		}
	})
}

// TestParseChecksumLine extracts the hex digest for a file from a goreleaser
// checksums file body (lines: "<sha256>  <fileName>").
func TestParseChecksumLine(t *testing.T) {
	body := "abc123  beszel_linux_amd64.tar.gz\ndef456  beszel-agent_linux_amd64.tar.gz\n"
	cases := []struct{ file, want string }{
		{"beszel-agent_linux_amd64.tar.gz", "def456"},
		{"beszel_linux_amd64.tar.gz", "abc123"},
		{"missing.tar.gz", ""},
	}
	for _, c := range cases {
		if got := parseChecksumLine(body, c.file); got != c.want {
			t.Errorf("parseChecksumLine(%q) = %q, want %q", c.file, got, c.want)
		}
	}
}

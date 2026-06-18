package ghupdate

import (
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

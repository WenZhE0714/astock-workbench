package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRegimeStudyCopiesVerifiedFrozenInputs(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	data := []byte(`{"bars":[]}`)
	hash := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(source, "sample.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"sample.json": hex.EncodeToString(hash[:])}
	if err := copyVerifiedStudyInputs(source, target, files); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "sample.json"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyVerifiedStudyInputs(source, t.TempDir(), files); err == nil {
		t.Fatal("changed historical input accepted")
	}
	if err := copyVerifiedStudyInputs(source, t.TempDir(), map[string]string{"../outside": "bad"}); err == nil {
		t.Fatal("path traversal accepted")
	}
}

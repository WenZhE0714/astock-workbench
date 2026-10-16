package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFreezePositionStudyCacheCopiesOnlyRequestedTickers(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	data := []byte(`{"bars":[]}`)
	for _, symbol := range []string{"sh600000", "sh600001"} {
		if err := os.WriteFile(filepath.Join(source, symbol+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := freezePositionStudyCache(source, target, []string{"sh600000"})
	if err != nil || len(files) != 1 {
		t.Fatalf("unexpected input files: %+v %v", files, err)
	}
	hash := sha256.Sum256(data)
	if files["sh600000.json"] != hex.EncodeToString(hash[:]) {
		t.Fatal("input fingerprint incorrect")
	}
	if _, err := os.Stat(filepath.Join(target, "sh600001.json")); !os.IsNotExist(err) {
		t.Fatal("unrequested ticker copied")
	}
	if err := os.WriteFile(filepath.Join(source, "sh600000.json"), []byte(`{"changed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	frozen, _ := os.ReadFile(filepath.Join(target, "sh600000.json"))
	if string(frozen) != string(data) {
		t.Fatal("source update changed frozen input")
	}
	if _, err := freezePositionStudyCache(source, target, []string{"../bad"}); err == nil {
		t.Fatal("invalid ticker accepted")
	}
}

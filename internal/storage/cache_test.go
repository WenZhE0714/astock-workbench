package storage

import "testing"

func TestToPinyin(t *testing.T) {
	if value := ToPinyin("平安银行"); value != "ping an yin hang" {
		t.Fatalf("unexpected pinyin: %q", value)
	}
}

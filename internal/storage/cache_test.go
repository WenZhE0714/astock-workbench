package storage

import "testing"

func TestToPinyin(t *testing.T) {
	if value := ToPinyin("平安银行"); value != "ping an yin hang" {
		t.Fatalf("unexpected pinyin: %q", value)
	}
	if value := ToPinyin("日经225 KOSPI"); value != "ri jing 225 kospi" {
		t.Fatalf("ASCII letters or digits were split: %q", value)
	}
}

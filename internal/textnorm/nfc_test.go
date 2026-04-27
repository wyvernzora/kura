package textnorm

import "testing"

func TestNFC(t *testing.T) {
	value := NFC(" Cafe\u0301 ")
	if value.String() != "Café" {
		t.Fatalf("NFC = %q, want Café", value)
	}
	if NFC(" \t ").IsZero() {
		return
	}
	t.Fatal("blank normalized string is not zero")
}

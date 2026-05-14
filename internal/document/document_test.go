package document_test

import (
	"testing"

	"papwer-claw/internal/document"
)

func TestFormatDirName(t *testing.T) {
	got := document.FormatDirName("2026-05-13", "Finanzamt", "Letter")
	want := "2026-05-13_Finanzamt_Letter"

	if got != want {
		t.Errorf("FormatDirName() = %q, want %q", got, want)
	}
}

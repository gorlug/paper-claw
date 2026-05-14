package document_test

import (
	"context"
	"strings"
	"testing"

	"paper-claw/internal/document"
)

func TestExtractText_Stadtwerke(t *testing.T) {
	text, err := document.ExtractText(context.Background(), "../../testdata/stadtwerke-stromrechnung.pdf")
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	for _, want := range []string{"Stadtwerke", "Stromrechnung", "München"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected transcript to contain %q", want)
		}
	}
}

func TestExtractText_Finanzamt(t *testing.T) {
	text, err := document.ExtractText(context.Background(), "../../testdata/finanzamt-bescheid.pdf")
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	for _, want := range []string{"Finanzamt", "München"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected transcript to contain %q", want)
		}
	}
}

func TestExtractText_MissingFile(t *testing.T) {
	_, err := document.ExtractText(context.Background(), "/nonexistent/path/doc.pdf")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

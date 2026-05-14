package document_test

import (
	"testing"

	"paper-claw/internal/document"
)

func ptr(f float64) *float64 { return &f }

func validMetadata() document.Metadata {
	return document.Metadata{
		ID:             "a3b4c5d6e7f8a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a1b2c3d4e5f6a7b8",
		Type:           "utility_bill",
		DocumentDate:   "2026-04-01",
		Vendor:         "Stadtwerke München",
		Summary:        "Electricity bill for April 2026.",
		SourceFilename: "stadtwerke-stromrechnung.pdf",
		ProcessedAt:    "2026-05-14T10:00:00Z",
		ContentHash:    "a3b4c5d6e7f8a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a1b2c3d4e5f6a7b8",
	}
}

func TestValidateMetadata_Valid(t *testing.T) {
	m := validMetadata()
	if err := document.ValidateMetadata(&m); err != nil {
		t.Fatalf("expected valid metadata to pass: %v", err)
	}
}

func TestValidateMetadata_WithOptionalFields(t *testing.T) {
	m := validMetadata()
	m.Amount = ptr(89.50)
	m.Currency = "EUR"
	m.DueDate = "2026-04-30"
	m.Language = "de"
	m.Tags = []string{"electricity", "annual"}
	if err := document.ValidateMetadata(&m); err != nil {
		t.Fatalf("expected valid metadata with optional fields to pass: %v", err)
	}
}

func TestValidateMetadata_InvalidType(t *testing.T) {
	m := validMetadata()
	m.Type = "grocery_receipt"
	if err := document.ValidateMetadata(&m); err == nil {
		t.Fatal("expected invalid type to fail validation")
	}
}

func TestValidateMetadata_MissingRequiredField(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*document.Metadata)
	}{
		{"missing id", func(m *document.Metadata) { m.ID = "" }},
		{"missing type", func(m *document.Metadata) { m.Type = "" }},
		{"missing document_date", func(m *document.Metadata) { m.DocumentDate = "" }},
		{"missing vendor", func(m *document.Metadata) { m.Vendor = "" }},
		{"missing summary", func(m *document.Metadata) { m.Summary = "" }},
		{"missing source_filename", func(m *document.Metadata) { m.SourceFilename = "" }},
		{"missing processed_at", func(m *document.Metadata) { m.ProcessedAt = "" }},
		{"missing content_hash", func(m *document.Metadata) { m.ContentHash = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMetadata()
			tt.mutate(&m)
			if err := document.ValidateMetadata(&m); err == nil {
				t.Fatalf("expected validation to fail for %s", tt.name)
			}
		})
	}
}

func TestValidateMetadata_InvalidHash(t *testing.T) {
	m := validMetadata()
	m.ID = "tooshort"
	m.ContentHash = "tooshort"
	if err := document.ValidateMetadata(&m); err == nil {
		t.Fatal("expected short hash to fail validation")
	}
}

func TestValidateMetadata_FileDescriptionNotSerialized(t *testing.T) {
	m := validMetadata()
	m.FileDescription = "strom-rechnung"
	// FileDescription must not cause schema validation to fail (it uses json:"-")
	if err := document.ValidateMetadata(&m); err != nil {
		t.Fatalf("FileDescription should not appear in JSON and not cause failure: %v", err)
	}
}

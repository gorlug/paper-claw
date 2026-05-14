package document

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed schema.json
var schemaBytes []byte

// Metadata holds structured information extracted from a document.
type Metadata struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	DocumentDate   string   `json:"document_date"`
	Vendor         string   `json:"vendor"`
	Summary        string   `json:"summary"`
	SourceFilename string   `json:"source_filename"`
	ProcessedAt    string   `json:"processed_at"`
	ContentHash    string   `json:"content_hash"`
	Amount         *float64 `json:"amount,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	DueDate        string   `json:"due_date,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Language       string   `json:"language,omitempty"`

	// FileDescription is a short slug used to name the library directory.
	// It is not stored in metadata.json (json:"-").
	FileDescription string `json:"-"`
}

// ValidateMetadata validates m against the embedded JSON Schema.
func ValidateMetadata(m *Metadata) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}
	schemaLoader := gojsonschema.NewBytesLoader(schemaBytes)
	docLoader := gojsonschema.NewBytesLoader(data)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return fmt.Errorf("schema validation: %w", err)
	}
	if !result.Valid() {
		msgs := make([]string, 0, len(result.Errors()))
		for _, e := range result.Errors() {
			msgs = append(msgs, e.String())
		}
		return fmt.Errorf("invalid metadata: %s", strings.Join(msgs, "; "))
	}
	return nil
}

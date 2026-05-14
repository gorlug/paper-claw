package document_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xeipuuv/gojsonschema"
)

func TestSchemaIsValidJSON(t *testing.T) {
	data, err := os.ReadFile("schema.json")
	if err != nil {
		t.Fatalf("reading schema.json: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema.json is not valid JSON: %v", err)
	}
	if _, ok := schema["$schema"]; !ok {
		t.Error("schema.json missing $schema field")
	}
}

func TestAllExpectedFilesConformToSchema(t *testing.T) {
	schemaAbs, err := filepath.Abs("schema.json")
	if err != nil {
		t.Fatalf("abs path for schema.json: %v", err)
	}
	schemaLoader := gojsonschema.NewReferenceLoader("file://" + schemaAbs)

	files, err := filepath.Glob("../../testdata/expected/*.json")
	if err != nil {
		t.Fatalf("globbing expected files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no expected JSON files found under testdata/expected/")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			abs, err := filepath.Abs(f)
			if err != nil {
				t.Fatalf("abs path for %s: %v", f, err)
			}
			docLoader := gojsonschema.NewReferenceLoader("file://" + abs)
			result, err := gojsonschema.Validate(schemaLoader, docLoader)
			if err != nil {
				t.Fatalf("validation error: %v", err)
			}
			if !result.Valid() {
				for _, e := range result.Errors() {
					t.Errorf("  %s", e.String())
				}
			}
		})
	}
}

func TestSampleJSONConformsToSchema(t *testing.T) {
	schemaBytes, err := os.ReadFile("schema.json")
	if err != nil {
		t.Fatalf("reading schema.json: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("parsing schema.json: %v", err)
	}

	sampleBytes, err := os.ReadFile("../../testdata/expected/sample.json")
	if err != nil {
		t.Fatalf("reading testdata/expected/sample.json: %v", err)
	}
	var sample map[string]any
	if err := json.Unmarshal(sampleBytes, &sample); err != nil {
		t.Fatalf("parsing sample.json: %v", err)
	}

	props, _ := schema["properties"].(map[string]any)

	// All required fields must be present.
	required, _ := schema["required"].([]any)
	for _, r := range required {
		field, _ := r.(string)
		if _, ok := sample[field]; !ok {
			t.Errorf("required field %q missing from sample", field)
		}
	}

	// No fields outside the schema's properties are allowed.
	for k := range sample {
		if _, ok := props[k]; !ok {
			t.Errorf("sample has undeclared property %q", k)
		}
	}

	// The type field must be one of the enum values.
	typeProp, _ := props["type"].(map[string]any)
	enum, _ := typeProp["enum"].([]any)
	sampleType, _ := sample["type"].(string)
	validType := false
	for _, v := range enum {
		if s, _ := v.(string); s == sampleType {
			validType = true
			break
		}
	}
	if !validType {
		t.Errorf("sample type %q is not in enum", sampleType)
	}

	// id and content_hash must be 64-char hex strings.
	for _, field := range []string{"id", "content_hash"} {
		val, _ := sample[field].(string)
		if len(val) != 64 {
			t.Errorf("field %q must be 64 chars, got %d", field, len(val))
		}
		for _, c := range val {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("field %q contains non-hex character %q", field, c)
				break
			}
		}
	}
}

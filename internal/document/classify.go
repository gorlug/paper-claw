package document

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Classifier classifies a PDF transcript and returns structured Metadata.
type Classifier interface {
	Classify(ctx context.Context, transcript, sourceFilename, contentHash string, processedAt time.Time) (Metadata, error)
}

// classifierProps is the LLM-extracted subset of fields (excludes system-set fields).
type classifierProps struct {
	Type            string   `json:"type"`
	DocumentDate    string   `json:"document_date"`
	Vendor          string   `json:"vendor"`
	Summary         string   `json:"summary"`
	FileDescription string   `json:"file_description"`
	Amount          *float64 `json:"amount,omitempty"`
	Currency        string   `json:"currency,omitempty"`
	DueDate         string   `json:"due_date,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Language        string   `json:"language,omitempty"`
}

var classifierInputSchema = anthropic.ToolInputSchemaParam{
	Properties: map[string]any{
		"type": map[string]any{
			"type":        "string",
			"enum":        []string{"invoice", "utility_bill", "bank_statement", "insurance_letter", "contract", "government_letter", "other"},
			"description": "Document category.",
		},
		"document_date": map[string]any{
			"type":        "string",
			"description": "ISO 8601 date (YYYY-MM-DD) extracted from the document content.",
		},
		"vendor": map[string]any{
			"type":        "string",
			"description": "Full name of the issuer or sender.",
		},
		"summary": map[string]any{
			"type":        "string",
			"description": "One-sentence agent-readable summary of the document.",
		},
		"file_description": map[string]any{
			"type":        "string",
			"description": "Short 2-4 word description for the filename, lowercase hyphenated (e.g. strom-rechnung, steuerbescheid-2024).",
		},
		"amount": map[string]any{
			"type":        "number",
			"minimum":     0,
			"description": "Monetary amount if present.",
		},
		"currency": map[string]any{
			"type":        "string",
			"description": "ISO 4217 currency code, e.g. EUR.",
		},
		"due_date": map[string]any{
			"type":        "string",
			"description": "ISO 8601 due date if present.",
		},
		"tags": map[string]any{
			"type":  "array",
			"items": map[string]any{"type": "string"},
		},
		"language": map[string]any{
			"type":        "string",
			"description": "BCP-47 language tag, e.g. de or en.",
		},
	},
	Required: []string{"type", "document_date", "vendor", "summary", "file_description"},
}

const classifyToolName = "classify_document"

// ClaudeClassifier calls Claude Sonnet 4.6 via the Anthropic API.
type ClaudeClassifier struct {
	client anthropic.Client
}

// NewClaudeClassifier creates a classifier using ANTHROPIC_API_KEY from the environment.
func NewClaudeClassifier() *ClaudeClassifier {
	return &ClaudeClassifier{client: anthropic.NewClient()}
}

// Classify calls the Anthropic API to classify a document transcript and returns structured metadata.
func (c *ClaudeClassifier) Classify(ctx context.Context, transcript, sourceFilename, contentHash string, processedAt time.Time) (Metadata, error) {
	prompt := fmt.Sprintf(
		"Classify the following document and extract metadata.\n\nFilename: %s\n\nContent:\n%s",
		sourceFilename, transcript,
	)
	msg, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 1024,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &anthropic.ToolParam{
				Name:        classifyToolName,
				Description: anthropic.String("Classify a document and extract its metadata."),
				InputSchema: classifierInputSchema,
			}},
		},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{Name: classifyToolName},
		},
	})
	if err != nil {
		return Metadata{}, fmt.Errorf("claude API: %w", err)
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == classifyToolName {
			var props classifierProps
			if err := json.Unmarshal(block.Input, &props); err != nil {
				return Metadata{}, fmt.Errorf("parsing classifier response: %w", err)
			}
			return Metadata{
				ID:              contentHash,
				Type:            props.Type,
				DocumentDate:    props.DocumentDate,
				Vendor:          props.Vendor,
				Summary:         props.Summary,
				FileDescription: props.FileDescription,
				SourceFilename:  sourceFilename,
				ProcessedAt:     processedAt.UTC().Format(time.RFC3339),
				ContentHash:     contentHash,
				Amount:          props.Amount,
				Currency:        props.Currency,
				DueDate:         props.DueDate,
				Tags:            props.Tags,
				Language:        props.Language,
			}, nil
		}
	}
	return Metadata{}, fmt.Errorf("no tool_use block in classifier response")
}

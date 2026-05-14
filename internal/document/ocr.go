package document

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExtractText extracts the text content from a PDF using pdftotext.
// Returns a non-empty string on success; error if the tool fails or produces empty output.
func ExtractText(ctx context.Context, pdfPath string) (string, error) {
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "pdftotext", pdfPath, "-") //nolint:gosec // path is from trusted inbox
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("pdftotext: empty transcript for %s", pdfPath)
	}
	return text, nil
}

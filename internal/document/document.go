// Package document provides core domain types, pipeline orchestration, and
// OCR/classification logic for paper-claw.
package document

import (
	"fmt"
	"strings"
)

var umlautMap = strings.NewReplacer(
	"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
	"Ä", "ae", "Ö", "oe", "Ü", "ue",
)

func toSlug(s string) string {
	s = umlautMap.Replace(s)
	s = strings.ToLower(s)
	var b strings.Builder
	prevWasSep := true
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevWasSep = false
		} else if !prevWasSep {
			b.WriteRune('-')
			prevWasSep = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func truncateSlug(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	s = s[:maxLen]
	if idx := strings.LastIndex(s, "-"); idx > 0 {
		return s[:idx]
	}
	return s
}

// FormatDirName returns the library directory name for a document.
// Vendor is truncated to 30 slug characters; description to 40.
// German umlauts are transliterated (ä→ae, ö→oe, ü→ue, ß→ss).
func FormatDirName(date, vendor, description string) string {
	v := truncateSlug(toSlug(vendor), 30)
	d := truncateSlug(toSlug(description), 40)
	return date + "_" + v + "_" + d
}

// UniqueDirName returns a collision-free version of base.
// If base is not taken, it is returned unchanged.
// Otherwise -2, -3, … suffixes are tried until a free slot is found.
func UniqueDirName(base string, exists func(string) bool) string {
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !exists(candidate) {
			return candidate
		}
	}
}

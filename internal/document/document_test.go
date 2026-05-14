package document_test

import (
	"testing"

	"paper-claw/internal/document"
)

func TestFormatDirName(t *testing.T) {
	tests := []struct {
		date, vendor, desc, want string
	}{
		{
			"2026-05-13", "Finanzamt", "Letter",
			"2026-05-13_finanzamt_letter",
		},
		{
			"2026-04-01", "Stadtwerke", "Strom-Rechnung",
			"2026-04-01_stadtwerke_strom-rechnung",
		},
		{
			"2026-01-15", "Müller GmbH", "Überweisung Bestätigung",
			"2026-01-15_mueller-gmbh_ueberweisung-bestaetigung",
		},
		{
			"2026-01-15", "Straße GmbH", "Bescheid",
			"2026-01-15_strasse-gmbh_bescheid",
		},
		{
			// vendor truncated at word boundary ≤30 slug chars
			"2026-01-15", "A Very Long Vendor Name That Exceeds Thirty Characters Total", "Short Desc",
			"2026-01-15_a-very-long-vendor-name-that_short-desc",
		},
		{
			// description truncated at word boundary ≤40 slug chars
			"2026-01-15", "Vendor", "A Description That Is Quite Long And Exceeds Forty Characters",
			"2026-01-15_vendor_a-description-that-is-quite-long-and",
		},
		{
			// leading/trailing non-alnum characters are stripped
			"2026-01-15", "  leading-trailing  ", "  spaces  ",
			"2026-01-15_leading-trailing_spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := document.FormatDirName(tt.date, tt.vendor, tt.desc)
			if got != tt.want {
				t.Errorf("FormatDirName(%q, %q, %q) = %q, want %q",
					tt.date, tt.vendor, tt.desc, got, tt.want)
			}
		})
	}
}

func TestUniqueDirName(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		taken map[string]bool
		want  string
	}{
		{
			name: "no collision",
			base: "2026-05-13_finanzamt_letter",
			want: "2026-05-13_finanzamt_letter",
		},
		{
			name:  "one collision",
			base:  "2026-05-13_finanzamt_letter",
			taken: map[string]bool{"2026-05-13_finanzamt_letter": true},
			want:  "2026-05-13_finanzamt_letter-2",
		},
		{
			name: "two collisions",
			base: "2026-05-13_finanzamt_letter",
			taken: map[string]bool{
				"2026-05-13_finanzamt_letter":   true,
				"2026-05-13_finanzamt_letter-2": true,
			},
			want: "2026-05-13_finanzamt_letter-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := document.UniqueDirName(tt.base, func(s string) bool {
				return tt.taken[s]
			})
			if got != tt.want {
				t.Errorf("UniqueDirName(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

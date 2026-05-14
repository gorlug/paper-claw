package document

// FormatDirName returns the library directory name for a document.
// Format: YYYY-MM-DD_Vendor_Description
func FormatDirName(date, vendor, description string) string {
	return date + "_" + vendor + "_" + description
}

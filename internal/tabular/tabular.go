// Package tabular splits markdown table rows.
package tabular

import "strings"

// SplitRow splits one table row into trimmed cells.
func SplitRow(line string) []string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// IsSeparator reports whether cells is a header separator row.
func IsSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c == "" {
			continue
		}
		if strings.Trim(strings.ReplaceAll(c, " ", ""), "-:") != "" {
			return false
		}
	}
	return true
}

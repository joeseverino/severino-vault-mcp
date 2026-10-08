// Package clock is the module's one source of "now" and calendar dates, so
// tests can pin time.
package clock

import (
	"fmt"
	"time"
)

// Now returns the current local time. Tests replace it.
var Now = time.Now

// Today is the current local date at midnight.
func Today() time.Time {
	n := Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
}

// TodayISO is today as YYYY-MM-DD.
func TodayISO() string { return Today().Format("2006-01-02") }

// ParseDate parses YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) {
	if len(s) != 10 {
		return time.Time{}, fmt.Errorf("Invalid isoformat string: %q", s) //nolint:staticcheck // wording is part of the CLI's error contract
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("Invalid isoformat string: %q", s) //nolint:staticcheck // wording is part of the CLI's error contract
	}
	return t, nil
}

// Date builds a validated calendar date.
func Date(year, month, day int) (time.Time, error) {
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return time.Time{}, fmt.Errorf("day is out of range for month")
	}
	return t, nil
}

// DaysBetween counts calendar days from a to b.
func DaysBetween(a, b time.Time) int {
	ad := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	bd := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(bd.Sub(ad).Hours() / 24)
}

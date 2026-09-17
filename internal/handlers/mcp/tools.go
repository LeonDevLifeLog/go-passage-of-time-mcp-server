package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	mcp_go "github.com/mark3labs/mcp-go/mcp"
)

const dateFormat = "2006-01-02"

// YYYY-MM-DD HH:MM:SS
const dateTimeFormat = "2006-01-02 15:04:05"

// TimeManager supplies the readings used by the relative tools.  It is an
// interface so tests can pin "now" to a fixed instant.
type TimeManager interface {
	Now() time.Time
}

// TimeOpts is a struct that holds options for parsing time.
type TimeOpts struct {
	input      string
	timeFormat string
}

// ParseTime parses a wall-clock date/time reading.
//
// Times are plain local readings with no time zone attached: "2026-04-19
// 14:00:00" simply denotes the 19th of April at 14:00.  Both accepted layouts
// are zone-free, so the result is always built in UTC -- not because the input
// is understood as UTC, but because UTC carries no daylight-saving rules and
// therefore gives a stable, rule-free frame for the arithmetic.  A date-only
// input means midnight at the start of that day.
func ParseTime(opts *TimeOpts) (time.Time, error) {
	if opts == nil {
		return time.Time{}, NewNilTimeOptsError()
	}
	if opts.input == "" {
		return time.Time{}, NewNilInputTime()
	}

	if opts.timeFormat != "" {
		t, err := time.ParseInLocation(opts.timeFormat, opts.input, time.UTC)
		if err != nil {
			return time.Time{}, NewInvalidTimeFormatError(opts.input)
		}
		return t, nil
	}

	t, err := time.ParseInLocation(dateTimeFormat, opts.input, time.UTC)
	if err != nil {
		t, err = time.ParseInLocation(dateFormat, opts.input, time.UTC)
		if err != nil {
			return time.Time{}, NewInvalidTimeFormatError(opts.input)
		}
	}
	return t, nil
}

// hoursPerDay is the length of the "d" (day) unit accepted by ParseDuration.
const hoursPerDay = 24

// ParseDuration parses a duration string.  It accepts everything that
// time.ParseDuration accepts ("1h30m", "30m", "90s", "1.5h", ...) plus a "d"
// unit for days, where "1d" is exactly 24 hours.
//
// Days are an absolute length, so "1d" is identical to "24h" and "1d3h" is
// identical to "27h".
func ParseDuration(s string) (time.Duration, error) {
	// Fast path: no day unit present, so time.ParseDuration is authoritative.
	if !strings.ContainsAny(s, "dD") {
		if d, err := time.ParseDuration(s); err == nil {
			return d, nil
		}
		return 0, fmt.Errorf("invalid duration %q", s)
	}

	// Up to two passes are needed: "1d" -> "24h" -> parsed.
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	expanded, err := expandDayUnits(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	if d, err := time.ParseDuration(expanded); err == nil {
		return d, nil
	}
	return 0, fmt.Errorf("invalid duration %q", s)
}

// expandDayUnits rewrites every "<number>d" segment into an equivalent number
// of hours, leaving every other character untouched so that time.ParseDuration
// remains responsible for validating (and reporting errors on) the rest.
func expandDayUnits(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		start := i
		for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
			i++
		}
		if start == i {
			// Not a number: copy the character (covers sign and unit letters).
			b.WriteByte(s[i])
			i++
			continue
		}

		number := s[start:i]
		if i < len(s) && (s[i] == 'd' || s[i] == 'D') {
			days, err := strconv.ParseFloat(number, 64)
			if err != nil {
				return "", fmt.Errorf("invalid duration %q", s)
			}
			b.WriteString(strconv.FormatFloat(days*hoursPerDay, 'f', -1, 64))
			b.WriteByte('h')
			i++ // consume the day unit
			continue
		}
		b.WriteString(number)
	}
	return b.String(), nil
}

// LiveTimeManager reads the real clock.
type LiveTimeManager struct{}

func (l *LiveTimeManager) Now() time.Time {
	return time.Now()
}

// wallClock strips the zone from a reading, keeping only the digits a person
// would read off a clock face.  Parsed inputs are already zone-free, so "now"
// must be reduced the same way before the two are compared or subtracted --
// otherwise the host's UTC offset leaks into every relative calculation.
func wallClock(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// now returns the current reading on the same zone-free frame as ParseTime.
func (s *Server) now() time.Time {
	return wallClock(s.TimeManager.Now())
}

func (s *Server) CurrentDateTime(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	t := s.now()

	zoneName, offsetSeconds := s.TimeManager.Now().Zone()
	slog.InfoContext(ctx, "CurrentDateTime", slog.String("server_zone", zoneName), slog.Int("server_offset_seconds", offsetSeconds))

	return mcp_go.NewToolResultText(t.Format(dateTimeFormat)), nil
}

func (s *Server) TimeSince(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}

	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}

	now := s.now()

	if t.After(now) {
		return mcp_go.NewToolResultError("The specified time is in the future"), nil
	}
	if t.Equal(now) {
		return mcp_go.NewToolResultText("The specified time is now."), nil
	}

	return mcp_go.NewToolResultText(now.Sub(t).String()), nil
}

func (s *Server) TimeUntil(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}

	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}

	now := s.now()

	if t.Before(now) {
		return mcp_go.NewToolResultError("The specified time is in the past"), nil
	}

	return mcp_go.NewToolResultText(t.Sub(now).String()), nil
}

func (s *Server) TimeDifference(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	firstDateTime := request.GetString("firstDateTime", "")
	secondDateTime := request.GetString("secondDateTime", "")
	if firstDateTime == "" || secondDateTime == "" {
		return mcp_go.NewToolResultError("Both firstDateTime and secondDateTime must be provided"), nil
	}

	firstTime, err := ParseTime(&TimeOpts{input: firstDateTime})
	if err != nil {
		return mcp_go.NewToolResultErrorFromErr("error with first input time", err), nil
	}
	secondTime, err := ParseTime(&TimeOpts{input: secondDateTime})
	if err != nil {
		return mcp_go.NewToolResultErrorFromErr("error with second input time", err), nil
	}

	if firstTime.Equal(secondTime) {
		return mcp_go.NewToolResultText("The two times are equal."), nil
	}
	if firstTime.Before(secondTime) {
		return mcp_go.NewToolResultText("The first time is earlier than the second time by " + secondTime.Sub(firstTime).String()), nil
	}
	return mcp_go.NewToolResultText("The first time is later than the second time by " + firstTime.Sub(secondTime).String()), nil
}

func (s *Server) IsLeapYear(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	year := request.GetInt("year", 0)
	if year <= 0 {
		return mcp_go.NewToolResultError("Invalid year provided"), nil
	}
	isLeap := (year%4 == 0 && year%100 != 0) || (year%400 == 0)
	result := "is not a leap year."
	if isLeap {
		result = "is a leap year."
	}
	return mcp_go.NewToolResultText(fmt.Sprintf("%d %s", year, result)), nil
}

func (s *Server) DayOfWeek(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}

	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}

	return mcp_go.NewToolResultText(fmt.Sprintf("The day of the week for %s is %s.", t.Format(dateFormat), t.Weekday().String())), nil
}

// AddDuration adds a signed duration to a date/time.  A negative duration
// subtracts, so "-2h" moves the reading two hours earlier.
func (s *Server) AddDuration(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}

	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}

	durationStr := request.GetString("duration", "")
	if durationStr == "" {
		return mcp_go.NewToolResultError("Duration must be provided"), nil
	}

	duration, err := ParseDuration(durationStr)
	if err != nil {
		return mcp_go.NewToolResultError(fmt.Sprintf("Invalid duration format: %s: %v", durationStr, err)), nil
	}

	newTime := t.Add(duration)
	return mcp_go.NewToolResultText(fmt.Sprintf("New time after adding duration: %s", newTime.Format(dateTimeFormat))), nil
}

// NextOccurrence calculates the next date for a specified day of the week (e.g. "Monday") after a given date.
func (s *Server) NextOccurrence(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}
	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}
	dayOfWeekStr := request.GetString("dayOfWeek", "")
	if dayOfWeekStr == "" {
		return mcp_go.NewToolResultError("Day of week must be provided"), nil
	}
	dayOfWeek, err := parseWeekday(dayOfWeekStr)
	if err != nil {
		return mcp_go.NewToolResultError(fmt.Sprintf("Invalid day of week format: %s", dayOfWeekStr)), nil
	}

	nextOccurrence := t
	if nextOccurrence.Weekday() == dayOfWeek {
		// Already on the requested day, so move a full week forward.
		nextOccurrence = nextOccurrence.AddDate(0, 0, 7)
	} else {
		for nextOccurrence.Weekday() != dayOfWeek {
			nextOccurrence = nextOccurrence.AddDate(0, 0, 1)
		}
	}

	slog.InfoContext(ctx, "NextOccurrence", slog.String("input_time", t.Format(dateTimeFormat)), slog.String("requested_day_of_week", dayOfWeekStr), slog.String("next_occurrence", nextOccurrence.Format(dateTimeFormat)))
	return mcp_go.NewToolResultText(fmt.Sprintf("The next occurrence of %s after %s is %s.", dayOfWeekStr, t.Format(dateTimeFormat), nextOccurrence.Format(dateTimeFormat))), nil
}

func (s *Server) PreviousOccurrence(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}
	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}
	dayOfWeekStr := request.GetString("dayOfWeek", "")
	if dayOfWeekStr == "" {
		return mcp_go.NewToolResultError("Day of week must be provided"), nil
	}
	dayOfWeek, err := parseWeekday(dayOfWeekStr)
	if err != nil {
		return mcp_go.NewToolResultError(fmt.Sprintf("Invalid day of week format: %s", dayOfWeekStr)), nil
	}

	prevOccurrence := t
	if prevOccurrence.Weekday() == dayOfWeek {
		// Already on the requested day, so move a full week back.
		prevOccurrence = prevOccurrence.AddDate(0, 0, -7)
	} else {
		for prevOccurrence.Weekday() != dayOfWeek {
			prevOccurrence = prevOccurrence.AddDate(0, 0, -1)
		}
	}

	slog.InfoContext(ctx, "PreviousOccurrence", slog.String("input_time", t.Format(dateTimeFormat)), slog.String("requested_day_of_week", dayOfWeekStr), slog.String("previous_occurrence", prevOccurrence.Format(dateTimeFormat)))
	return mcp_go.NewToolResultText(fmt.Sprintf("The previous occurrence of %s before %s is %s.", dayOfWeekStr, t.Format(dateTimeFormat), prevOccurrence.Format(dateTimeFormat))), nil
}

func (s *Server) IsWeekend(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}
	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}

	result := "is not a weekend."
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		result = "is a weekend."
	}
	return mcp_go.NewToolResultText(fmt.Sprintf("%s %s", t.Format(dateFormat), result)), nil
}

func (s *Server) IsWeekday(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	input := request.GetString("dateTime", "")
	if input == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}
	t, err := ParseTime(&TimeOpts{input: input})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}

	result := "is not a weekday."
	if t.Weekday() >= time.Monday && t.Weekday() <= time.Friday {
		result = "is a weekday."
	}
	return mcp_go.NewToolResultText(fmt.Sprintf("%s %s", t.Format(dateFormat), result)), nil
}

// DaysBetween returns the whole number of calendar days between two dates.
// The declared parameters are firstDate/secondDate; firstDateTime/secondDateTime
// are accepted as aliases for callers that pass full date/time strings.
func (s *Server) DaysBetween(ctx context.Context, request mcp_go.CallToolRequest) (*mcp_go.CallToolResult, error) {
	firstInput := request.GetString("firstDate", "")
	if firstInput == "" {
		firstInput = request.GetString("firstDateTime", "")
	}
	if firstInput == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}
	secondInput := request.GetString("secondDate", "")
	if secondInput == "" {
		secondInput = request.GetString("secondDateTime", "")
	}
	if secondInput == "" {
		return mcp_go.NewToolResultError(NewNilInputTime().Error()), nil
	}

	firstTime, err := ParseTime(&TimeOpts{input: firstInput})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}
	secondTime, err := ParseTime(&TimeOpts{input: secondInput})
	if err != nil {
		return mcp_go.NewToolResultError(err.Error()), nil
	}

	// Compare civil dates only, so the result is a whole number of days and any
	// time component is ignored rather than truncated away mid-day.
	firstDate := civilDate(firstTime)
	secondDate := civilDate(secondTime)
	daysBetween := int(secondDate.Sub(firstDate).Hours() / 24)

	return mcp_go.NewToolResultText(fmt.Sprintf("There are %d days between %s and %s.", daysBetween, firstDate.Format(dateFormat), secondDate.Format(dateFormat))), nil
}

// civilDate truncates a time to its calendar date at midnight UTC.
func civilDate(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// parseWeekday maps a string to a time.Weekday value.
func parseWeekday(s string) (time.Weekday, error) {
	switch normalizeWeekdayString(s) {
	case "sunday":
		return time.Sunday, nil
	case "monday":
		return time.Monday, nil
	case "tuesday":
		return time.Tuesday, nil
	case "wednesday":
		return time.Wednesday, nil
	case "thursday":
		return time.Thursday, nil
	case "friday":
		return time.Friday, nil
	case "saturday":
		return time.Saturday, nil
	default:
		return time.Sunday, fmt.Errorf("invalid weekday: %s", s)
	}
}

func normalizeWeekdayString(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

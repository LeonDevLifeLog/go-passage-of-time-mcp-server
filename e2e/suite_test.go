// The behavioural suite.  Every function here is transport-agnostic: it asserts
// what the server does, never how the bytes travel, so running it over each
// transport proves the two agree.
//
// Transport-specific expectations live in stdio_test.go and http_test.go.
package e2e

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// transport returns a fresh, handshaken client for each transport.
func transports(t *testing.T) map[string]mcpTransport {
	t.Helper()
	return map[string]mcpTransport{
		"stdio": startStdio(t),
		"http":  startHTTP(t),
	}
}

// ---------------------------------------------------------------------------
// Handshake and discovery
// ---------------------------------------------------------------------------

func testHandshakeAndToolDiscovery(t *testing.T, tx mcpTransport) {
	tools := tx.ListTools(t)

	got := map[string]bool{}
	for _, tl := range tools {
		got[tl.Name] = true
	}
	for _, name := range expectedTools {
		if !got[name] {
			t.Errorf("tool %q missing from tools/list", name)
		}
	}
	if len(tools) != len(expectedTools) {
		t.Errorf("tools/list returned %d tools, want %d", len(tools), len(expectedTools))
	}
	// The merged design must not expose a separate subtract tool.
	if got["subtractDuration"] {
		t.Error("subtractDuration should have been merged into addDuration")
	}

	for _, tl := range tools {
		if tl.Description == "" {
			t.Errorf("tool %q has no description", tl.Name)
		}
		if tl.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want object", tl.Name, tl.InputSchema.Type)
		}
		if tl.Annotations.ReadOnlyHint == nil || !*tl.Annotations.ReadOnlyHint {
			t.Errorf("tool %q is not annotated read-only", tl.Name)
		}
		// No time-zone parameter should survive the removal.
		for _, banned := range []string{"timeZone", "firstTimeZone", "secondTimeZone"} {
			if _, ok := tl.InputSchema.Properties[banned]; ok {
				t.Errorf("tool %q still advertises the removed property %q", tl.Name, banned)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Every tool actually works
// ---------------------------------------------------------------------------

func testEveryToolIsCallable(t *testing.T, tx mcpTransport) {
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"addDuration", map[string]any{"dateTime": "2026-04-19 14:00:00", "duration": "1h30m"}},
		{"timeDifference", map[string]any{"firstDateTime": "2026-04-19 09:00:00", "secondDateTime": "2026-04-19 17:30:00"}},
		{"daysBetween", map[string]any{"firstDate": "2026-04-01", "secondDate": "2026-04-19"}},
		{"dayOfWeek", map[string]any{"dateTime": "2026-04-19"}},
		{"isWeekday", map[string]any{"dateTime": "2026-04-20"}},
		{"isWeekend", map[string]any{"dateTime": "2026-04-19"}},
		{"isLeapYear", map[string]any{"year": 2028}},
		{"nextOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Monday"}},
		{"previousOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Friday"}},
		{"currentDateTime", map[string]any{}},
		{"timeSince", map[string]any{"dateTime": "2020-01-01 00:00:00"}},
		{"timeUntil", map[string]any{"dateTime": "2099-01-01 00:00:00"}},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			text, isErr := tx.CallTool(t, tc.tool, tc.args)
			if isErr {
				t.Fatalf("%s returned an error: %s", tc.tool, text)
			}
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s returned empty text", tc.tool)
			}
			t.Logf("%s -> %s", tc.tool, text)
		})
	}
}

// testDaysBetweenRegression is the regression for the schema / parameter
// mismatch that previously made every call fail.
func testDaysBetweenRegression(t *testing.T, tx mcpTransport) {
	text, isErr := tx.CallTool(t, "daysBetween", map[string]any{
		"firstDate": "2026-04-01", "secondDate": "2026-04-19",
	})
	if isErr {
		t.Fatalf("daysBetween returned an error: %s", text)
	}
	want := "There are 18 days between 2026-04-01 and 2026-04-19."
	if text != want {
		t.Errorf("daysBetween = %q, want %q", text, want)
	}
}

// ---------------------------------------------------------------------------
// The wall clock is preserved
// ---------------------------------------------------------------------------

// testWallClockIsPreserved is the regression for the old zone-shifting bug:
// readings must come back exactly as given, with no offset.
func testWallClockIsPreserved(t *testing.T, tx mcpTransport) {
	cases := []struct {
		desc  string
		input string
		want  string
	}{
		{"afternoon", "2026-04-19 14:00:00", "2026-04-19 14:00:00"},
		{"late evening is not shifted into the next day", "2026-04-19 23:30:00", "2026-04-19 23:30:00"},
		{"early morning is not shifted into the previous day", "2026-04-19 00:30:00", "2026-04-19 00:30:00"},
		{"a date-only input means midnight", "2026-04-19", "2026-04-19 00:00:00"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			text, isErr := tx.CallTool(t, "addDuration", map[string]any{
				"dateTime": tc.input, "duration": "0s",
			})
			if isErr {
				t.Fatalf("error: %s", text)
			}
			want := "New time after adding duration: " + tc.want
			if text != want {
				t.Errorf("got %q, want %q", text, want)
			}
		})
	}
}

// testOutputsCarryNoZoneSuffix confirms no tool ever emits a UTC offset.
func testOutputsCarryNoZoneSuffix(t *testing.T, tx mcpTransport) {
	zoneSuffix := regexp.MustCompile(`[+-]\d{4}\b`)

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"currentDateTime", map[string]any{}},
		{"addDuration", map[string]any{"dateTime": "2026-04-19 14:00:00", "duration": "1h"}},
		{"nextOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Monday"}},
		{"previousOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Friday"}},
	}

	for _, call := range calls {
		t.Run(call.tool, func(t *testing.T) {
			text, isErr := tx.CallTool(t, call.tool, call.args)
			if isErr {
				t.Fatalf("error: %s", text)
			}
			if m := zoneSuffix.FindString(text); m != "" {
				t.Errorf("%s emitted a zone suffix %q in %q", call.tool, m, text)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Durations
// ---------------------------------------------------------------------------

// testDurationDayUnit covers the "d" unit.
func testDurationDayUnit(t *testing.T, tx mcpTransport) {
	cases := []struct {
		desc     string
		duration string
		want     string
	}{
		{"one day", "1d", "New time after adding duration: 2026-04-20 14:00:00"},
		{"days plus hours", "1d3h", "New time after adding duration: 2026-04-20 17:00:00"},
		{"two days", "2d", "New time after adding duration: 2026-04-21 14:00:00"},
		{"days match hours exactly", "24h", "New time after adding duration: 2026-04-20 14:00:00"},
		{"still supports hours and minutes", "1h30m", "New time after adding duration: 2026-04-19 15:30:00"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			text, isErr := tx.CallTool(t, "addDuration", map[string]any{
				"dateTime": "2026-04-19 14:00:00", "duration": tc.duration,
			})
			if isErr {
				t.Fatalf("error: %s", text)
			}
			if text != tc.want {
				t.Errorf("addDuration(%q) = %q, want %q", tc.duration, text, tc.want)
			}
		})
	}

	t.Run("invalid duration is rejected", func(t *testing.T) {
		text, isErr := tx.CallTool(t, "addDuration", map[string]any{
			"dateTime": "2026-04-19 14:00:00", "duration": "1w",
		})
		if !isErr {
			t.Errorf("addDuration with duration '1w' should have failed, got %q", text)
		}
	})
}

// testAddDurationIsTheSubtractTool covers the merge: negative durations subtract.
func testAddDurationIsTheSubtractTool(t *testing.T, tx mcpTransport) {
	cases := []struct {
		desc     string
		duration string
		want     string
	}{
		{"positive adds", "2h", "New time after adding duration: 2026-04-19 16:00:00"},
		{"negative subtracts", "-2h", "New time after adding duration: 2026-04-19 12:00:00"},
		{"negative day subtracts", "-1d", "New time after adding duration: 2026-04-18 14:00:00"},
		{"zero is a no-op", "0s", "New time after adding duration: 2026-04-19 14:00:00"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			text, isErr := tx.CallTool(t, "addDuration", map[string]any{
				"dateTime": "2026-04-19 14:00:00", "duration": tc.duration,
			})
			if isErr {
				t.Fatalf("error: %s", text)
			}
			if text != tc.want {
				t.Errorf("addDuration(%q) = %q, want %q", tc.duration, text, tc.want)
			}
		})
	}
}

// testNoDaylightSavingAdjustments proves the server applies no zone rules: the
// same duration always advances the clock by the same amount, regardless of
// which real-world daylight-saving transition the date happens to straddle.
func testNoDaylightSavingAdjustments(t *testing.T, tx mcpTransport) {
	cases := []struct {
		desc  string
		input string
		want  string
	}{
		{"US spring-forward date", "2026-03-07 12:00:00", "2026-03-08 12:00:00"},
		{"US fall-back date", "2026-10-31 12:00:00", "2026-11-01 12:00:00"},
		{"EU spring-forward date", "2026-03-28 12:00:00", "2026-03-29 12:00:00"},
		{"EU fall-back date", "2026-10-24 12:00:00", "2026-10-25 12:00:00"},
		{"an ordinary date for comparison", "2026-04-19 12:00:00", "2026-04-20 12:00:00"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			text, isErr := tx.CallTool(t, "addDuration", map[string]any{
				"dateTime": tc.input, "duration": "1d",
			})
			if isErr {
				t.Fatalf("error: %s", text)
			}
			want := "New time after adding duration: " + tc.want
			if text != want {
				t.Errorf("addDuration(1d) = %q, want %q", text, want)
			}
		})
	}

	t.Run("elapsed span is plain arithmetic", func(t *testing.T) {
		text, isErr := tx.CallTool(t, "timeDifference", map[string]any{
			"firstDateTime": "2026-03-07 12:00:00", "secondDateTime": "2026-03-09 12:00:00",
		})
		if isErr {
			t.Fatalf("error: %s", text)
		}
		want := "The first time is earlier than the second time by 48h0m0s"
		if text != want {
			t.Errorf("timeDifference = %q, want %q", text, want)
		}
	})
}

// ---------------------------------------------------------------------------
// Calendars
// ---------------------------------------------------------------------------

// testWeekdayToolsSpotChecks pins known-correct weekday answers.
func testWeekdayToolsSpotChecks(t *testing.T, tx mcpTransport) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"dayOfWeek", map[string]any{"dateTime": "2026-04-19"}, "The day of the week for 2026-04-19 is Sunday."},
		{"isWeekend", map[string]any{"dateTime": "2026-04-19"}, "2026-04-19 is a weekend."},
		{"isWeekday", map[string]any{"dateTime": "2026-04-20"}, "2026-04-20 is a weekday."},
		{"isWeekday", map[string]any{"dateTime": "2026-04-19"}, "2026-04-19 is not a weekday."},
		{"isLeapYear", map[string]any{"year": 2028}, "2028 is a leap year."},
		{"isLeapYear", map[string]any{"year": 1900}, "1900 is not a leap year."},
		{"isLeapYear", map[string]any{"year": 2000}, "2000 is a leap year."},
		{"nextOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Monday"},
			"The next occurrence of Monday after 2026-04-19 00:00:00 is 2026-04-20 00:00:00."},
		{"previousOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Friday"},
			"The previous occurrence of Friday before 2026-04-19 00:00:00 is 2026-04-17 00:00:00."},
		{"nextOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Sunday"},
			"The next occurrence of Sunday after 2026-04-19 00:00:00 is 2026-04-26 00:00:00."},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%s_%d", tc.tool, i), func(t *testing.T) {
			text, isErr := tx.CallTool(t, tc.tool, tc.args)
			if isErr {
				t.Fatalf("error: %s", text)
			}
			if text != tc.want {
				t.Errorf("%s = %q, want %q", tc.tool, text, tc.want)
			}
		})
	}
}

// testTimeDifferencePinpoints pins the three response shapes.
func testTimeDifferencePinpoints(t *testing.T, tx mcpTransport) {
	cases := []struct {
		desc   string
		first  string
		second string
		want   string
	}{
		{
			desc:  "same-day span",
			first: "2026-04-19 09:00:00", second: "2026-04-19 17:30:00",
			want: "The first time is earlier than the second time by 8h30m0s",
		},
		{
			desc:  "reversed",
			first: "2026-04-19 17:30:00", second: "2026-04-19 09:00:00",
			want: "The first time is later than the second time by 8h30m0s",
		},
		{
			desc:  "equal",
			first: "2026-04-19 09:00:00", second: "2026-04-19 09:00:00",
			want: "The two times are equal.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			text, isErr := tx.CallTool(t, "timeDifference", map[string]any{
				"firstDateTime": tc.first, "secondDateTime": tc.second,
			})
			if isErr {
				t.Fatalf("error: %s", text)
			}
			if text != tc.want {
				t.Errorf("got %q, want %q", text, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Failure handling
// ---------------------------------------------------------------------------

// testErrorHandling verifies bad input yields a well-formed tool error rather
// than a crash or an empty result.
func testErrorHandling(t *testing.T, tx mcpTransport) {
	cases := []struct {
		desc string
		tool string
		args map[string]any
	}{
		{"empty dateTime", "dayOfWeek", map[string]any{"dateTime": ""}},
		{"malformed date", "dayOfWeek", map[string]any{"dateTime": "not-a-date"}},
		{"missing dateTime", "addDuration", map[string]any{"duration": "1h"}},
		{"missing duration", "addDuration", map[string]any{"dateTime": "2026-04-19"}},
		{"timeSince in the future", "timeSince", map[string]any{"dateTime": "2099-01-01 00:00:00"}},
		{"timeUntil in the past", "timeUntil", map[string]any{"dateTime": "2000-01-01 00:00:00"}},
		{"invalid weekday", "nextOccurrence", map[string]any{"dateTime": "2026-04-19", "dayOfWeek": "Funday"}},
		{"invalid year", "isLeapYear", map[string]any{"year": 0}},
		{"missing second date", "daysBetween", map[string]any{"firstDate": "2026-04-01"}},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			text, isErr := tx.CallTool(t, tc.tool, tc.args)
			if !isErr {
				t.Errorf("%s%v should have errored, got %q", tc.tool, tc.args, text)
			}
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s returned an empty error message", tc.tool)
			}
		})
	}
}

// testServerSurvivesInvalidInput checks the process stays healthy after a batch
// of bad calls and still answers a valid one.
func testServerSurvivesInvalidInput(t *testing.T, tx mcpTransport) {
	for i := 0; i < 5; i++ {
		tx.CallTool(t, "dayOfWeek", map[string]any{"dateTime": "garbage"})
		tx.CallTool(t, "addDuration", map[string]any{"dateTime": "", "duration": ""})
		tx.CallTool(t, "notATool", map[string]any{})
	}

	text, isErr := tx.CallTool(t, "dayOfWeek", map[string]any{"dateTime": "2026-04-19"})
	if isErr {
		t.Fatalf("server unhealthy after invalid input: %s", text)
	}
	if text != "The day of the week for 2026-04-19 is Sunday." {
		t.Errorf("got %q after recovery", text)
	}
}

// ---------------------------------------------------------------------------
// Output shapes
// ---------------------------------------------------------------------------

// testDurationOutputShape confirms the duration-returning tools emit a Go
// duration string that can be round-tripped.
func testDurationOutputShape(t *testing.T, tx mcpTransport) {
	re := regexp.MustCompile(`^\d+h\d+m[\d.]+s$`)

	text, isErr := tx.CallTool(t, "timeSince", map[string]any{"dateTime": "2000-01-01 00:00:00"})
	if isErr {
		t.Fatalf("timeSince error: %s", text)
	}
	if !re.MatchString(text) {
		t.Errorf("timeSince returned %q, want a Go duration string", text)
	}

	text, isErr = tx.CallTool(t, "timeUntil", map[string]any{"dateTime": "2099-01-01 00:00:00"})
	if isErr {
		t.Fatalf("timeUntil error: %s", text)
	}
	if !re.MatchString(text) {
		t.Errorf("timeUntil returned %q, want a Go duration string", text)
	}
}

// testCurrentDateTimeShape checks the shape of the clock reading.
func testCurrentDateTimeShape(t *testing.T, tx mcpTransport) {
	text, isErr := tx.CallTool(t, "currentDateTime", map[string]any{})
	if isErr {
		t.Fatalf("currentDateTime error: %s", text)
	}
	layout := "2006-01-02 15:04:05"
	if _, err := time.Parse(layout, text); err != nil {
		t.Errorf("currentDateTime returned %q, which is not %q: %v", text, layout, err)
	}
}

// ---------------------------------------------------------------------------
// Multi-tool workflows
// ---------------------------------------------------------------------------

// testStackedWorkflow exercises a realistic multi-tool sequence in one session.
func testStackedWorkflow(t *testing.T, tx mcpTransport) {
	// "Next Monday, what date is it, is it a working day, and how far away?"
	next, isErr := tx.CallTool(t, "nextOccurrence", map[string]any{
		"dateTime": "2026-04-19", "dayOfWeek": "Monday",
	})
	if isErr {
		t.Fatalf("nextOccurrence: %s", next)
	}

	dateRe := regexp.MustCompile(`is (\d{4}-\d{2}-\d{2}) `)
	m := dateRe.FindStringSubmatch(next)
	if m == nil {
		t.Fatalf("could not extract a date from %q", next)
	}
	date := m[1]
	if date != "2026-04-20" {
		t.Fatalf("nextOccurrence gave %q, want 2026-04-20", date)
	}

	dow, isErr := tx.CallTool(t, "dayOfWeek", map[string]any{"dateTime": date})
	if isErr {
		t.Fatalf("dayOfWeek: %s", dow)
	}
	if !strings.HasSuffix(dow, "is Monday.") {
		t.Errorf("dayOfWeek(%s) = %q, want Monday", date, dow)
	}

	wd, isErr := tx.CallTool(t, "isWeekday", map[string]any{"dateTime": date})
	if isErr {
		t.Fatalf("isWeekday: %s", wd)
	}
	if !strings.Contains(wd, "is a weekday.") {
		t.Errorf("isWeekday(%s) = %q, want a weekday", date, wd)
	}

	span, isErr := tx.CallTool(t, "daysBetween", map[string]any{
		"firstDate": "2026-04-19", "secondDate": date,
	})
	if isErr {
		t.Fatalf("daysBetween: %s", span)
	}
	if span != "There are 1 days between 2026-04-19 and 2026-04-20." {
		t.Errorf("daysBetween = %q", span)
	}

	end, isErr := tx.CallTool(t, "addDuration", map[string]any{
		"dateTime": date + " 09:00:00", "duration": "8h",
	})
	if isErr {
		t.Fatalf("addDuration: %s", end)
	}
	if end != "New time after adding duration: 2026-04-20 17:00:00" {
		t.Errorf("addDuration = %q, want 2026-04-20 17:00:00", end)
	}

	back, isErr := tx.CallTool(t, "addDuration", map[string]any{
		"dateTime": date + " 17:00:00", "duration": "-8h",
	})
	if isErr {
		t.Fatalf("addDuration(-8h): %s", back)
	}
	if back != "New time after adding duration: 2026-04-20 09:00:00" {
		t.Errorf("addDuration(-8h) = %q, want 2026-04-20 09:00:00", back)
	}
}

// testRelativeToolsUseTheLocalClock is the guard for the frame that
// currentDateTime, timeSince and timeUntil all share.
//
// The unit fixtures mock "now" with a UTC reading, which hides any offset
// between the server's clock and the parsed inputs.  This test runs the real
// binary against the real clock instead, so a mismatch shows up as an hours-
// wide error (or a spurious "in the future"/"in the past" rejection).
func testRelativeToolsUseTheLocalClock(t *testing.T, tx mcpTransport) {
	clock, isErr := tx.CallTool(t, "currentDateTime", map[string]any{})
	if isErr {
		t.Fatalf("currentDateTime error: %s", clock)
	}

	now, err := time.ParseInLocation("2006-01-02 15:04:05", clock, time.UTC)
	if err != nil {
		t.Fatalf("currentDateTime returned %q, which is not a plain reading: %v", clock, err)
	}

	// The reading must be the host's wall clock, not UTC.
	if local := time.Now().Format("2006-01-02 15:04:05"); clock != local {
		t.Errorf("currentDateTime = %q, want the local clock %q", clock, local)
	}

	// Thirty minutes before the reported reading must be reported as past.
	past := now.Add(-30 * time.Minute).Format("2006-01-02 15:04:05")
	text, isErr := tx.CallTool(t, "timeSince", map[string]any{"dateTime": past})
	if isErr {
		t.Fatalf("timeSince(%q) rejected a past local reading: %s", past, text)
	}
	elapsed, err := time.ParseDuration(text)
	if err != nil {
		t.Fatalf("timeSince returned %q, not a duration: %v", text, err)
	}
	if elapsed < 29*time.Minute || elapsed > 31*time.Minute {
		t.Errorf("timeSince(%q) = %v, want ~30m", past, elapsed)
	}

	// Thirty minutes after it must be reported as future.
	future := now.Add(30 * time.Minute).Format("2006-01-02 15:04:05")
	text, isErr = tx.CallTool(t, "timeUntil", map[string]any{"dateTime": future})
	if isErr {
		t.Fatalf("timeUntil(%q) rejected a future local reading: %s", future, text)
	}
	remaining, err := time.ParseDuration(text)
	if err != nil {
		t.Fatalf("timeUntil returned %q, not a duration: %v", text, err)
	}
	if remaining < 29*time.Minute || remaining > 31*time.Minute {
		t.Errorf("timeUntil(%q) = %v, want ~30m", future, remaining)
	}
}

// ---------------------------------------------------------------------------
// Entry points
// ---------------------------------------------------------------------------

// transportSuite is the registry of transport-agnostic cases.  Adding a case
// here runs it over every transport in transports(), so neither one can fall
// behind the other.
var transportSuite = map[string]func(*testing.T, mcpTransport){
	"HandshakeAndToolDiscovery":     testHandshakeAndToolDiscovery,
	"EveryToolIsCallable":           testEveryToolIsCallable,
	"DaysBetweenRegression":         testDaysBetweenRegression,
	"WallClockIsPreserved":          testWallClockIsPreserved,
	"OutputsCarryNoZoneSuffix":      testOutputsCarryNoZoneSuffix,
	"DurationDayUnit":               testDurationDayUnit,
	"AddDurationIsTheSubtractTool":  testAddDurationIsTheSubtractTool,
	"NoDaylightSavingAdjustments":   testNoDaylightSavingAdjustments,
	"WeekdayToolsSpotChecks":        testWeekdayToolsSpotChecks,
	"TimeDifferencePinpoints":       testTimeDifferencePinpoints,
	"ErrorHandling":                 testErrorHandling,
	"ServerSurvivesInvalidInput":    testServerSurvivesInvalidInput,
	"DurationOutputShape":           testDurationOutputShape,
	"CurrentDateTimeShape":          testCurrentDateTimeShape,
	"StackedWorkflow":               testStackedWorkflow,
	"RelativeToolsUseTheLocalClock": testRelativeToolsUseTheLocalClock,
}

// TestMCPOverEveryTransport runs the behavioural suite once per transport, so
// stdio and Streamable HTTP are held to exactly the same standard.
func TestMCPOverEveryTransport(t *testing.T) {
	for name, tx := range transports(t) {
		t.Run(name, func(t *testing.T) {
			for caseName, run := range transportSuite {
				t.Run(caseName, func(t *testing.T) {
					run(t, tx)
				})
			}
		})
	}
}

// suiteToolsAndShape captures one transport's catalogue in the form a client
// sees it: names, descriptions and full input schemas.
func suiteToolsAndShape(t *testing.T, tx mcpTransport) []toolDef {
	t.Helper()
	tools := tx.ListTools(t)
	// A comparison between two empty lists would prove nothing.
	if len(tools) == 0 {
		t.Fatalf("%s advertised no tools at all; the cross-transport comparison would be vacuous", tx.Name())
	}
	out := make([]toolDef, 0, len(tools))
	for _, tl := range tools {
		out = append(out, tl)
	}
	return out
}

// TestTransportsAdvertiseTheSameCatalogue is the cross-transport check: the two
// transports are two wirings of one server, so a client must not be able to
// tell them apart from tools/list alone.
func TestTransportsAdvertiseTheSameCatalogue(t *testing.T) {
	overStdio := suiteToolsAndShape(t, startStdio(t))
	overHTTP := suiteToolsAndShape(t, startHTTP(t))

	if !reflect.DeepEqual(overStdio, overHTTP) {
		stdioNames := make([]string, len(overStdio))
		for i, tl := range overStdio {
			stdioNames[i] = tl.Name
		}
		httpNames := make([]string, len(overHTTP))
		for i, tl := range overHTTP {
			httpNames[i] = tl.Name
		}
		t.Errorf("tool catalogues differ between transports\nstdio: %v\nhttp:  %v", stdioNames, httpNames)
	}
}

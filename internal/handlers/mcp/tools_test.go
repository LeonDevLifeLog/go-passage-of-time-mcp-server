package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

type mockTmanager struct{}

func (m *mockTmanager) Now() time.Time {
	return time.Date(2023, 10, 1, 12, 30, 0, 0, time.UTC)
}

func TestParseTime(t *testing.T) {
	testCases := []struct {
		desc    string
		opts    *TimeOpts
		want    time.Time
		wanterr bool
	}{
		{
			desc: "Valid date and time",
			opts: &TimeOpts{
				input: "2023-10-01 12:30:00",
			},
			want:    time.Date(2023, 10, 1, 12, 30, 0, 0, time.UTC),
			wanterr: false,
		},
		{
			desc: "Valid date only",
			opts: &TimeOpts{
				input: "2023-10-01",
			},
			want:    time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC),
			wanterr: false,
		},
		{
			desc: "Valid date with custom format",
			opts: &TimeOpts{
				input:      "01-10-2023",
				timeFormat: "02-01-2006",
			},
			want:    time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC),
			wanterr: false,
		},
		{
			desc: "Valid date and time with custom format",
			opts: &TimeOpts{
				input:      "01-10-2023 12:30",
				timeFormat: "02-01-2006 15:04",
			},
			want:    time.Date(2023, 10, 1, 12, 30, 0, 0, time.UTC),
			wanterr: false,
		},
		{
			desc: "Bad custom format",
			opts: &TimeOpts{
				input:      "01-10-2023 12:30",
				timeFormat: "02-01-2006 15:04:05",
			},
			want:    time.Time{},
			wanterr: true,
		},
		{
			desc: "Invalid date format",
			opts: &TimeOpts{
				input: "2023-10-01 12:30",
			},
			want:    time.Time{},
			wanterr: true,
		},
		{
			desc: "Empty input",
			opts: &TimeOpts{
				input: "",
			},
			want:    time.Time{},
			wanterr: true,
		},
		{
			desc:    "Nil options",
			opts:    nil,
			want:    time.Time{},
			wanterr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := ParseTime(tc.opts)
			if (err != nil) != tc.wanterr {
				t.Errorf("ParseTime() error = %v, wantErr %v", err, tc.wanterr)
				return
			}
			if !got.Equal(tc.want) {
				t.Errorf("ParseTime() got = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCurrentDateTime(t *testing.T) {
	ctx := context.Background()
	s := &Server{TimeManager: &mockTmanager{}}

	got, err := s.CurrentDateTime(ctx, mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("CurrentDateTime() error = %v", err)
	}
	want := "2023-10-01 12:30:00"
	if text := textOf(t, got); text != want {
		t.Errorf("CurrentDateTime() = %q, want %q", text, want)
	}
}

func TestTimeSince(t *testing.T) {
	testCases := []struct {
		desc     string
		dateTime string
		want     string
		wantErr  bool
	}{
		{
			desc:     "Past date and time",
			dateTime: "2023-09-30 12:00:00",
			want:     "24h30m0s",
		},
		{
			desc:     "Past date only",
			dateTime: "2023-09-30",
			want:     "36h30m0s",
		},
		{
			desc:     "Exactly now",
			dateTime: "2023-10-01 12:30:00",
			want:     "The specified time is now.",
		},
		{
			desc:     "Future is rejected",
			dateTime: "2023-10-02 12:30:00",
			wantErr:  true,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, err := s.TimeSince(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{"dateTime": tc.dateTime}},
			})
			if err != nil {
				t.Fatalf("TimeSince() error = %v", err)
			}
			if got.IsError != tc.wantErr {
				t.Fatalf("TimeSince() IsError = %v, want %v", got.IsError, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("TimeSince() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestTimeUntil(t *testing.T) {
	testCases := []struct {
		desc     string
		dateTime string
		want     string
		wantErr  bool
	}{
		{
			desc:     "Future date and time",
			dateTime: "2023-10-02 12:30:00",
			want:     "24h0m0s",
		},
		{
			desc:     "Future date only",
			dateTime: "2023-10-02",
			want:     "11h30m0s",
		},
		{
			desc:     "Past is rejected",
			dateTime: "2023-09-30 12:00:00",
			wantErr:  true,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, err := s.TimeUntil(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{"dateTime": tc.dateTime}},
			})
			if err != nil {
				t.Fatalf("TimeUntil() error = %v", err)
			}
			if got.IsError != tc.wantErr {
				t.Fatalf("TimeUntil() IsError = %v, want %v", got.IsError, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("TimeUntil() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestTimeDifference(t *testing.T) {
	testCases := []struct {
		desc   string
		first  string
		second string
		want   string
	}{
		{
			desc:   "first earlier",
			first:  "2023-09-30 12:00:00",
			second: "2023-10-01 12:30:00",
			want:   "The first time is earlier than the second time by 24h30m0s",
		},
		{
			desc:   "first later",
			first:  "2023-10-01 12:30:00",
			second: "2023-09-30 12:00:00",
			want:   "The first time is later than the second time by 24h30m0s",
		},
		{
			desc:   "equal",
			first:  "2023-10-01 12:30:00",
			second: "2023-10-01 12:30:00",
			want:   "The two times are equal.",
		},
		{
			desc:   "date only versus date and time",
			first:  "2023-10-01",
			second: "2023-10-01 12:30:00",
			want:   "The first time is earlier than the second time by 12h30m0s",
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, err := s.TimeDifference(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{
					"firstDateTime":  tc.first,
					"secondDateTime": tc.second,
				}},
			})
			if err != nil {
				t.Fatalf("TimeDifference() error = %v", err)
			}
			if got.IsError {
				t.Fatalf("TimeDifference() returned error: %s", textOf(t, got))
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("TimeDifference() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestIsLeapYear(t *testing.T) {
	testCases := []struct {
		desc    string
		year    int
		want    string
		wantErr bool
	}{
		{desc: "divisible by 4 but not 100", year: 2024, want: "2024 is a leap year."},
		{desc: "not divisible by 4", year: 2023, want: "2023 is not a leap year."},
		{desc: "divisible by 100 but not 400", year: 1900, want: "1900 is not a leap year."},
		{desc: "divisible by 400", year: 2000, want: "2000 is a leap year."},
		{desc: "zero", year: 0, wantErr: true},
		{desc: "negative", year: -2020, wantErr: true},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.IsLeapYear(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{"year": tc.year}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("IsLeapYear() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("IsLeapYear() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestDayOfWeek(t *testing.T) {
	testCases := []struct {
		desc    string
		input   string
		want    string
		wantErr bool
	}{
		{desc: "date", input: "2023-10-01", want: "The day of the week for 2023-10-01 is Sunday."},
		{desc: "date and time", input: "2023-10-02 15:00:00", want: "The day of the week for 2023-10-02 is Monday."},
		{desc: "empty", input: "", wantErr: true},
		{desc: "bad format", input: "10/01/2023", wantErr: true},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.DayOfWeek(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{"dateTime": tc.input}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("DayOfWeek() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("DayOfWeek() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestAddDuration(t *testing.T) {
	testCases := []struct {
		desc     string
		dateTime string
		duration string
		want     string
		wantErr  bool
	}{
		{
			desc:     "add one hour",
			dateTime: "2023-10-01 12:30:00",
			duration: "1h",
			want:     "New time after adding duration: 2023-10-01 13:30:00",
		},
		{
			desc:     "add thirty minutes",
			dateTime: "2023-10-01 12:30:00",
			duration: "30m",
			want:     "New time after adding duration: 2023-10-01 13:00:00",
		},
		{
			desc:     "negative duration subtracts",
			dateTime: "2023-10-01 12:30:00",
			duration: "-1h",
			want:     "New time after adding duration: 2023-10-01 11:30:00",
		},
		{
			desc:     "negative duration subtracts two hours",
			dateTime: "2023-10-01 12:30:00",
			duration: "-2h",
			want:     "New time after adding duration: 2023-10-01 10:30:00",
		},
		{
			desc:     "add one day",
			dateTime: "2023-10-01 12:30:00",
			duration: "1d",
			want:     "New time after adding duration: 2023-10-02 12:30:00",
		},
		{
			desc:     "add days and hours",
			dateTime: "2023-10-01 12:30:00",
			duration: "1d3h",
			want:     "New time after adding duration: 2023-10-02 15:30:00",
		},
		{
			desc:     "add days crossing a month boundary",
			dateTime: "2023-10-01 12:30:00",
			duration: "2d",
			want:     "New time after adding duration: 2023-10-03 12:30:00",
		},
		{
			desc:     "date only input",
			dateTime: "2023-10-01",
			duration: "1h",
			want:     "New time after adding duration: 2023-10-01 01:00:00",
		},
		{
			desc:     "missing dateTime",
			dateTime: "",
			duration: "1h",
			wantErr:  true,
		},
		{
			desc:     "missing duration",
			dateTime: "2023-10-01 12:30:00",
			duration: "",
			wantErr:  true,
		},
		{
			desc:     "invalid duration",
			dateTime: "2023-10-01 12:30:00",
			duration: "abc",
			wantErr:  true,
		},
		{
			desc:     "invalid dateTime",
			dateTime: "not-a-date",
			duration: "1h",
			wantErr:  true,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.AddDuration(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{
					"dateTime": tc.dateTime,
					"duration": tc.duration,
				}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("AddDuration() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("AddDuration() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestNextOccurrence(t *testing.T) {
	testCases := []struct {
		desc      string
		dateTime  string
		dayOfWeek string
		want      string
		wantErr   bool
	}{
		{
			desc:      "next Monday after Sunday",
			dateTime:  "2023-10-01 12:30:00",
			dayOfWeek: "Monday",
			want:      "The next occurrence of Monday after 2023-10-01 12:30:00 is 2023-10-02 12:30:00.",
		},
		{
			desc:      "next Sunday after Sunday is a week away",
			dateTime:  "2023-10-01 12:30:00",
			dayOfWeek: "Sunday",
			want:      "The next occurrence of Sunday after 2023-10-01 12:30:00 is 2023-10-08 12:30:00.",
		},
		{
			desc:      "next Wednesday after Monday",
			dateTime:  "2023-10-02 09:00:00",
			dayOfWeek: "Wednesday",
			want:      "The next occurrence of Wednesday after 2023-10-02 09:00:00 is 2023-10-04 09:00:00.",
		},
		{
			desc:      "case insensitive",
			dateTime:  "2023-10-01",
			dayOfWeek: "monday",
			want:      "The next occurrence of monday after 2023-10-01 00:00:00 is 2023-10-02 00:00:00.",
		},
		{desc: "missing dateTime", dateTime: "", dayOfWeek: "Monday", wantErr: true},
		{desc: "missing dayOfWeek", dateTime: "2023-10-01", dayOfWeek: "", wantErr: true},
		{desc: "invalid dayOfWeek", dateTime: "2023-10-01", dayOfWeek: "Funday", wantErr: true},
		{desc: "invalid dateTime", dateTime: "not-a-date", dayOfWeek: "Monday", wantErr: true},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.NextOccurrence(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{
					"dateTime":  tc.dateTime,
					"dayOfWeek": tc.dayOfWeek,
				}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("NextOccurrence() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("NextOccurrence() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestPreviousOccurrence(t *testing.T) {
	testCases := []struct {
		desc      string
		dateTime  string
		dayOfWeek string
		want      string
		wantErr   bool
	}{
		{
			desc:      "previous Monday before Sunday",
			dateTime:  "2023-10-01 12:30:00",
			dayOfWeek: "Monday",
			want:      "The previous occurrence of Monday before 2023-10-01 12:30:00 is 2023-09-25 12:30:00.",
		},
		{
			desc:      "previous Sunday before Sunday is a week back",
			dateTime:  "2023-10-01 12:30:00",
			dayOfWeek: "Sunday",
			want:      "The previous occurrence of Sunday before 2023-10-01 12:30:00 is 2023-09-24 12:30:00.",
		},
		{
			desc:      "previous Wednesday before Monday",
			dateTime:  "2023-10-02 09:00:00",
			dayOfWeek: "Wednesday",
			want:      "The previous occurrence of Wednesday before 2023-10-02 09:00:00 is 2023-09-27 09:00:00.",
		},
		{desc: "missing dateTime", dateTime: "", dayOfWeek: "Monday", wantErr: true},
		{desc: "missing dayOfWeek", dateTime: "2023-10-01", dayOfWeek: "", wantErr: true},
		{desc: "invalid dayOfWeek", dateTime: "2023-10-01", dayOfWeek: "Funday", wantErr: true},
		{desc: "invalid dateTime", dateTime: "not-a-date", dayOfWeek: "Monday", wantErr: true},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.PreviousOccurrence(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{
					"dateTime":  tc.dateTime,
					"dayOfWeek": tc.dayOfWeek,
				}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("PreviousOccurrence() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("PreviousOccurrence() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestIsWeekend(t *testing.T) {
	testCases := []struct {
		desc    string
		date    string
		want    string
		wantErr bool
	}{
		{desc: "Sunday", date: "2023-10-01", want: "2023-10-01 is a weekend."},
		{desc: "Saturday", date: "2023-09-30", want: "2023-09-30 is a weekend."},
		{desc: "Monday", date: "2023-10-02", want: "2023-10-02 is not a weekend."},
		{desc: "empty", date: "", wantErr: true},
		{desc: "bad format", date: "not-a-date", wantErr: true},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.IsWeekend(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{"dateTime": tc.date}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("IsWeekend() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("IsWeekend() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestIsWeekday(t *testing.T) {
	testCases := []struct {
		desc    string
		date    string
		want    string
		wantErr bool
	}{
		{desc: "Monday", date: "2023-10-02", want: "2023-10-02 is a weekday."},
		{desc: "Friday", date: "2023-10-06", want: "2023-10-06 is a weekday."},
		{desc: "Sunday", date: "2023-10-01", want: "2023-10-01 is not a weekday."},
		{desc: "Saturday", date: "2023-09-30", want: "2023-09-30 is not a weekday."},
		{desc: "empty", date: "", wantErr: true},
		{desc: "bad format", date: "not-a-date", wantErr: true},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.IsWeekday(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{"dateTime": tc.date}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("IsWeekday() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("IsWeekday() = %q, want %q", text, tc.want)
			}
		})
	}
}

func TestDaysBetween(t *testing.T) {
	testCases := []struct {
		desc       string
		firstDate  string
		secondDate string
		want       string
		wantErr    bool
	}{
		{
			desc:       "one day between consecutive dates",
			firstDate:  "2023-10-01",
			secondDate: "2023-10-02",
			want:       "There are 1 days between 2023-10-01 and 2023-10-02.",
		},
		{
			desc:       "zero days between the same date",
			firstDate:  "2023-10-01",
			secondDate: "2023-10-01",
			want:       "There are 0 days between 2023-10-01 and 2023-10-01.",
		},
		{
			desc:       "negative when reversed",
			firstDate:  "2023-10-02",
			secondDate: "2023-10-01",
			want:       "There are -1 days between 2023-10-02 and 2023-10-01.",
		},
		{
			desc:       "across a month boundary",
			firstDate:  "2023-09-28",
			secondDate: "2023-10-01",
			want:       "There are 3 days between 2023-09-28 and 2023-10-01.",
		},
		{
			desc:       "time components are ignored, not truncated",
			firstDate:  "2023-10-01 23:00:00",
			secondDate: "2023-10-03 01:00:00",
			want:       "There are 2 days between 2023-10-01 and 2023-10-03.",
		},
		{desc: "missing first", firstDate: "", secondDate: "2023-10-01", wantErr: true},
		{desc: "missing second", firstDate: "2023-10-01", secondDate: "", wantErr: true},
		{desc: "invalid first", firstDate: "not-a-date", secondDate: "2023-10-01", wantErr: true},
		{desc: "invalid second", firstDate: "2023-10-01", secondDate: "not-a-date", wantErr: true},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			s := &Server{TimeManager: &mockTmanager{}}
			got, _ := s.DaysBetween(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{Arguments: map[string]any{
					"firstDate":  tc.firstDate,
					"secondDate": tc.secondDate,
				}},
			})
			if tc.wantErr {
				if got == nil || !got.IsError {
					t.Errorf("DaysBetween() expected error, got = %+v", got)
				}
				return
			}
			if text := textOf(t, got); text != tc.want {
				t.Errorf("DaysBetween() = %q, want %q", text, tc.want)
			}
		})
	}
}

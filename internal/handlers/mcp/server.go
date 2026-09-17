package mcp

import (
	mcp_go "github.com/mark3labs/mcp-go/mcp"
	mcp_go_server "github.com/mark3labs/mcp-go/server"
)

// Version is the build version reported to MCP clients.  Override at build time
// with: go build -ldflags "-X .../internal/handlers/mcp.Version=1.2.3"
var Version = "dev"

type Server struct {
	*mcp_go_server.MCPServer
	TimeManager TimeManager
	ready       bool
}

// zoneFreeNote is appended to every tool description that accepts a date or
// date/time.  MCP clients surface tool descriptions to the model verbatim, so
// this is the only place a caller learns that times carry no zone.
const zoneFreeNote = "Times are plain local readings with no time zone attached."

// dateTimeParam documents the shape of the shared date/time argument.
const dateTimeDescription = "A date or date/time, e.g. '2026-04-19' or '2026-04-19 14:30:00'. A date without a time means midnight at the start of that day. " + zoneFreeNote

// durationDescription documents the shape of the shared duration argument.
const durationDescription = "A duration such as '30m', '1h30m', '90s', '2d' or '1d3h'. 'd' means whole days of 24 hours. Prefix with '-' to subtract, e.g. '-2h' moves the time two hours earlier."

func NewServer() *Server {
	s := &Server{
		MCPServer: mcp_go_server.NewMCPServer(
			"go-potms",
			Version,
			mcp_go_server.WithToolCapabilities(true),
			mcp_go_server.WithLogging(),
		),
		TimeManager: &LiveTimeManager{},
	}
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"currentDateTime",
			mcp_go.WithDescription("Get the current date and time. Returns a plain local date/time reading such as '2026-04-19 14:30:00'. "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(false),
		),
		s.CurrentDateTime)
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"timeSince",
			mcp_go.WithDescription("Calculate the elapsed time since a given date and time.  The date/time must be in the format YYYY-MM-DD HH:MM:SS or YYYY-MM-DD.  "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(false),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
		),
		s.TimeSince)

	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"timeUntil",
			mcp_go.WithDescription("Calculate the time remaining until a given date and time.  The date/time must be in the format YYYY-MM-DD HH:MM:SS or YYYY-MM-DD.  "+zoneFreeNote+"  A date without a time means midnight at the start of that day."),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(false),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
		),
		s.TimeUntil)
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"timeDifference",
			mcp_go.WithDescription("Calculate the difference between two date and time values.  Each date/time must be in the format YYYY-MM-DD HH:MM:SS or YYYY-MM-DD.  "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithString("firstDateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
			mcp_go.WithString("secondDateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
		),
		s.TimeDifference)

	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"isLeapYear",
			mcp_go.WithDescription("Check if a given year is a leap year.  The year must be provided as a number in the format YYYY."),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithNumber("year", mcp_go.Required()),
		),
		s.IsLeapYear)

	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"dayOfWeek",
			mcp_go.WithDescription("Get the day of the week for a given date. The date must be in the format YYYY-MM-DD or YYYY-MM-DD HH:MM:SS. "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
		),
		s.DayOfWeek)

	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"nextOccurrence",
			mcp_go.WithDescription("Get the next occurrence of a specified day of the week strictly after a given date. The date must be in the format YYYY-MM-DD. The day of the week must be provided as a string (e.g. 'Monday', 'Tuesday', etc.). "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
			mcp_go.WithString("dayOfWeek", mcp_go.Required(), mcp_go.Description("Day of the week in English, case-insensitive (e.g. 'Monday').")),
		),
		s.NextOccurrence)
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"addDuration",
			mcp_go.WithDescription("Add a signed duration to a given date and time.  The date/time must be in the format YYYY-MM-DD HH:MM:SS or YYYY-MM-DD.  "+zoneFreeNote+"  A negative duration subtracts, so '-2h' moves the reading two hours earlier; there is no separate subtract tool."),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(false),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
			mcp_go.WithString("duration", mcp_go.Required(), mcp_go.Description(durationDescription)),
		),
		s.AddDuration)
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"previousOccurrence",
			mcp_go.WithDescription("Get the previous occurrence of a specified day of the week strictly before a given date. The date must be in the format YYYY-MM-DD. The day of the week must be provided as a string (e.g. 'Monday', 'Tuesday', etc.). "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
			mcp_go.WithString("dayOfWeek", mcp_go.Required(), mcp_go.Description("Day of the week in English, case-insensitive (e.g. 'Monday').")),
		),
		s.PreviousOccurrence)
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"isWeekend",
			mcp_go.WithDescription("Check if a given date is a weekend (Saturday or Sunday). The date must be in the format YYYY-MM-DD or YYYY-MM-DD HH:MM:SS. "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
		),
		s.IsWeekend)
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"isWeekday",
			mcp_go.WithDescription("Check if a given date is a weekday (Monday to Friday). The date must be in the format YYYY-MM-DD or YYYY-MM-DD HH:MM:SS. Note this reflects the day of the week only and is not aware of public holidays. "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithString("dateTime", mcp_go.Required(), mcp_go.Description(dateTimeDescription)),
		),
		s.IsWeekday)
	s.MCPServer.AddTool(
		mcp_go.NewTool(
			"daysBetween",
			mcp_go.WithDescription("Calculate the whole number of calendar days between two dates. The dates must be in the format YYYY-MM-DD or YYYY-MM-DD HH:MM:SS. Only the calendar date is compared, so any time component is ignored. The result is negative when the second date is earlier than the first. "+zoneFreeNote),
			mcp_go.WithReadOnlyHintAnnotation(true),
			mcp_go.WithIdempotentHintAnnotation(true),
			mcp_go.WithString("firstDate", mcp_go.Required(), mcp_go.Description("The earlier date, in YYYY-MM-DD format. "+zoneFreeNote)),
			mcp_go.WithString("secondDate", mcp_go.Required(), mcp_go.Description("The later date, in YYYY-MM-DD format. "+zoneFreeNote)),
		),
		s.DaysBetween)

	return s
}

func (s *Server) Ready() bool {
	return s.ready
}

# go-passage-of-time-mcp-server

Passage of Time MCP Server in Golang.  This is heavily inspired by the "[Passage of Time](https://github.com/jlumbroso/passage-of-time-mcp/blob/main/README.md)" MCP server written in Python by @jlumbroso.  This MCP server enables an LLM to perform basic time operations and understand(?) the notion of the passage of time.

## Installation

### From Source

```bash
go install github.com/LeonDevLifeLog/go-passage-of-time-mcp-server/go-potms@latest
```

### From pre-built package

Download one of the provided packages from the releases page.

## Execution

By default, `go-potms` runs as an "stdio" MCP server.  To run as a Streamable HTTP
server instead, provide a port with the `-port` option.

```bash
# stdio mode (default) -- how MCP clients normally launch it
go-potms

# Streamable HTTP mode, served under /mcp
go-potms -port 8080
```

## Times carry no time zone

Time values are plain local readings.  `2026-04-19 14:00:00` denotes the 19th of
April at 14:00 and is returned exactly as written -- there is no `timeZone`
parameter, results carry no UTC-offset suffix, and no daylight-saving rules are
ever applied.  A day is always exactly 24 hours.  Apply any zone interpretation
in the calling application.

## Tools

All 12 tools are read-only calculations.  See [SKILL.md](SKILL.md) for the full
reference, including parameter formats and worked examples.

| Tool | Purpose |
|------|---------|
| `currentDateTime` | Current date and time |
| `timeSince` | Elapsed time since a given date/time |
| `timeUntil` | Remaining time until a given date/time |
| `timeDifference` | Difference between two date/times |
| `addDuration` | Add a signed duration (negative subtracts) |
| `daysBetween` | Whole calendar days between two dates |
| `dayOfWeek` | Weekday for a given date |
| `isWeekday` | Whether a date is Monday-Friday |
| `isWeekend` | Whether a date is Saturday/Sunday |
| `isLeapYear` | Whether a year is a leap year |
| `nextOccurrence` | Next occurrence of a weekday after a date |
| `previousOccurrence` | Previous occurrence of a weekday before a date |

Durations accept a `d` unit for days (`1d`, `1d3h`), where one day is exactly 24
hours, in addition to the standard Go units (`30m`, `1h30m`, `90s`).  Prefix a
duration with `-` to subtract: `addDuration` with `-2h` moves the reading two
hours earlier, so no separate subtract tool is needed.

## Development

```bash
go test ./...            # unit + end-to-end
go test ./internal/...   # unit and in-process protocol tests only
go test ./e2e/ -v        # end-to-end tests against the built binary
```

The end-to-end suite builds the real `go-potms` binary and drives it the same
way an MCP client does — once over the stdio transport, once over Streamable
HTTP.  The behavioural cases are transport-agnostic and run against both, so
they cannot fall behind each other:

```bash
go test ./e2e/ -v -run TestMCPOverEveryTransport        # both transports
go test ./e2e/ -v -run TestMCPOverEveryTransport/http   # HTTP only
go test ./e2e/ -v -run TestHTTP                         # HTTP-specific probes
```

See [TEST_REPORT.md](TEST_REPORT.md) for the current results.

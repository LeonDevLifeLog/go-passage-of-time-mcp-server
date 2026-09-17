// Package e2e drives the real go-potms binary the way an MCP client does: once
// over the stdio transport, once over Streamable HTTP.
//
// The suite in suite_test.go is transport-agnostic and runs against both, so a
// new behavioural case cannot silently cover only one of them.  Anything that
// only makes sense for a single transport lives in that transport's own file
// (stdio_test.go, http_test.go).
//
// Times in this server are plain local readings with no time zone attached, so
// every expectation here is a literal wall-clock value.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Shared MCP message shapes
// ---------------------------------------------------------------------------

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema struct {
		Type       string                    `json:"type"`
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	} `json:"inputSchema"`
	Annotations struct {
		ReadOnlyHint *bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

// mcpTransport is one way of reaching the server.  Both implementations talk to
// a freshly spawned go-potms process; they differ only in how bytes get there.
type mcpTransport interface {
	// Name identifies the transport in test output.
	Name() string

	// ListTools returns the server's tool catalogue.
	ListTools(t *testing.T) []toolDef

	// CallTool invokes a tool and returns its text payload.  The bool reports
	// whether the call failed, whether as a JSON-RPC error or a tool error.
	CallTool(t *testing.T, name string, args map[string]any) (string, bool)
}

// expectedTools is the complete, exact tool set.
var expectedTools = []string{
	"addDuration", "currentDateTime", "dayOfWeek", "daysBetween", "isLeapYear",
	"isWeekday", "isWeekend", "nextOccurrence", "previousOccurrence",
	"timeDifference", "timeSince", "timeUntil",
}

// ---------------------------------------------------------------------------
// Building the binary under test
// ---------------------------------------------------------------------------

// processHandle is a running go-potms process.  cmd.Wait must be called exactly
// once per process, so every path that needs to know it has exited goes through
// the single reaper goroutine started here.
type processHandle struct {
	cmd    *exec.Cmd
	exited chan struct{}
}

// startProcess launches an already-configured command and registers cleanup
// that kills it and waits for the reaper.  The command must not be waited on
// anywhere else.
func startProcess(t *testing.T, cmd *exec.Cmd) *processHandle {
	t.Helper()

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", cmd.Path, err)
	}

	h := &processHandle{cmd: cmd, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(h.exited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		// The reaper's Wait returns as soon as the process is gone; blocking on
		// it here also guarantees no test leaves a stray process behind.
		select {
		case <-h.exited:
		case <-time.After(10 * time.Second):
			t.Errorf("process %s did not exit after being killed", cmd.Path)
		}
	})

	return h
}

// hasExited reports whether the process is already gone, without blocking.
func (h *processHandle) hasExited() bool {
	select {
	case <-h.exited:
		return true
	default:
		return false
	}
}

var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
)

// buildBinary compiles go-potms once per test run and returns the path to it.
// Both transports drive this same binary, so what the tests exercise is what
// ships.
func buildBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "go-potms-e2e")
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(dir, "go-potms")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", bin, "../go-potms")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build failed: %v\n%s", err, out)
			return
		}
		builtPath = bin
	})
	if buildErr != nil {
		t.Fatalf("building go-potms: %v", buildErr)
	}
	return builtPath
}

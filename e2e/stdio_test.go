// Stdio transport: the binary is launched with no arguments and speaks
// newline-delimited JSON-RPC on stdin/stdout, exactly how an MCP client
// normally starts it.
package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type stdioTransport struct {
	cmd    *exec.Cmd
	proc   *processHandle
	stdin  io.WriteCloser
	respCh chan rpcResponse
	nextID int
	mu     sync.Mutex
}

// startStdio launches the binary as a stdio MCP server and completes the
// handshake.
func startStdio(t *testing.T) *stdioTransport {
	t.Helper()
	bin := buildBinary(t)

	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = io.Discard
	proc := startProcess(t, cmd)

	c := &stdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		proc:   proc,
		respCh: make(chan rpcResponse, 64),
		nextID: 1,
	}

	go func() {
		defer close(c.respCh)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var r rpcResponse
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				continue
			}
			c.respCh <- r
		}
	}()

	t.Cleanup(func() {
		// Closing stdin is how an MCP client asks the server to stop.  The
		// reaper's Wait is already running, so just wait for it to observe the
		// exit -- killing is the reaper's fallback if the server hangs.
		_ = stdin.Close()
		select {
		case <-proc.exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-proc.exited
		}
	})

	c.initialize(t)
	return c
}

func (c *stdioTransport) Name() string { return "stdio" }

func (c *stdioTransport) send(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(c.stdin, "%s\n", b); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

// await returns the response with the given id, ignoring notifications.
func (c *stdioTransport) await(t *testing.T, id int, timeout time.Duration) rpcResponse {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case r, ok := <-c.respCh:
			if !ok {
				t.Fatalf("server closed stdout while waiting for id=%d", id)
			}
			if r.ID == id {
				return r
			}
		case <-deadline:
			t.Fatalf("timed out waiting for response id=%d", id)
		}
	}
}

func (c *stdioTransport) initialize(t *testing.T) {
	t.Helper()
	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "e2e", "version": "1"},
		},
	})
	r := c.await(t, 1, 10*time.Second)
	if r.Error != nil {
		t.Fatalf("initialize failed: %s", r.Error.Message)
	}
	c.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	c.nextID = 2
}

func (c *stdioTransport) nextRequestID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// CallTool invokes a tool and returns its text payload.
func (c *stdioTransport) CallTool(t *testing.T, name string, args map[string]any) (string, bool) {
	t.Helper()
	id := c.nextRequestID()

	c.send(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	r := c.await(t, id, 10*time.Second)
	if r.Error != nil {
		return r.Error.Message, true
	}
	var tr toolResult
	if err := json.Unmarshal(r.Result, &tr); err != nil {
		t.Fatalf("decoding tool result for %s: %v", name, err)
	}
	if len(tr.Content) == 0 {
		t.Fatalf("tool %s returned no content", name)
	}
	return tr.Content[0].Text, tr.IsError
}

// ListTools returns the tool catalogue.
func (c *stdioTransport) ListTools(t *testing.T) []toolDef {
	t.Helper()
	id := c.nextRequestID()

	c.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/list"})
	r := c.await(t, id, 10*time.Second)
	if r.Error != nil {
		t.Fatalf("tools/list failed: %s", r.Error.Message)
	}
	var out struct {
		Tools []toolDef `json:"tools"`
	}
	if err := json.Unmarshal(r.Result, &out); err != nil {
		t.Fatalf("decoding tools/list: %v", err)
	}
	return out.Tools
}

// ---------------------------------------------------------------------------
// Stdio-specific behaviour
// ---------------------------------------------------------------------------

// TestStdioWireFormat pins the framing a stdio client depends on: one complete
// JSON object per line, no banner text, nothing else on stdout.  A single stray
// log line on stdout desynchronises the stream, so this is worth asserting
// against the raw bytes rather than the parsed messages the suite already
// consumes.
func TestStdioWireFormat(t *testing.T) {
	bin := buildBinary(t)

	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = io.Discard
	startProcess(t, cmd)

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dayOfWeek","arguments":{"dateTime":"2026-04-19"}}}`,
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	// Feed one request, read one line back.  Notifications produce no reply,
	// so they are written without a matching read.
	wantResponses := map[int]bool{1: false, 2: false, 3: false}
	lines := 0

	for _, req := range requests {
		if _, err := fmt.Fprintln(stdin, req); err != nil {
			t.Fatalf("write request: %v", err)
		}

		var id int
		if !strings.Contains(req, "notifications/initialized") {
			// wait for the next line and check it is complete, valid JSON
			if !sc.Scan() {
				t.Fatalf("server stopped responding after %d lines: %v", lines, sc.Err())
			}
			line := sc.Text()
			lines++
			if strings.TrimSpace(line) == "" {
				t.Fatalf("line %d was blank; every line must carry exactly one JSON object", lines)
			}
			var r rpcResponse
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Fatalf("line %d is not a standalone JSON object: %v\n%q", lines, err, line)
			}
			if r.JSONRPC != "2.0" {
				t.Errorf("line %d has jsonrpc = %q, want \"2.0\"", lines, r.JSONRPC)
			}
			id = r.ID
			if _, expected := wantResponses[id]; !expected {
				t.Errorf("line %d carried unexpected id %d", lines, id)
			}
			wantResponses[id] = true
		}
	}

	for id, seen := range wantResponses {
		if !seen {
			t.Errorf("no response was emitted for request id=%d", id)
		}
	}
}

// TestStdioExitsWhenStdinCloses checks the server shuts down cleanly when the
// client goes away, rather than lingering as an orphan process.
func TestStdioExitsWhenStdinCloses(t *testing.T) {
	bin := buildBinary(t)

	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	proc := startProcess(t, cmd)

	// Closing stdin is how MCP clients signal shutdown.
	if err := stdin.Close(); err != nil {
		t.Fatalf("closing stdin: %v", err)
	}

	select {
	case <-proc.exited:
		// Any exit status is fine; the point is that it exited.
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("server did not exit after stdin was closed")
	}
}

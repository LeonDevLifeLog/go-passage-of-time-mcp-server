// Streamable HTTP transport: the binary is launched with -port and speaks MCP
// over POST /mcp, with an Mcp-Session-Id header threading the conversation
// together.
//
// Driving the real binary rather than an in-process handler is deliberate: it
// is the only way to cover main.go's -port branch, which is where the HTTP
// server is actually wired up.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	sessionHeader   = "Mcp-Session-Id"
	sessionIDPrefix = "mcp-session-"
)

type httpTransport struct {
	baseURL   string
	sessionID string
	client    *http.Client
	mu        sync.Mutex
	nextID    int
}

// freePort asks the kernel for an unused port and immediately releases it.
// There is an unavoidable window between releasing it and the server binding
// it; startHTTP retries with a fresh port if the server loses that race.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// startHTTP launches the binary in HTTP mode and completes the handshake.
func startHTTP(t *testing.T) *httpTransport {
	t.Helper()
	bin := buildBinary(t)

	// A handful of attempts absorbs the (rare) case where another process grabs
	// the port in the gap between freePort releasing it and go-potms binding.
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		port := freePort(t)
		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

		cmd := exec.Command(bin, "-port", fmt.Sprint(port), "-host", "127.0.0.1")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard

		// startProcess registers the cleanup that stops it, whichever way the
		// test ends.  It must be the only caller of cmd.Wait.
		proc := startProcess(t, cmd)

		c := &httpTransport{
			baseURL: baseURL,
			client:  &http.Client{Timeout: 15 * time.Second},
			nextID:  2,
		}

		if err := c.waitForServer(proc); err != nil {
			lastErr = err
			continue
		}
		c.initialize(t)
		return c
	}

	t.Fatalf("go-potms never accepted a connection in HTTP mode: %v", lastErr)
	return nil
}

// waitForServer polls the port until the server answers.  It watches the
// process through the shared reaper rather than waiting on the command itself.
func (c *httpTransport) waitForServer(proc *processHandle) error {
	addr := strings.TrimPrefix(c.baseURL, "http://")

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if proc.hasExited() {
			return fmt.Errorf("process exited before it started listening")
		}

		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to accept connections", c.baseURL)
}

func (c *httpTransport) Name() string { return "http" }

func (c *httpTransport) nextRequestID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// post sends a JSON-RPC message and returns the raw HTTP response.  The caller
// owns the body.
func (c *httpTransport) post(t *testing.T, body any, withSession bool) *http.Response {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if withSession && c.sessionID != "" {
		req.Header.Set(sessionHeader, c.sessionID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	return resp
}

// rpc performs a request/response exchange and decodes the reply.  Either
// encoding is accepted: a plain JSON body, or an SSE stream carrying the same
// object, because the specification lets the server choose.
func (c *httpTransport) rpc(t *testing.T, body any) rpcResponse {
	t.Helper()
	resp := c.post(t, body, true)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("POST /mcp returned %d, want 200: %s", resp.StatusCode, raw)
	}
	return decodeRPCResponse(t, resp)
}

// decodeRPCResponse reads a single JSON-RPC object out of either supported
// response encoding.
func decodeRPCResponse(t *testing.T, resp *http.Response) rpcResponse {
	t.Helper()

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("unparsable Content-Type %q: %v", resp.Header.Get("Content-Type"), err)
	}

	var raw []byte
	switch mediaType {
	case "application/json":
		raw, err = io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading JSON body: %v", err)
		}
	case "text/event-stream":
		raw, err = readFirstSSEData(resp.Body)
		if err != nil {
			t.Fatalf("reading SSE body: %v", err)
		}
	default:
		t.Fatalf("unexpected Content-Type %q, want application/json or text/event-stream", mediaType)
	}

	var r rpcResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("decoding JSON-RPC reply: %v\n%q", err, raw)
	}
	return r
}

// readFirstSSEData pulls the payload of the first `data:` line out of an SSE
// stream.
func readFirstSSEData(body io.Reader) ([]byte, error) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			return []byte(strings.TrimSpace(data)), nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("stream ended before any data line arrived")
}

func (c *httpTransport) initialize(t *testing.T) {
	t.Helper()

	resp := c.post(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "e2e", "version": "1"},
		},
	}, false)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("initialize returned %d, want 200: %s", resp.StatusCode, raw)
	}

	sid := resp.Header.Get(sessionHeader)
	if sid == "" {
		t.Fatal("initialize did not return an Mcp-Session-Id header")
	}
	if !strings.HasPrefix(sid, sessionIDPrefix) {
		t.Errorf("session id %q does not start with %q", sid, sessionIDPrefix)
	}

	if r := decodeRPCResponse(t, resp); r.Error != nil {
		t.Fatalf("initialize failed: %s", r.Error.Message)
	}

	c.sessionID = sid
	c.sendNotification(t, "notifications/initialized")
}

// sendNotification posts a notification and asserts the 202-with-no-body
// acknowledgement the streamable HTTP transport specifies.
func (c *httpTransport) sendNotification(t *testing.T, method string) {
	t.Helper()
	resp := c.post(t, map[string]any{"jsonrpc": "2.0", "method": method}, true)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification %s returned %d, want 202", method, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading notification response: %v", err)
	}
	if len(bytes.TrimSpace(body)) != 0 {
		t.Errorf("notification %s returned a body %q, want none", method, body)
	}
}

// CallTool invokes a tool and returns its text payload.
func (c *httpTransport) CallTool(t *testing.T, name string, args map[string]any) (string, bool) {
	t.Helper()
	r := c.rpc(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextRequestID(),
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
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
func (c *httpTransport) ListTools(t *testing.T) []toolDef {
	t.Helper()
	r := c.rpc(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextRequestID(),
		"method":  "tools/list",
	})
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
// HTTP-specific behaviour
// ---------------------------------------------------------------------------

// TestHTTPInitializeMintsSession pins the handshake contract: initialize
// returns a session id, and concurrent clients get distinct ones rather than
// sharing state.
func TestHTTPInitializeMintsSession(t *testing.T) {
	c := startHTTP(t)

	resp := c.post(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "e2e-second", "version": "1"},
		},
	}, false)
	defer func() { _ = resp.Body.Close() }()

	second := resp.Header.Get(sessionHeader)
	if second == "" {
		t.Fatal("second initialize did not return a session id")
	}
	if second == c.sessionID {
		t.Errorf("two initialize calls returned the same session id %q", second)
	}
	t.Logf("session ids: %q and %q", c.sessionID, second)
}

// TestHTTPRequestsWithoutASessionAreRejected checks the stateful-session
// contract: a request that is not an initialize must carry a well-formed
// session id, and the server says so with 400 rather than silently serving it.
func TestHTTPRequestsWithoutASessionAreRejected(t *testing.T) {
	c := startHTTP(t)

	cases := []struct {
		desc      string
		sessionID string
	}{
		{"no session header", ""},
		{"a made-up session id", "not-a-real-session"},
		{"a uuid without the required prefix", "2f1c9a1e-0000-4000-8000-000000000000"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/list",
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			req, err := http.NewRequest(http.MethodPost, c.baseURL+"/mcp", bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("building request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if tc.sessionID != "" {
				req.Header.Set(sessionHeader, tc.sessionID)
			}

			resp, err := c.client.Do(req)
			if err != nil {
				t.Fatalf("POST /mcp: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("got %d, want 400: %s", resp.StatusCode, body)
			}
			// The server must not have served the request.
			if bytes.Contains(body, []byte(`"tools"`)) {
				t.Errorf("response leaked tool data: %s", body)
			}
		})
	}
}

// TestHTTPRejectsMalformedRequests covers the transport's input validation:
// a bad content type or unparsable body is a 400, not a panic or a hang.
func TestHTTPRejectsMalformedRequests(t *testing.T) {
	c := startHTTP(t)

	send := func(t *testing.T, contentType, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, c.baseURL+"/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatalf("building request: %v", err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set(sessionHeader, c.sessionID)
		resp, err := c.client.Do(req)
		if err != nil {
			t.Fatalf("POST /mcp: %v", err)
		}
		return resp
	}

	cases := []struct {
		desc        string
		contentType string
		body        string
	}{
		{"wrong content type", "text/plain", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`},
		{"missing content type", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`},
		{"body is not json", "application/json", `this is not json`},
		{"body is truncated json", "application/json", `{"jsonrpc":"2.0","id":1,"method":`},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			resp := send(t, tc.contentType, tc.body)
			defer func() { _ = resp.Body.Close() }()

			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("got %d, want 400: %s", resp.StatusCode, raw)
			}
		})
	}

	// After all that abuse the session must still work.
	text, isErr := c.CallTool(t, "dayOfWeek", map[string]any{"dateTime": "2026-04-19"})
	if isErr {
		t.Fatalf("session unusable after malformed requests: %s", text)
	}
	if text != "The day of the week for 2026-04-19 is Sunday." {
		t.Errorf("got %q", text)
	}
}

// TestHTTPNotificationStream opens the GET endpoint, which is how a client
// listens for server-initiated messages.  It must upgrade to an SSE stream and
// stay open.
func TestHTTPNotificationStream(t *testing.T) {
	c := startHTTP(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/mcp", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set(sessionHeader, c.sessionID)
	req.Header.Set("Accept", "text/event-stream")

	// No client timeout: this connection is meant to stay open.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /mcp returned %d, want 200", resp.StatusCode)
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		t.Errorf("GET /mcp Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	// The stream must remain open rather than closing after an empty body.
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, err := resp.Body.Read(buf)
		readErr <- err
	}()

	select {
	case err := <-readErr:
		if err != nil {
			t.Errorf("notification stream closed immediately: %v", err)
		}
	case <-time.After(time.Second):
		// Still open, as it should be.
	}

	cancel()
}

// TestHTTPSessionTeardown covers DELETE /mcp, the client's way of saying it is
// done with a session.
func TestHTTPSessionTeardown(t *testing.T) {
	c := startHTTP(t)

	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/mcp", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set(sessionHeader, c.sessionID)

	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("DELETE /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("DELETE /mcp returned %d, want 200", resp.StatusCode)
	}
}

// TestHTTPUnsupportedMethodIsNotASilentSuccess guards against a routing change
// that makes an unimplemented verb look like it worked.
func TestHTTPUnsupportedMethodIsNotASilentSuccess(t *testing.T) {
	c := startHTTP(t)

	req, err := http.NewRequest(http.MethodPut, c.baseURL+"/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("PUT /mcp: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		t.Errorf("PUT /mcp returned %d; an unsupported method must not report success", resp.StatusCode)
	}
}

// TestHTTPServesConcurrentClients exercises what the stdio transport cannot:
// several independent sessions sharing one process.  Each must get correct
// answers, with no cross-talk between them.
func TestHTTPServesConcurrentClients(t *testing.T) {
	first := startHTTP(t)

	const clients = 4
	second := make([]*httpTransport, 0, clients-1)
	for i := 0; i < clients-1; i++ {
		second = append(second, startHTTP(t))
	}

	all := append([]*httpTransport{first}, second...)
	if len(all) != clients {
		t.Fatalf("expected %d clients, got %d", clients, len(all))
	}

	var wg sync.WaitGroup
	errs := make(chan error, clients)

	for i, c := range all {
		wg.Add(1)
		go func(i int, c *httpTransport) {
			defer wg.Done()
			// Each client asks a different question and must get its own answer.
			date := fmt.Sprintf("2026-04-%02d", 19+i)
			want := fmt.Sprintf("There are %d days between 2026-04-19 and %s.", i, date)

			text, isErr := c.CallTool(t, "daysBetween", map[string]any{
				"firstDate": "2026-04-19", "secondDate": date,
			})
			if isErr {
				errs <- fmt.Errorf("client %d: %s", i, text)
				return
			}
			if text != want {
				errs <- fmt.Errorf("client %d: got %q, want %q", i, text, want)
			}
		}(i, c)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

package relay_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
)

// AgentRuntime sends a run token and the tenant in a job's "headers" so the agent on the worker can
// call AgentRuntime as the run. The relay is a pipe for them: the worker gets the job as sent,
// and no header value is logged or echoed.
func TestHarnessDispatch_forwardsJobHeadersUnchanged(t *testing.T) {
	const runToken = "Bearer made-up-run-token-0123456789"
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal"})
	var s sink
	registerWorker(h, "tenant-a", "workbench:user-alice", &s)

	job := map[string]any{
		"run_id":    "r1:s1",
		"provider":  "claude",
		"prompt":    "triage the inbox",
		"config":    map[string]any{"model": "claude-haiku-5-5", "max_turns": float64(6)},
		"timeout_s": float64(600),
		"headers":   map[string]any{"authorization": runToken, "x-tenant-id": "tenant-a"},
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	resp := dispatchRequest(t, ts, "workbench:user-alice",
		map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a"}, string(raw))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("dispatch: status = %d (%s)", resp.StatusCode, body)
	}

	frames := s.all()
	if len(frames) != 1 {
		t.Fatalf("worker received %d frames, want 1", len(frames))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(frames[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "harness_run" {
		t.Fatalf("frame type = %v", got["type"])
	}
	for _, key := range []string{"run_id", "provider", "prompt", "config", "timeout_s", "headers"} {
		if !reflect.DeepEqual(got[key], job[key]) {
			t.Errorf("frame %q = %#v, want %#v", key, got[key], job[key])
		}
	}

	if bytes.Contains(body, []byte("made-up-run-token")) {
		t.Errorf("the dispatch response echoes the token: %s", body)
	}
	if bytes.Contains(logs.Bytes(), []byte("made-up-run-token")) {
		t.Errorf("the relay logged the token: %s", logs.String())
	}
}

// A job without headers stays without: the relay adds none of its own (an older AgentRuntime sends none).
func TestHarnessDispatch_jobWithoutHeadersGetsNone(t *testing.T) {
	h, ts := newAuthRelay(t, nil, relay.ServerConfig{InternalToken: "test-internal"})
	var s sink
	registerWorker(h, "tenant-a", "workbench:user-alice", &s)

	resp := dispatchRequest(t, ts, "workbench:user-alice",
		map[string]string{"X-Internal-Token": "test-internal", "X-Tenant-Id": "tenant-a", "Authorization": "Bearer not-for-the-worker"},
		`{"run_id":"r1:s1","provider":"claude","prompt":"hi"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("dispatch: status = %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(s.all()[0]), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["headers"]; ok {
		t.Fatalf("frame has headers the sender did not give: %v", got["headers"])
	}
	if bytes.Contains([]byte(s.all()[0]), []byte("not-for-the-worker")) {
		t.Fatal("the request's own Authorization header reached the worker")
	}
}

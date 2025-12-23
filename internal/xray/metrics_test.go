package xray

import "testing"

func TestParseDebugVars_Observatory(t *testing.T) {
	data := []byte(`{
  "observatory": {
    "n-a": {"alive": true, "delay": 123, "outbound_tag": "n-a", "last_seen_time": 1700000000, "last_try_time": 1700000001},
    "n-b": {"alive": false, "delay": 0, "outbound_tag": "n-b", "last_seen_time": 0, "last_try_time": 1700000002}
  }
}`)

	h, err := ParseDebugVars(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(h))
	}
	if !h["n-a"].Alive || h["n-a"].Delay.Milliseconds() != 123 {
		t.Fatalf("bad parse for n-a: %+v", h["n-a"])
	}
	if h["n-b"].Alive {
		t.Fatalf("expected n-b not alive")
	}
}


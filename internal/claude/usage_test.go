package claude

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// usageFixture copies testdata/usage.jsonl into a session with one subagent.
func usageFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	b, err := os.ReadFile("testdata/usage.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	sd := filepath.Join(dir, "s1", "subagents")
	_ = os.MkdirAll(sd, 0o755)
	sub := `{"type":"assistant","isSidechain":true,"message":{"model":"claude-haiku-5-5","id":"msg_s","usage":{"input_tokens":2000,"cache_creation_input_tokens":0,"cache_read_input_tokens":50000,"output_tokens":300}}}` + "\n"
	if err := os.WriteFile(filepath.Join(sd, "agent-a1.jsonl"), []byte(sub), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUsageOf(t *testing.T) {
	path := usageFixture(t)
	u := UsageOf(path)
	// msg_1 is logged twice (one line per content block): counted once, last usage wins;
	// its 20k cache write has the 1-hour TTL, msg_cut was interrupted before billing
	if got, want := u.Main["claude-opus-5-5"], (Tokens{In: 15, Out: 1000, CacheW: 23000, CacheR: 20000, CacheW1h: 20000}); got != want {
		t.Errorf("opus tokens = %+v, want %+v", got, want)
	}
	if got, want := u.Main["claude-haiku-5-5"], (Tokens{In: 1000, Out: 100}); got != want {
		t.Errorf("sidechain tokens = %+v, want %+v", got, want)
	}
	if _, ok := u.Main["<synthetic>"]; ok {
		t.Error("synthetic entries are not spend")
	}
	if u.Ctx != 23005 || u.Peak != 23005 || u.Model != "claude-opus-5-5" {
		t.Errorf("ctx=%d peak=%d model=%q", u.Ctx, u.Peak, u.Model)
	}
	if u.Window() != 200_000 || u.Pct() != 11 {
		t.Errorf("window=%d pct=%d", u.Window(), u.Pct())
	}
	if got := u.Subs["claude-haiku-5-5"]; got != (Tokens{In: 2000, Out: 300, CacheR: 50000}) {
		t.Errorf("subagent tokens = %+v", got)
	}
	if c := u.Cost(); !near(c, (15*4+1000*20+3000*5+20000*2*4+20000*0.2+1000*0.1+100*0.5)/1e6) {
		t.Errorf("cost = %v", c)
	}
	if c := u.SubCost(); !near(c, (2000*0.1+50000*0.01+300*0.5)/1e6) {
		t.Errorf("subagent cost = %v", c)
	}
	if got := u.Total().Sum(); got != 15+1000+23000+20000+1000+100+2000+300+50000 {
		t.Errorf("total = %d", got)
	}
}

// Appended bytes are read incrementally; a line still being written waits for its newline.
func TestUsageIncremental(t *testing.T) {
	path := usageFixture(t)
	UsageOf(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := `{"type":"assistant","message":{"model":"claude-opus-5-5[1m]","id":"msg_3","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":250000,"output_tokens":10}}}`
	_, _ = f.WriteString(line[:40])
	if u := UsageOf(path); u.Ctx != 23005 {
		t.Fatalf("partial line counted: ctx=%d", u.Ctx)
	}
	_, _ = f.WriteString(line[40:] + "\n")
	u := UsageOf(path)
	if u.Ctx != 250001 || u.Window() != 1_000_000 || u.Pct() != 25 {
		t.Errorf("after append: ctx=%d window=%d pct=%d", u.Ctx, u.Window(), u.Pct())
	}
	if got := u.Main["claude-opus-5-5[1m]"].CacheR; got != 250000 {
		t.Errorf("appended tokens = %d", got)
	}
}

func TestPriceOf(t *testing.T) {
	defer SetPrices(nil)
	for _, c := range []struct {
		model string
		in    float64
	}{
		{"claude-opus-5-5", 4}, {"claude-opus-5", 5}, {"claude-opus-4-8", 5}, {"claude-opus-4-1-20250805", 15},
		{"claude-sonnet-5-5", 2}, {"claude-sonnet-4-5-20250929", 3}, {"claude-haiku-4-5", 1}, {"claude-haiku-5-5", 0.1},
		{"claude-fable-5-1", 10}, {"claude-mythos-5-1", 10}, {"claude-sonnet-9-9", 2}, {"some-new-model", 4},
		{"us.anthropic.claude-opus-4-8", 5}, {"claude-opus-5-5[1m]", 4},
	} {
		if p := PriceOf(c.model); p.Input != c.in {
			t.Errorf("%s: input %v, want %v", c.model, p.Input, c.in)
		}
	}
	SetPrices(map[string]Price{"Opus": {Input: 1}})
	if p := PriceOf("claude-opus-4-8"); p.Input != 1 || p.Output != 25 {
		t.Errorf("override: %+v", p)
	}
	if p := PriceOf("claude-sonnet-5-5"); p.Input != 2 {
		t.Errorf("override leaked to sonnet: %+v", p)
	}
}

// The cut-out parse reads the same as a full decode, whatever the key order.
func TestParseUsageRec(t *testing.T) {
	for _, l := range []string{
		`{"parentUuid":"p","isSidechain":true,"message":{"model":"claude-opus-5-5","id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"\"usage\":{\"input_tokens\":9} {[}"}],"usage":{"input_tokens":3,"output_tokens":4,"iterations":[{"input_tokens":1}],"cache_creation":{"ephemeral_1h_input_tokens":2}}},"type":"assistant","uuid":"u"}`,
		`{"message":{"content":[{"type":"text","text":"hi"}],"id":"msg_2","model":"claude-haiku-5-5","role":"assistant","usage":{"input_tokens":5,"output_tokens":6}},"type":"assistant"}`,
		`{"type":"user","message":{"role":"user","content":"x"},"toolUseResult":{"usage":{"input_tokens":7}}}`,
	} {
		var want usageRec
		_ = json.Unmarshal([]byte(l), &want)
		got, ok := parseUsageRec([]byte(l))
		if !ok || !reflect.DeepEqual(got.Message, want.Message) && want.Type == "assistant" || got.IsSidechain != want.IsSidechain {
			t.Errorf("%s:\n got  %+v\n want %+v", l, got.Message, want.Message)
		}
	}
}

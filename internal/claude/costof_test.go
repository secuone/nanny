package claude

import (
	"math"
	"testing"
)

// CostOf prices one model's tokens like Usage.Cost does.
func TestCostOf(t *testing.T) {
	tk := Tokens{In: 1_000_000, Out: 1_000_000, CacheW: 1_000_000, CacheR: 1_000_000, CacheW1h: 500_000}
	u := Usage{Main: map[string]Tokens{"claude-opus-5-5": tk}}
	if got, want := CostOf("claude-opus-5-5", tk), u.Cost(); got <= 0 || math.Abs(got-want) > 1e-9 {
		t.Errorf("CostOf = %v, Usage.Cost = %v", got, want)
	}
	if CostOf("claude-opus-5-5", Tokens{}) != 0 {
		t.Error("no tokens, no cost")
	}
}

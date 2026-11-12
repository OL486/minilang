package numfmt

import (
	"math"
	"testing"

	"github.com/OL486/minilang/internal/semantics"
)

// Number 只是 semantics.Number 的薄转发，这里只验证转发关系，
// 详细规则见 internal/semantics 的测试。
func TestNumberDelegatesToSemantics(t *testing.T) {
	for _, f := range []float64{0, 1, -2.5, 0.1, 1000000, math.NaN(), math.Inf(1)} {
		if got, want := Number(f), semantics.Number(f); got != want {
			t.Errorf("Number(%v) = %q, semantics.Number = %q", f, got, want)
		}
	}
	if got := Number(1); got != "1" {
		t.Errorf("Number(1) = %q, want %q", got, "1")
	}
}

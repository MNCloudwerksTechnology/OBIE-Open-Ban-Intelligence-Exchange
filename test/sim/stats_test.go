package sim

import (
	"math"
	"testing"
)

func TestSummarize(t *testing.T) {
	s := summarize([]float64{1, 2, 3, 4, 5})
	// sd = sqrt(2.5); half-width = t(4) · sd / sqrt(5) = 2.776 · 0.7071.
	half := 2.776 * math.Sqrt(2.5) / math.Sqrt(5)
	if s.n != 5 || s.mean != 3 || math.Abs(s.sd-math.Sqrt(2.5)) > 1e-12 ||
		math.Abs(s.low-(3-half)) > 1e-9 || math.Abs(s.high-(3+half)) > 1e-9 || s.minV != 1 || s.maxV != 5 {
		t.Errorf("summarize(1..5) = %+v, want mean 3, sd %.4f, CI 3 ± %.4f, min 1, max 5", s, math.Sqrt(2.5), half)
	}
}

func TestSummarizeConstant(t *testing.T) {
	s := summarize([]float64{1, 1, 1})
	if s.mean != 1 || s.low != 1 || s.high != 1 {
		t.Errorf("summarize(1, 1, 1) = %+v, want mean and CI 1", s)
	}
}

func TestSummarizeFewValues(t *testing.T) {
	if s := summarize(nil); s.n != 0 || !math.IsNaN(s.mean) {
		t.Errorf("summarize(nil) = %+v, want n 0 and a NaN mean", s)
	}
	if s := summarize([]float64{0.5}); s.n != 1 || s.mean != 0.5 || !math.IsNaN(s.low) || s.minV != 0.5 {
		t.Errorf("summarize(0.5) = %+v, want mean 0.5 and no interval", s)
	}
}

func TestTCritical(t *testing.T) {
	for _, tc := range []struct {
		df   int
		want float64
	}{{1, 12.706}, {19, 2.093}, {30, 2.042}, {1000, 1.962}} {
		if got := tCritical(tc.df); math.Abs(got-tc.want) > 0.001 {
			t.Errorf("tCritical(%d) = %.4f, want %.3f", tc.df, got, tc.want)
		}
	}
	if got := tCritical(0); !math.IsNaN(got) {
		t.Errorf("tCritical(0) = %v, want NaN", got)
	}
}

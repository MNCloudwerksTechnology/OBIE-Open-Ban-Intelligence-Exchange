package sim

import "math"

// summary is a metric over the seeds of a scenario and variant: the mean
// with its 95 % confidence interval from Student's t.
type summary struct {
	n          int
	mean, sd   float64
	low, high  float64
	minV, maxV float64
}

// summarize returns the summary of the values.
func summarize(values []float64) summary {
	s := summary{n: len(values), minV: math.Inf(1), maxV: math.Inf(-1)}
	if s.n == 0 {
		return summary{mean: math.NaN(), sd: math.NaN(), low: math.NaN(), high: math.NaN(), minV: math.NaN(), maxV: math.NaN()}
	}
	for _, v := range values {
		s.mean += v
		s.minV, s.maxV = math.Min(s.minV, v), math.Max(s.maxV, v)
	}
	s.mean /= float64(s.n)
	if s.n == 1 {
		s.sd, s.low, s.high = math.NaN(), math.NaN(), math.NaN()
		return s
	}
	var ss float64
	for _, v := range values {
		ss += (v - s.mean) * (v - s.mean)
	}
	s.sd = math.Sqrt(ss / float64(s.n-1))
	half := tCritical(s.n-1) * s.sd / math.Sqrt(float64(s.n))
	s.low, s.high = s.mean-half, s.mean+half
	return s
}

// tTable holds the two-sided 95 % critical values of Student's t for 1 to
// 30 degrees of freedom.
var tTable = [...]float64{
	12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228,
	2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086,
	2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042,
}

// tCritical returns the two-sided 95 % critical value of Student's t with
// df degrees of freedom; beyond the table, the normal approximation with
// the first correction term.
func tCritical(df int) float64 {
	if df < 1 {
		return math.NaN()
	}
	if df <= len(tTable) {
		return tTable[df-1]
	}
	const z = 1.959964
	return z + (z*z*z+z)/(4*float64(df))
}

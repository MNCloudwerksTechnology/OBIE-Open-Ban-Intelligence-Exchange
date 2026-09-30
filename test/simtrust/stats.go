package simtrust

import (
	"math"
	"sort"
)

// Estimate is the mean of a metric over the seeds for which it is
// defined, with its 95 % Student-t confidence interval.
type Estimate struct {
	Mean, Low, High float64
	// N is the number of seeds with a value; the interval needs two.
	N int
}

// HalfWidth returns half the width of the interval.
func (e Estimate) HalfWidth() float64 {
	return (e.High - e.Low) / 2
}

// estimate returns the estimate of xs, skipping NaN values.
func estimate(xs []float64) Estimate {
	var sum float64
	n := 0
	for _, x := range xs {
		if !math.IsNaN(x) {
			sum += x
			n++
		}
	}
	e := Estimate{Mean: math.NaN(), Low: math.NaN(), High: math.NaN(), N: n}
	if n == 0 {
		return e
	}
	e.Mean = sum / float64(n)
	if n < 2 {
		return e
	}
	var ss float64
	for _, x := range xs {
		if !math.IsNaN(x) {
			ss += (x - e.Mean) * (x - e.Mean)
		}
	}
	hw := tQuantile975(n-1) * math.Sqrt(ss/float64(n-1)/float64(n))
	e.Low, e.High = e.Mean-hw, e.Mean+hw
	return e
}

// tTable holds the 0.975 quantiles of Student's t distribution by
// degrees of freedom.
var tTable = []struct {
	df int
	t  float64
}{
	{1, 12.706}, {2, 4.303}, {3, 3.182}, {4, 2.776}, {5, 2.571}, {6, 2.447}, {7, 2.365}, {8, 2.306},
	{9, 2.262}, {10, 2.228}, {11, 2.201}, {12, 2.179}, {13, 2.160}, {14, 2.145}, {15, 2.131}, {16, 2.120},
	{17, 2.110}, {18, 2.101}, {19, 2.093}, {20, 2.086}, {21, 2.080}, {22, 2.074}, {23, 2.069}, {24, 2.064},
	{25, 2.060}, {26, 2.056}, {27, 2.052}, {28, 2.048}, {29, 2.045}, {30, 2.042}, {40, 2.021}, {60, 2.000},
	{120, 1.980},
}

// tQuantile975 returns the 0.975 quantile of Student's t distribution
// with df degrees of freedom, interpolated in 1/df between the table's
// entries, and 1.960 beyond them.
func tQuantile975(df int) float64 {
	i := sort.Search(len(tTable), func(i int) bool { return tTable[i].df >= df })
	switch {
	case i == len(tTable):
		lo := tTable[len(tTable)-1]
		return 1.960 + (lo.t-1.960)*float64(lo.df)/float64(df)
	case tTable[i].df == df || i == 0:
		return tTable[i].t
	}
	lo, hi := tTable[i-1], tTable[i]
	x := (1/float64(lo.df) - 1/float64(df)) / (1/float64(lo.df) - 1/float64(hi.df))
	return lo.t + x*(hi.t-lo.t)
}

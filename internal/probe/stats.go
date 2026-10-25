package probe

import (
	"math"
	"slices"
	"time"
)

// Stats summarises a series of round trips, in ms.
type Stats struct {
	Count  int       `json:"count"`
	Errors int       `json:"errors"` // attempts that got no answer, not in Count
	Since  time.Time `json:"since"`
	Min    float64   `json:"min"`
	P50    float64   `json:"p50"`
	P95    float64   `json:"p95"`
	P99    float64   `json:"p99"`
	Max    float64   `json:"max"`
}

// Summarize computes the percentiles of values by nearest rank. Nil when there is
// nothing to summarise.
func Summarize(values []float64, errors int, since time.Time) *Stats {
	if len(values) == 0 && errors == 0 {
		return nil
	}
	s := &Stats{Count: len(values), Errors: errors, Since: since}
	if len(values) == 0 {
		return s
	}
	v := slices.Clone(values)
	slices.Sort(v)
	rank := func(p float64) float64 {
		i := int(math.Ceil(p/100*float64(len(v)))) - 1
		return v[max(0, min(i, len(v)-1))]
	}
	s.Min, s.P50, s.P95, s.P99, s.Max = v[0], rank(50), rank(95), rank(99), v[len(v)-1]
	return s
}

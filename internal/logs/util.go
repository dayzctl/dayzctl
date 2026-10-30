package logs

import (
	"math"
	"strconv"
)

func hypot(x, y float64) float64 { return math.Hypot(x, y) }

func absFloat(v float64) float64 { return math.Abs(v) }

// formatSeconds renders a duration rounded to whole seconds.
func formatSeconds(v float64) string {
	return strconv.Itoa(int(math.Round(v)))
}

// formatMeters renders a distance rounded to whole metres.
func formatMeters(v float64) string {
	return strconv.Itoa(int(math.Round(v)))
}

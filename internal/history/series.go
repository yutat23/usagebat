package history

import (
	"sort"
	"time"

	"github.com/yutat23/usagebat/internal/model"
)

// Point is one plotted value.
type Point struct {
	At    time.Time
	Value float64
}

// Series identifies one line a chart can draw.
type Series struct {
	Source string
	Window model.Window
}

// Available lists the series present in the samples, in a stable order, so the
// UI can offer exactly what there is data for.
func Available(samples []Sample) []Series {
	seen := map[Series]bool{}
	for _, sample := range samples {
		for _, entry := range sample.Entries {
			seen[Series{Source: entry.Source, Window: entry.Window}] = true
		}
	}
	out := make([]Series, 0, len(seen))
	for series := range seen {
		out = append(out, series)
	}
	order := map[model.Window]int{}
	for i, w := range model.AllWindows {
		order[w] = i
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return order[out[i].Window] < order[out[j].Window]
	})
	return out
}

// Remaining is the headroom line: the percentage the battery showed, over
// time. It sawtooths, climbing back up whenever a window resets.
func Remaining(samples []Sample, series Series) []Point {
	var out []Point
	forEach(samples, series, func(sample Sample, entry Entry) {
		out = append(out, Point{At: sample.Time(), Value: entry.Remaining})
	})
	return out
}

// TokenUsage converts the provider's running totals into what was consumed
// between one sample and the next.
//
// Totals restart when the window rolls over. A drop therefore means a reset,
// not negative usage, and everything counted after it is new consumption.
func TokenUsage(samples []Sample, series Series) []Point {
	var out []Point
	var previous int64
	first := true
	forEach(samples, series, func(sample Sample, entry Entry) {
		if entry.Tokens == nil {
			return
		}
		total := entry.Tokens.Total()
		switch {
		case first:
			// The first sample has nothing to subtract from: reporting the
			// running total as consumption would invent a spike.
			first = false
		case total < previous:
			out = append(out, Point{At: sample.Time(), Value: float64(total)})
		default:
			out = append(out, Point{At: sample.Time(), Value: float64(total - previous)})
		}
		previous = total
	})
	return out
}

// Heatmap is consumption bucketed by local weekday and hour. Index [0] is
// Sunday, matching time.Weekday.
type Heatmap [7][24]float64

// Max is the largest bucket, for scaling the colour ramp. It is zero when
// nothing was consumed.
func (h Heatmap) Max() float64 {
	var max float64
	for _, day := range h {
		for _, value := range day {
			if value > max {
				max = value
			}
		}
	}
	return max
}

// Activity buckets consumption by when it happened, in percentage points of
// the limit used. Percentages work for every provider, including the ones that
// report no tokens at all.
//
// A window reset shows up as headroom going back up, which is not usage, so
// those intervals contribute nothing. Each interval is credited to the hour the
// later sample falls in; at a sampling interval of minutes the error where an
// interval straddles an hour boundary is not visible in the chart.
func Activity(samples []Sample, series Series, loc *time.Location) Heatmap {
	if loc == nil {
		loc = time.Local
	}
	var out Heatmap
	previous := -1.0
	forEach(samples, series, func(sample Sample, entry Entry) {
		remaining := entry.Remaining
		defer func() { previous = remaining }()
		if previous < 0 || remaining >= previous {
			return
		}
		at := sample.Time().In(loc)
		out[int(at.Weekday())][at.Hour()] += previous - remaining
	})
	return out
}

// forEach visits the samples holding the series, oldest first.
func forEach(samples []Sample, series Series, visit func(Sample, Entry)) {
	for _, sample := range samples {
		for _, entry := range sample.Entries {
			if entry.Source != series.Source || entry.Window != series.Window {
				continue
			}
			visit(sample, entry)
			break
		}
	}
}

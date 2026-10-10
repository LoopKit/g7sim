package g7

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// Algorithm states the 4e and backfill records carry.
const (
	algoWarmup  = 2
	algoOK      = 6
	algoExpired = 24
)

// GlucoseMode is how readings are made up: "sine" around Value, "fixed" at
// Value, or "walk", a mean-reverting random walk around Value.
type GlucoseMode struct {
	Kind          string  `json:"kind"`
	Value         float64 `json:"value"`
	Amplitude     float64 `json:"amplitude,omitempty"`
	PeriodMinutes float64 `json:"periodMinutes,omitempty"`
	// AlgorithmState, when set, replaces the computed state (e.g. 18 for a
	// temporary sensor issue, 25 for a failed sensor).
	AlgorithmState byte `json:"algorithmState,omitempty"`
}

type GlucoseModel struct {
	state *State
}

func NewGlucoseModel(state *State) *GlucoseModel { return &GlucoseModel{state: state} }

func (g *GlucoseModel) SetMode(m GlucoseMode) { g.state.Glucose = m }

func (g *GlucoseModel) Describe() GlucoseMode { return g.state.Glucose }

func (g *GlucoseModel) raw(i int, interval time.Duration) float64 {
	m := g.state.Glucose
	switch m.Kind {
	case "fixed":
		return m.Value
	case "walk":
		prev := m.Value
		if r := g.state.latest(); r != nil && r.Glucose != nil {
			prev = float64(*r.Glucose) - g.state.CalOffset
		}
		return prev + (m.Value-prev)*0.05 + rand.NormFloat64()*2
	default:
		period := m.PeriodMinutes
		if period <= 0 {
			period = 240
		}
		minutes := float64(i) * interval.Minutes()
		return m.Value + m.Amplitude*math.Sin(2*math.Pi*minutes/period)
	}
}

func (g *GlucoseModel) Reading(i int, sensorTime uint32, interval time.Duration) *Reading {
	r := &Reading{Index: i, Time: sensorTime}
	age := time.Duration(sensorTime) * time.Second
	switch {
	case age < g.state.Warmup:
		r.State = algoWarmup
		return r
	case age > g.state.SessionLength:
		r.State = algoExpired
		return r
	}
	r.State = algoOK
	if s := g.state.Glucose.AlgorithmState; s != 0 {
		r.State = s
		if s != algoOK {
			return r
		}
	}

	v := clamp(g.raw(i, interval)+g.state.CalOffset, 40, 400)
	value := uint16(math.Round(v))
	r.Glucose = &value
	if prev := g.state.latest(); prev != nil && prev.Glucose != nil && prev.Index == i-1 {
		trend := (v - float64(*prev.Glucose)) / interval.Minutes()
		trend = math.Round(trend*10) / 10
		r.Trend = &trend
	}
	return r
}

// Calibrate shifts later readings so the one at `at` would have read glucose.
func (g *GlucoseModel) Calibrate(glucose uint16, at uint32) {
	var nearest *Reading
	for _, r := range g.state.Readings {
		if r.Glucose != nil && (nearest == nil || absDiff(r.Time, at) < absDiff(nearest.Time, at)) {
			nearest = r
		}
	}
	if nearest == nil {
		return
	}
	g.state.CalOffset += float64(glucose) - float64(*nearest.Glucose)
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func (m GlucoseMode) String() string {
	return fmt.Sprintf("%s %.0f", m.Kind, m.Value)
}

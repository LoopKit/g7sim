package g7

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"
)

type Reading struct {
	Index       int      `json:"index"`
	Time        uint32   `json:"time"` // sensor seconds
	Glucose     *uint16  `json:"glucose,omitempty"`
	State       byte     `json:"state"`
	Trend       *float64 `json:"trend,omitempty"` // mg/dL/min
	DisplayOnly bool     `json:"displayOnly,omitempty"`
}

func (r *Reading) String() string {
	g := "---"
	if r.Glucose != nil {
		g = fmt.Sprint(*r.Glucose)
	}
	t := "-"
	if r.Trend != nil {
		t = fmt.Sprintf("%+.1f", *r.Trend)
	}
	return fmt.Sprintf("%s mg/dL trend %s state %d at sensor %ds", g, t, r.State, r.Time)
}

const keptReadings = 24 * 12

// Models are the G7-family sensors and the name prefix each advertises.
// They share the protocol; G7SensorKit tells them apart by prefix alone.
var Models = map[string]string{"g7": "DXCM", "oneplus": "DX02", "stelo": "DX01"}

// GracePeriod is how long a sensor keeps reading past its rated lifetime.
// The session length in the 52 reply includes it.
const GracePeriod = 12 * time.Hour

// Preset is what a real 10- or 15-day sensor reports in its 52 reply.
type Preset struct {
	SessionLength   time.Duration
	Warmup          time.Duration
	Algorithm       uint32
	MaxLifetimeDays uint16
}

// Presets are from captured 52 replies:
//
//	10-day: 52 00 c0d70d00 5406 00020404 ff 0c00
//	15-day: 52 00 406f1400 880e 00010a04 ff 1100
var Presets = map[int]Preset{
	10: {907200 * time.Second, 1620 * time.Second, 0x04040200, 12},
	15: {1339200 * time.Second, 3720 * time.Second, 0x040a0100, 17},
}

type State struct {
	Serial          uint64        `json:"serial"`
	Code            string        `json:"code"`
	Model           string        `json:"model,omitempty"` // empty in older state files: g7
	Suffix          string        `json:"suffix"`
	Activation      time.Time     `json:"activation"`
	Days            int           `json:"days,omitempty"`
	SessionLength   time.Duration `json:"sessionLength"`
	Warmup          time.Duration `json:"warmup"`
	Algorithm       uint32        `json:"algorithm,omitempty"`
	MaxLifetimeDays uint16        `json:"maxLifetimeDays,omitempty"`

	Slots       map[byte]*Slot  `json:"slots"`
	Identity    *SensorIdentity `json:"identity,omitempty"`
	Calibration *Calibration    `json:"calibration,omitempty"`
	CalOffset   float64         `json:"calOffset"`
	Glucose     GlucoseMode     `json:"glucose"`
	Readings    []*Reading      `json:"readings"`
}

func NewState(serial uint64, code, model, suffix string, activation time.Time, days int) *State {
	s := &State{
		Serial:     serial,
		Code:       code,
		Model:      model,
		Suffix:     suffix,
		Activation: activation,
		Slots:      map[byte]*Slot{},
		Glucose:    GlucoseMode{Kind: "sine", Value: 120, Amplitude: 50, PeriodMinutes: 240},
	}
	s.applyPreset(days)
	return s
}

func (s *State) applyPreset(days int) {
	if _, ok := Presets[days]; !ok {
		days = 10
	}
	p := Presets[days]
	s.Days = days
	s.SessionLength, s.Warmup = p.SessionLength, p.Warmup
	s.Algorithm, s.MaxLifetimeDays = p.Algorithm, p.MaxLifetimeDays
}

// Lifetime is the rated wear time, before the grace period.
func (s *State) Lifetime() time.Duration {
	return s.SessionLength - GracePeriod
}

func LoadState(path string) (*State, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &State{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	if s.Slots == nil {
		s.Slots = map[byte]*Slot{}
	}
	// Files from before presets were stored: infer one, keeping the lengths.
	if s.Algorithm == 0 {
		days := 10
		if s.SessionLength >= Presets[15].SessionLength {
			days = 15
		}
		sessionLength, warmup := s.SessionLength, s.Warmup
		s.applyPreset(days)
		s.SessionLength, s.Warmup = sessionLength, warmup
	}
	return s, nil
}

func (s *State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := ioutil.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *State) latest() *Reading {
	if len(s.Readings) == 0 {
		return nil
	}
	return s.Readings[len(s.Readings)-1]
}

func (s *State) nextIndex() int {
	if r := s.latest(); r != nil {
		return r.Index + 1
	}
	return 0
}

func (s *State) addReading(r *Reading) {
	s.Readings = append(s.Readings, r)
	if len(s.Readings) > keptReadings {
		s.Readings = s.Readings[len(s.Readings)-keptReadings:]
	}
}

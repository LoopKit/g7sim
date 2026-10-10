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

type State struct {
	Serial        uint64        `json:"serial"`
	Code          string        `json:"code"`
	Suffix        string        `json:"suffix"`
	Activation    time.Time     `json:"activation"`
	SessionLength time.Duration `json:"sessionLength"`
	Warmup        time.Duration `json:"warmup"`

	Slots       map[byte]*Slot `json:"slots"`
	Calibration *Calibration   `json:"calibration,omitempty"`
	CalOffset   float64        `json:"calOffset"`
	Glucose     GlucoseMode    `json:"glucose"`
	Readings    []*Reading     `json:"readings"`
}

func NewState(serial uint64, code, suffix string, activation time.Time, days int) *State {
	s := &State{
		Serial:     serial,
		Code:       code,
		Suffix:     suffix,
		Activation: activation,
		Slots:      map[byte]*Slot{},
		Glucose:    GlucoseMode{Kind: "sine", Value: 120, Amplitude: 50, PeriodMinutes: 240},
	}
	// The lengths a real sensor reports in its 52 reply; each includes the
	// 12-hour grace period.
	if days == 15 {
		s.SessionLength, s.Warmup = 1339200*time.Second, 3720*time.Second
	} else {
		s.SessionLength, s.Warmup = 907200*time.Second, 1620*time.Second
	}
	return s
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

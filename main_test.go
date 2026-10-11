package main

import (
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }

func TestNewStateDefaults(t *testing.T) {
	s, err := newState(sensorConfig{Model: "stelo"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Days != 15 || s.Lifetime() != 15*24*time.Hour || s.Warmup != 62*time.Minute {
		t.Errorf("stelo: %d days, lifetime %v, warmup %v", s.Days, s.Lifetime(), s.Warmup)
	}
	if !codePattern.MatchString(s.Code) || s.Serial == 0 {
		t.Errorf("random code %q serial %d", s.Code, s.Serial)
	}
	if age := time.Since(s.Activation); age < 119*time.Minute || age > 121*time.Minute {
		t.Errorf("default age %v, want 2 h", age)
	}
}

func TestNewStateOverrides(t *testing.T) {
	s, err := newState(sensorConfig{Model: "g7", Days: 10, Code: "0042", Serial: 123,
		AgeMinutes: f(0), LifetimeHours: f(36), WarmupMinutes: f(5)})
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionLength != 48*time.Hour || s.Warmup != 5*time.Minute || s.Code != "0042" || s.Serial != 123 {
		t.Errorf("got session %v warmup %v code %s serial %d", s.SessionLength, s.Warmup, s.Code, s.Serial)
	}
	if s.Algorithm != 0x04040200 || s.MaxLifetimeDays != 12 {
		t.Errorf("10-day preset tail not kept: %08x %d", s.Algorithm, s.MaxLifetimeDays)
	}
}

func TestNewStateRejects(t *testing.T) {
	for name, c := range map[string]sensorConfig{
		"model":           {Model: "g6"},
		"days":            {Days: 14},
		"code":            {Code: "12a4"},
		"negative age":    {AgeMinutes: f(-1)},
		"zero lifetime":   {LifetimeHours: f(0)},
		"warmup too long": {WarmupMinutes: f(2000)},
		"warmup > life":   {LifetimeHours: f(1), WarmupMinutes: f(90)},
	} {
		if _, err := newState(c); err == nil {
			t.Errorf("%s: accepted %+v", name, c)
		}
	}
}

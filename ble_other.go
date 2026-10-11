//go:build !linux
// +build !linux

package main

import (
	"errors"

	"github.com/loopkit/g7sim/g7"
)

func NewRadio(sensor *g7.Sensor, maxConnections int) (*Radio, error) {
	return nil, errors.New("the Bluetooth transport needs Linux; use -transport impossible for the iOS Simulator")
}

type Radio struct{}

func (r *Radio) Run()  {}
func (r *Radio) Stop() {}

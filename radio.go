package main

import "github.com/loopkit/g7sim/g7"

// radio is how the sensor reaches a phone: a Bluetooth adapter (ble.go) or
// ImpossiBLE's socket for the iOS Simulator (impossible.go).
type radio interface {
	Run()
	Stop()
}

// event is one thing that happened on a link, queued so its handler sees
// them in order and the transport's read loop never blocks on protocol work.
type event struct {
	char       g7.Char
	data       []byte
	subscribed bool
}

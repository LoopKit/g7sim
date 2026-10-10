package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/loopkit/g7sim/g7"
)

func main() {
	statePath := flag.String("state", "g7state.json", "sensor state file")
	fresh := flag.Bool("fresh", false, "start a new sensor instead of restoring the state file")
	code := flag.String("code", "", "pairing code for a new sensor (default: random)")
	serial := flag.Uint64("serial", 0, "serial for a new sensor (default: random)")
	age := flag.Duration("age", 2*time.Hour, "how long ago a new sensor was activated; 0 starts in warmup")
	days := flag.Int("days", 10, "session length of a new sensor: 10 or 15")
	maxConn := flag.Int("max-connections", 4, "simultaneous links the controller accepts")
	api := flag.String("api", ":8080", "HTTP control API address")
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	var state *g7.State
	if !*fresh {
		s, err := g7.LoadState(*statePath)
		if err != nil && !os.IsNotExist(err) {
			log.Fatalf("restoring %s: %v (use -fresh to start a new sensor)", *statePath, err)
		}
		state = s
	}
	if state == nil {
		rand.Seed(time.Now().UnixNano())
		if *serial == 0 {
			*serial = 3000000000 + uint64(rand.Int63n(999999999))
		}
		if *code == "" {
			*code = fmt.Sprintf("%04d", rand.Intn(10000))
		}
		// Derive the two-letter name suffix from the serial, so a given serial
		// always presents as the same sensor (name and BLE address) even after
		// the state file is lost and recreated.
		suffix := fmt.Sprintf("%c%c", 'a'+byte((*serial/26)%26), 'a'+byte(*serial%26))
		state = g7.NewState(*serial, *code, suffix, time.Now().Add(-*age), *days)
		if err := state.Save(*statePath); err != nil {
			log.Fatalf("saving %s: %v", *statePath, err)
		}
		log.Printf("new sensor")
	}

	log.Printf("g7sim — for development use only, not a Dexcom product; simulated data only")

	sensor := g7.NewSensor(state, *statePath, g7.MeasuredTiming())
	log.Printf("%s serial %d pairing code %s, activated %s", sensor.Name(), sensor.Serial(), sensor.PairingCode(), state.Activation.Format(time.RFC3339))

	radio, err := NewRadio(sensor, *maxConn)
	if err != nil {
		log.Fatalf("starting Bluetooth: %v", err)
	}
	go serveAPI(*api, sensor)
	radio.Run()
}

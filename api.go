package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/loopkit/g7sim/g7"
)

// serveAPI exposes the sensor's state and lets a tester steer it:
//
//	GET  /status                 sensor, slots, current window, latest reading
//	POST /glucose  {"kind":"fixed","value":65}
//	               {"kind":"sine","value":120,"amplitude":50,"periodMinutes":240}
//	               {"kind":"walk","value":140}
//	               add "algorithmState":18 to simulate a sensor issue
//	POST /forget                 drop every display's key
func serveAPI(addr string, sensor *g7.Sensor) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(indexHTML))
	})
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sensor.Status())
	})
	http.HandleFunc("/glucose", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST a glucose mode", http.StatusMethodNotAllowed)
			return
		}
		var m g7.GlucoseMode
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m.Kind == "" {
			http.Error(w, "bad glucose mode", http.StatusBadRequest)
			return
		}
		sensor.SetGlucoseMode(m)
		log.Printf("glucose mode now %s", m)
		writeJSON(w, sensor.Status())
	})
	http.HandleFunc("/forget", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST to forget", http.StatusMethodNotAllowed)
			return
		}
		sensor.ForgetSlots()
		log.Printf("forgot every display's key")
		writeJSON(w, sensor.Status())
	})
	log.Printf("API on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

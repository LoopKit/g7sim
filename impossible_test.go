package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/loopkit/g7sim/g7"
)

type simTestClient struct {
	t    *testing.T
	conn net.Conn
	in   *bufio.Scanner
}

func (c *simTestClient) send(msg map[string]interface{}) {
	b, _ := json.Marshal(msg)
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

// expect reads until a message of the given type arrives.
func (c *simTestClient) expect(typ string) map[string]interface{} {
	c.t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for c.in.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(c.in.Bytes(), &msg); err != nil {
			c.t.Fatal(err)
		}
		if msg["type"] == typ {
			return msg
		}
	}
	c.t.Fatalf("no %s: %v", typ, c.in.Err())
	return nil
}

// Walks the calls G7SensorKit makes through ImpossiBLE: scan, connect,
// discovery, subscribe, a write the handler answers, and a disconnect.
func TestSimRadioServesTheSensor(t *testing.T) {
	dir, err := ioutil.TempDir("/tmp", "g7sim")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Three seconds into a reading's empty burst, so the sensor advertises.
	age := 120 + 3.0/60
	state, err := newState(sensorConfig{Code: "1234", Serial: 3123456789, AgeMinutes: &age})
	if err != nil {
		t.Fatal(err)
	}
	sensor := g7.NewSensor(state, filepath.Join(dir, "state.json"), g7.MeasuredTiming())
	socket := filepath.Join(dir, "sock")
	r, err := NewSimRadio(sensor, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	go r.Run()

	if _, err := NewSimRadio(sensor, socket); err == nil {
		t.Error("a second provider took over a live socket")
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := &simTestClient{t: t, conn: conn, in: bufio.NewScanner(conn)}
	c.send(map[string]interface{}{"type": "hello", "bundleId": "com.example.loop", "clientVersion": "3.1.0"})

	c.send(map[string]interface{}{"type": "scan", "services": []string{"FEBC"}})
	d := c.expect("didDiscover")
	id := peripheralID(3123456789)
	if d["id"] != id || d["name"] != sensor.Name() {
		t.Fatalf("discovered %v", d)
	}
	adv := d["adv"].(map[string]interface{})
	if mfg, _ := base64.StdEncoding.DecodeString(adv["kCBAdvDataManufacturerData"].(string)); len(mfg) != 6 || mfg[0] != 0xD0 {
		t.Errorf("manufacturer data % x", mfg)
	}

	c.send(map[string]interface{}{"type": "connect", "id": id})
	c.expect("didConnect")

	c.send(map[string]interface{}{"type": "discoverServices", "id": id})
	services := c.expect("didDiscoverServices")["services"].([]interface{})
	if len(services) != 2 {
		t.Fatalf("services %v", services)
	}
	cgm := services[0].(map[string]interface{})["id"].(string)
	c.send(map[string]interface{}{"type": "discoverCharacteristics", "id": id, "serviceId": cgm})
	chars := c.expect("didDiscoverCharacteristics")["characteristics"].([]interface{})
	ids := map[string]string{}
	for _, ch := range chars {
		m := ch.(map[string]interface{})
		ids[m["uuid"].(string)] = m["id"].(string)
	}
	auth := ids["F8083535-849E-531C-C594-30F1F86A4EA5"]
	if len(chars) != 5 || auth == "" {
		t.Fatalf("characteristics %v", chars)
	}

	c.send(map[string]interface{}{"type": "setNotify", "id": id, "characteristicId": auth, "enabled": true})
	if n := c.expect("didUpdateNotification"); n["enabled"] != true || n["error"] != "" {
		t.Fatalf("subscribe: %v", n)
	}

	// A reconnect challenge with no stored key: the sensor answers with noise.
	challenge := []byte{0x02, 1, 2, 3, 4, 5, 6, 7, 8, g7.DisplayPhone}
	c.send(map[string]interface{}{"type": "write", "id": id, "characteristicId": auth,
		"value": base64.StdEncoding.EncodeToString(challenge), "writeType": 0})
	if w := c.expect("didWriteValue"); w["error"] != "" {
		t.Fatalf("write: %v", w)
	}
	if v := c.expect("didUpdateValue"); v["characteristicId"] != auth || v["value"] == "" {
		t.Fatalf("challenge reply %v", v)
	}

	c.send(map[string]interface{}{"type": "cancel", "id": id})
	c.expect("didDisconnect")
}

func TestSimRadioCountsReadingsNothingWouldWake(t *testing.T) {
	dir, err := ioutil.TempDir("/tmp", "g7sim")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	age := 120 + 3.0/60
	state, err := newState(sensorConfig{Code: "1234", Serial: 3123456789, AgeMinutes: &age})
	if err != nil {
		t.Fatal(err)
	}
	sensor := g7.NewSensor(state, filepath.Join(dir, "state.json"), g7.MeasuredTiming())
	r, err := NewSimRadio(sensor, filepath.Join(dir, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	go r.Run()

	time.Sleep(200 * time.Millisecond)
	r.mu.Lock()
	missed := r.missedWakes
	r.mu.Unlock()
	if missed != 1 {
		t.Errorf("missed wakes %d, want 1", missed)
	}
}

// An app that ends with a scan lodged gets it back through state restoration
// when it returns; one that was force-quit does not.
func TestSimRadioHoldsATerminatedAppsRequests(t *testing.T) {
	dir, err := ioutil.TempDir("/tmp", "g7sim")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	state, err := newState(sensorConfig{Code: "1234", Serial: 3123456789})
	if err != nil {
		t.Fatal(err)
	}
	sensor := g7.NewSensor(state, filepath.Join(dir, "state.json"), g7.MeasuredTiming())
	socket := filepath.Join(dir, "sock")
	r, err := NewSimRadio(sensor, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	go r.Run()

	launch := func() *simTestClient {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		c := &simTestClient{t: t, conn: conn, in: bufio.NewScanner(conn)}
		c.send(map[string]interface{}{"type": "hello", "bundleId": "com.example.loop"})
		return c
	}
	held := func() *heldRequests {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.held
	}
	waitHeld := func(want bool) {
		for i := 0; i < 100 && (held() != nil) != want; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		if (held() != nil) != want {
			t.Fatalf("held %v, want held %v", held(), want)
		}
	}

	c := launch()
	c.send(map[string]interface{}{"type": "scan", "services": []string{"FEBC"}})
	c.expect("didDiscover")
	c.conn.Close()
	waitHeld(true)

	c = launch()
	c.send(map[string]interface{}{"type": "registerForConnectionEvents"})
	restored := c.expect("didRestoreState")
	if s := restored["scanServices"].([]interface{}); len(s) != 1 || s[0] != "FEBC" {
		t.Errorf("restored %v", restored)
	}
	waitHeld(false)

	r.ForceQuit()
	c.conn.Close()
	time.Sleep(100 * time.Millisecond)
	waitHeld(false)
}

package main

import (
	"bufio"
	"encoding/json"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type end struct {
	t    *testing.T
	conn net.Conn
	in   chan map[string]interface{}
}

func newEnd(t *testing.T, conn net.Conn) *end {
	e := &end{t: t, conn: conn, in: make(chan map[string]interface{}, 64)}
	go func() {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			var msg map[string]interface{}
			json.Unmarshal(sc.Bytes(), &msg)
			e.in <- msg
		}
		close(e.in)
	}()
	return e
}

func (e *end) send(msg map[string]interface{}) {
	b, _ := json.Marshal(msg)
	e.conn.Write(append(b, '\n'))
}

func (e *end) expect(typ string) map[string]interface{} {
	e.t.Helper()
	select {
	case msg, ok := <-e.in:
		if !ok {
			e.t.Fatalf("closed, want %s", typ)
		}
		if msg["type"] != typ {
			e.t.Fatalf("got %v, want %s", msg, typ)
		}
		return msg
	case <-time.After(2 * time.Second):
		e.t.Fatalf("no %s", typ)
	}
	return nil
}

func (e *end) quiet() {
	e.t.Helper()
	select {
	case msg, ok := <-e.in:
		if ok {
			e.t.Fatalf("unexpected %v", msg)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func (e *end) closed() {
	e.t.Helper()
	select {
	case msg, ok := <-e.in:
		if ok {
			e.t.Fatalf("unexpected %v", msg)
		}
	case <-time.After(2 * time.Second):
		e.t.Fatal("still open")
	}
}

func listen(t *testing.T, path string) chan *end {
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan *end, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			ch <- newEnd(t, c)
		}
	}()
	t.Cleanup(func() { l.Close() })
	return ch
}

func accepted(t *testing.T, ch chan *end) *end {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("the mux did not connect")
	}
	return nil
}

type fakeSuspender struct {
	mu       sync.Mutex
	attached int
	wakes    int
}

func (f *fakeSuspender) Attach(pid int, bundle string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attached = pid
}

func (f *fakeSuspender) Detach(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attached == pid {
		f.attached = 0
	}
}

func (f *fakeSuspender) Wake(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attached == pid {
		f.wakes++
	}
}

func (f *fakeSuspender) state() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attached, f.wakes
}

func TestMuxRoutesEachPeripheralToItsProvider(t *testing.T) {
	// Unix socket paths are short; $TMPDIR on macOS is too long for one.
	dir, err := ioutil.TempDir("/tmp", "mux")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sensorPath, podPath, appPath := filepath.Join(dir, "g7.sock"), filepath.Join(dir, "pod.sock"), filepath.Join(dir, "app.sock")
	sensors, pods := listen(t, sensorPath), listen(t, podPath)

	l, err := net.Listen("unix", appPath)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	suspended := &fakeSuspender{}
	m := newMux([]string{sensorPath, podPath}, suspended)
	go func() {
		c, err := l.Accept()
		if err == nil {
			m.serve(c)
		}
	}()
	conn, err := net.Dial("unix", appPath)
	if err != nil {
		t.Fatal(err)
	}
	app := newEnd(t, conn)
	sensor, pod := accepted(t, sensors), accepted(t, pods)

	app.send(map[string]interface{}{"type": "hello", "bundleId": "org.loopkit.Loop", "pid": 1})
	sensor.expect("hello")
	pod.expect("hello")
	app.send(map[string]interface{}{"type": "scan"})
	sensor.expect("scan")
	pod.expect("scan")

	sensor.send(map[string]interface{}{"type": "didDiscover", "id": "SENSOR"})
	app.expect("didDiscover")
	pod.send(map[string]interface{}{"type": "didDiscover", "id": "POD"})
	app.expect("didDiscover")

	// Requests for a peripheral go to its provider alone.
	app.send(map[string]interface{}{"type": "connect", "id": "POD"})
	pod.expect("connect")
	sensor.quiet()
	_, before := suspended.state()
	pod.send(map[string]interface{}{"type": "didConnect", "id": "POD"})
	app.expect("didConnect")
	// Every provider's events wake a suspended app, not only the sensor's.
	if attached, wakes := suspended.state(); attached != 1 || wakes != before+1 {
		t.Fatalf("app %d woken %d times for the pod's event, want app 1 woken once", attached, wakes-before)
	}

	// An advertisement wakes the app only if its scan would find it.
	app.send(map[string]interface{}{"type": "scan", "services": []string{"C00A"}})
	sensor.expect("scan")
	pod.expect("scan")
	advert := func(services ...string) map[string]interface{} {
		return map[string]interface{}{"type": "didDiscover", "id": "POD",
			"adv": map[string]interface{}{"kCBAdvDataServiceUUIDs": services}}
	}
	_, before = suspended.state()
	pod.send(advert("4024", "C001"))
	app.expect("didDiscover")
	if _, wakes := suspended.state(); wakes != before {
		t.Fatal("woken for an advertisement the scan filters out")
	}
	pod.send(advert("4024", "0000C00A-0000-1000-8000-00805F9B34FB"))
	app.expect("didDiscover")
	if _, wakes := suspended.state(); wakes != before+1 {
		t.Fatal("not woken for an advertisement the scan finds")
	}

	// A connect to a peripheral the app knows from before goes to every
	// provider; the one holding it is its owner, the others' refusals drop.
	app.send(map[string]interface{}{"type": "connect", "id": "OLD-SENSOR"})
	sensor.expect("connect")
	pod.expect("connect")
	pod.send(map[string]interface{}{"type": "didFailConnect", "id": "OLD-SENSOR", "error": "unknown peripheral"})
	app.quiet()
	app.send(map[string]interface{}{"type": "discoverServices", "id": "OLD-SENSOR"})
	sensor.expect("discoverServices")
	pod.quiet()

	// A provider that goes away disconnects its peripherals from the app;
	// a restarted one sees the session from its start.
	pod.conn.Close()
	if msg := app.expect("didDisconnect"); msg["id"] != "POD" {
		t.Fatalf("disconnected %v", msg)
	}
	pod = accepted(t, pods)
	pod.expect("hello")
	pod.expect("scan")

	// The app's end is each provider's.
	conn.Close()
	sensor.closed()
	pod.closed()
	if attached, _ := suspended.state(); attached != 0 {
		t.Fatalf("still following app %d after it left", attached)
	}
}

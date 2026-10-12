package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"
)

// line is one end of a newline-delimited JSON connection.
type line struct {
	conn net.Conn
	mu   sync.Mutex
}

func (l *line) send(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := l.conn.Write(append(append([]byte(nil), b...), '\n')); err != nil {
		log.Printf("send to %s: %v", l.conn.RemoteAddr(), err)
	}
}

func (l *line) sendMsg(msg map[string]interface{}) {
	b, _ := json.Marshal(msg)
	l.send(b)
}

func readLines(conn net.Conn, each func(raw []byte, msg map[string]interface{})) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			log.Printf("bad message: %v", err)
			continue
		}
		each(append([]byte(nil), sc.Bytes()...), msg)
	}
}

func str(msg map[string]interface{}, key string) string {
	s, _ := msg[key].(string)
	return s
}

type mux struct {
	providers []string

	mu      sync.Mutex
	current *session
}

func newMux(providers []string) *mux {
	return &mux{providers: providers}
}

// serve connects an app to every provider. A new app takes over from the
// previous one, as with a single provider.
func (m *mux) serve(conn net.Conn) {
	s := &session{
		mux:       m,
		app:       &line{conn: conn},
		backends:  make([]*line, len(m.providers)),
		owner:     map[string]int{},
		pending:   map[string]map[int]bool{},
		connected: map[string]int{},
	}
	m.mu.Lock()
	old := m.current
	m.current = s
	m.mu.Unlock()
	if old != nil {
		log.Printf("a new app connected; dropping the previous one")
		old.close()
	}
	for i := range m.providers {
		go s.keepProvider(i)
	}
	readLines(conn, s.fromApp)
	s.close()
}

// session is one app's connection and its connections to the providers.
type session struct {
	mux *mux
	app *line

	mu        sync.Mutex
	closed    bool
	hello     []byte
	scan      []byte // the app's scan, while it scans
	backends  []*line
	owner     map[string]int          // provider of each peripheral id
	pending   map[string]map[int]bool // providers asked to connect to a peripheral no one has claimed yet
	connected map[string]int          // peripherals connected to the app, by provider
}

func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	backends := s.backends
	s.mu.Unlock()
	s.app.conn.Close()
	// Each provider sees the app end, as it would without the mux.
	for _, b := range backends {
		if b != nil {
			b.conn.Close()
		}
	}
}

// keepProvider connects to provider i and reconnects when it restarts, for
// as long as the app is there.
func (s *session) keepProvider(i int) {
	path := s.mux.providers[i]
	reported := false
	for {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		conn, err := net.Dial("unix", path)
		if err != nil {
			if !reported {
				log.Printf("%s: %v; retrying", path, err)
				reported = true
			}
			time.Sleep(time.Second)
			continue
		}
		reported = false
		b := &line{conn: conn}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			conn.Close()
			return
		}
		s.backends[i] = b
		// A provider that joins late sees the session as one that just began.
		hello, scan := s.hello, s.scan
		s.mu.Unlock()
		log.Printf("%s: connected", path)
		if hello != nil {
			b.send(hello)
		}
		if scan != nil {
			b.send(scan)
		}
		readLines(conn, func(raw []byte, msg map[string]interface{}) { s.fromProvider(i, raw, msg) })
		s.providerGone(i)
		log.Printf("%s: disconnected", path)
	}
}

// providerGone forgets a provider that went away. Its peripherals disconnect
// from the app, as a device that powers off does.
func (s *session) providerGone(i int) {
	s.mu.Lock()
	s.backends[i] = nil
	for id, o := range s.owner {
		if o == i {
			delete(s.owner, id)
		}
	}
	var lost []string
	for id, o := range s.connected {
		if o == i {
			lost = append(lost, id)
			delete(s.connected, id)
		}
	}
	var failed []string
	for id, pend := range s.pending {
		if pend[i] {
			delete(pend, i)
			if len(pend) == 0 {
				delete(s.pending, id)
				failed = append(failed, id)
			}
		}
	}
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	for _, id := range lost {
		s.app.sendMsg(map[string]interface{}{"type": "didDisconnect", "id": id, "error": "provider went away",
			"timestamp": float64(time.Now().UnixNano())/1e9 - 978307200, "isReconnecting": false})
	}
	for _, id := range failed {
		s.app.sendMsg(map[string]interface{}{"type": "didFailConnect", "id": id, "error": "unknown peripheral"})
	}
}

func (s *session) live() []int {
	var out []int
	for i, b := range s.backends {
		if b != nil {
			out = append(out, i)
		}
	}
	return out
}

func (s *session) sendTo(indices []int, raw []byte) {
	s.mu.Lock()
	var targets []*line
	for _, i := range indices {
		if b := s.backends[i]; b != nil {
			targets = append(targets, b)
		}
	}
	s.mu.Unlock()
	for _, b := range targets {
		b.send(raw)
	}
}

func (s *session) fromApp(raw []byte, msg map[string]interface{}) {
	t, id := str(msg, "type"), str(msg, "id")
	s.mu.Lock()
	all := s.live()
	var to []int
	switch {
	case t == "hello":
		s.hello = raw
		to = all
	case t == "scan":
		s.scan = raw
		to = all
	case t == "stopScan":
		s.scan = nil
		to = all
	case id == "":
		to = all
	default:
		if o, ok := s.owner[id]; ok {
			to = []int{o}
		} else if pend := s.pending[id]; pend != nil {
			for i := range pend {
				to = append(to, i)
			}
		} else if t == "connect" && len(all) > 0 {
			// The app knows this peripheral from before (retrieved, or
			// restored); the provider that has it holds the connect and
			// the others refuse it.
			pend := map[int]bool{}
			for _, i := range all {
				pend[i] = true
			}
			s.pending[id] = pend
			to = all
		}
	}
	s.mu.Unlock()
	if len(to) == 0 && id != "" {
		switch t {
		case "connect":
			s.app.sendMsg(map[string]interface{}{"type": "didFailConnect", "id": id, "error": "unknown peripheral"})
		case "cancel":
			s.app.sendMsg(map[string]interface{}{"type": "didDisconnect", "id": id, "error": "",
				"timestamp": float64(time.Now().UnixNano())/1e9 - 978307200, "isReconnecting": false})
		default:
			log.Printf("app: %s for unknown peripheral %s dropped", t, id)
		}
		return
	}
	s.sendTo(to, raw)
}

func (s *session) fromProvider(i int, raw []byte, msg map[string]interface{}) {
	t, id := str(msg, "type"), str(msg, "id")
	s.mu.Lock()
	forward := true
	switch {
	case t == "connectionRejected":
		forward = false
		log.Printf("%s refused the app: %s", s.mux.providers[i], str(msg, "message"))
	case t == "didSetMockConfiguration":
		// Every provider answers; the app asked once.
		live := s.live()
		forward = len(live) > 0 && live[0] == i
	case t == "didRestoreState":
		if ids, ok := msg["peripheralIds"].([]interface{}); ok {
			for _, v := range ids {
				if p, ok := v.(string); ok {
					s.owner[p] = i
				}
			}
		}
	case id != "":
		if pend := s.pending[id]; pend[i] {
			if t == "didFailConnect" {
				delete(pend, i)
				switch len(pend) {
				case 0:
					delete(s.pending, id)
				case 1:
					// The one left is holding the connect: it has the peripheral.
					for o := range pend {
						s.owner[id] = o
					}
					delete(s.pending, id)
					forward = false
				default:
					forward = false
				}
				break
			}
			s.owner[id] = i
			delete(s.pending, id)
		} else if o, ok := s.owner[id]; ok && o != i {
			forward = false
			log.Printf("%s: %s for %s, which %s has, dropped", s.mux.providers[i], t, id, s.mux.providers[o])
		} else if !ok {
			s.owner[id] = i
		}
		if forward {
			switch t {
			case "didConnect":
				s.connected[id] = i
			case "didDisconnect", "didFailConnect":
				delete(s.connected, id)
			}
		}
	}
	s.mu.Unlock()
	if forward {
		s.app.send(raw)
	}
}

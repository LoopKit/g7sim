// impossible-mux lets several simulated peripherals serve one app in the iOS
// Simulator. ImpossiBLE connects the app to a single provider socket; the mux
// takes that socket and fans the app out to providers on their own sockets,
// such as g7sim and the LoopKit/pod simulator:
//
//	g7sim -transport impossible -socket /tmp/impossible-g7.sock
//	pod -transport impossible -socket /tmp/impossible-pod.sock
//	impossible-mux /tmp/impossible-g7.sock /tmp/impossible-pod.sock
//
// Each provider sees the app as its only client, as it would without the mux.
//
// With -suspend the mux, not each provider, stops the app while iOS would have
// it suspended, and wakes it for any provider's Bluetooth event.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/loopkit/g7sim/suspend"
)

func main() {
	socket := flag.String("socket", "/tmp/impossible.sock", "the socket ImpossiBLE connects the app to")
	simulator := flag.String("simulator", "", "the app's simulator (name or UDID), for -suspend")
	suspendApp := flag.Bool("suspend", false, "stop the app while iOS would have it suspended, waking it for Bluetooth events")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [-socket path] [-suspend -simulator name] provider.sock...\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	var s suspender
	if *suspendApp {
		if *simulator == "" {
			log.Fatalf("-suspend needs -simulator")
		}
		s = suspend.New(*simulator)
	}
	if c, err := net.Dial("unix", *socket); err == nil {
		c.Close()
		log.Fatalf("%s is served by another provider; quit it (ImpossiBLE's menu bar app?) first", *socket)
	}
	os.Remove(*socket)
	l, err := net.Listen("unix", *socket)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s for %v", *socket, flag.Args())
	m := newMux(flag.Args(), s)
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go m.serve(conn)
	}
}

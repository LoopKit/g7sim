package g7

// Char names the CGM service characteristics a session talks on.
type Char int

const (
	CharAuth Char = iota
	CharControl
	CharBackfill
	CharCertificate
)

func (c Char) String() string {
	return [...]string{"auth", "control", "backfill", "cert"}[c]
}

// Link is one central's connection, as the BLE layer provides it.
type Link interface {
	Addr() string
	Notify(c Char, b []byte) error
	Close()
}

// Handler is the sensor's side of one connection. The BLE layer calls it
// from that connection's own goroutine, in the order the central's writes
// arrived, so an implementation needs no locking of its own.
type Handler interface {
	Write(c Char, data []byte)
	Subscribed(c Char)
	Closed()
}

// NewHandler builds the handler for a new link. mode is what the sensor was
// advertising when the link came up. Tests override it to install a stub.
var NewHandler = newHandler

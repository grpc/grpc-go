package grpc

import (
	"time"
)

// Picker picks a Conn for RPC requests.
// This is EXPERIMENTAL and Please do not implement your own Picker for now.
type Picker interface {
	// Pick returns the Conn to use for the upcoming RPC. It may return different
	// Conn's up to the implementation.
	Pick() (*Conn, error)
	State() ConnectivityState
	WaitForStateChange(timeout time.Duration, sourceState ConnectivityState) bool
	// Close closes all the Conn's owned by this Picker.
	Close() error
}

func newUnicastPicker(target string, dopts dialOptions) (Picker, error) {
	c, err := NewConn(target, dopts)
	if err != nil {
		return nil, err
	}
	return &unicastPicker{
		conn: c,
	}, nil
}

// unicastPicker is the default Picker which is used when there is no custom Picker
// specified by users. It always picks the same Conn.
type unicastPicker struct {
	conn *Conn
}

func (p *unicastPicker) Pick() (*Conn, error) {
	return p.conn, nil
}

func (p *unicastPicker) State() ConnectivityState {
	return p.conn.State()
}

func (p *unicastPicker) WaitForStateChange(timeout time.Duration, sourceState ConnectivityState) bool {
	return p.conn.WaitForStateChange(timeout, sourceState)
}

func (p *unicastPicker) Close() error {
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

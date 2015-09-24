package grpc

// Picker picks a Conn for RPC requests.
// This is EXPERIMENTAL and Please do not implement your own Picker for now.
type Picker interface {
	// Pick returns the Conn to use for the upcoming RPC. It may return different
	// Conn's up to the implementation.
	Pick() (*Conn, error)
	// Peek returns the Conn use use for the next upcoming RPC. It returns the same
	// Conn until next time Pick gets invoked.
	Peek() *Conn
	// Close closes all the Conn's owned by this Picker.
	Close() error
}

func newSimplePicker(target string, dopts dialOptions) (Picker, error) {
	c, err := NewConn(target, dopts)
	if err != nil {
		return nil, err
	}
	return &simplePicker{
		conn: c,
	}, nil
}

// simplePicker is default Picker which is used when there is no custom Picker
// specified by users. It always picks the same Conn.
type simplePicker struct {
	conn *Conn
}

func (p *simplePicker) Pick() (*Conn, error) {
	return p.conn, nil
}

func (p *simplePicker) Peek() *Conn {
	return p.conn
}

func (p *simplePicker) Close() error {
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

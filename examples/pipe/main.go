package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
)

type memAddr string

func (a memAddr) Network() string { return "mem" }
func (a memAddr) String() string  { return string(a) }

type memListener struct {
	c chan net.Conn
}

func (ln *memListener) Accept() (net.Conn, error) {
	conn, ok := <-ln.c
	if !ok {
		return nil, fmt.Errorf("closed")
	}
	return conn, nil
}

func (ln *memListener) Addr() net.Addr {
	return memAddr(fmt.Sprintf("%p", ln))
}

func (ln *memListener) Close() error {
	close(ln.c)
	return nil
}

func main() {
	grpc.EnableTracing = true

	ln := &memListener{
		c: make(chan net.Conn, 1),
	}

	go func() {
		s := grpc.NewServer()
		log.Fatal(s.Serve(ln))
	}()

	log.Printf("Dialing to server over a synchronous pipe...")
	serverConn, err := grpc.Dial("inmemory",
		grpc.WithInsecure(),
		grpc.WithBlock(),
		grpc.WithDialer(func(_ string, _ time.Duration) (net.Conn, error) {
			c1, c2 := net.Pipe()
			log.Printf("Pipe created: %v %v", c1, c2)
			ln.c <- c2
			log.Printf("Pipe accepted: %v %v", c1, c2)
			return c1, nil
		}))
	if err != nil {
		log.Fatal(err)
	}

	// BUG: never reached
	log.Printf("SUCCESS! Connected to server: %v", serverConn)
}

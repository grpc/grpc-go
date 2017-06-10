package transport

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"golang.org/x/net/context"
)

const (
	accuracy = 1000 // Unit: bytes.
)

func TestBDP(t *testing.T) {
	testBDP(t, time.Millisecond*5, 2, 1<<20, time.Second*2)
}

func testBDP(t *testing.T, latency time.Duration, kbps, messageSize int, duration time.Duration) {
	fmt.Println("latency:", latency)
	fmt.Println("kbps:", kbps)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	var (
		sconn    net.Conn
		serverTr ServerTransport
	)
	writeDone := make(chan struct{})
	defer func() {
		if sconn != nil {
			sconn.Close()
		}
	}()
	// Launch server.
	go func() {
		var er error
		sconn, er = lis.Accept()
		if er != nil {
			t.Errorf("Server failed to accept. Err: %v", er)
			return
		}
		serverTr, er = NewServerTransport("http2", sconn, &ServerConfig{})
		if er != nil {
			t.Errorf("server failed to create transport. Err: %v", er)
			return
		}
		serverTr.HandleStreams(func(stream *Stream) {
			// Launch server reader.
			read(t, stream, messageSize)
			msg := make([]byte, messageSize)
			opts := &Options{}
			// Launch server writer.
			go func() {
				for {
					select {
					case <-writeDone:
						if err := serverTr.WriteStatus(stream, status.New(codes.OK, "")); err != nil {
							t.Errorf("Server failed to write status. Err: %v", err)
							return
						}
					default:
						if err := serverTr.Write(stream, msg, opts); err != nil {
							t.Errorf("Server failed to write. Err: %v", err)
							return
						}
					}
				}
			}()
		}, func(ctx context.Context, _ string) context.Context {
			return ctx
		})
	}()

	ctx, _ := context.WithTimeout(context.Background(), time.Second)
	clientTr, err := NewClientTransport(ctx, TargetInfo{Addr: lis.Addr().String()}, ConnectOptions{})
	if err != nil {
		t.Fatalf("Client failed to create transport. Err: %v", err)
	}
	defer clientTr.Close()

	ct := clientTr.(*http2Client)
	waitWhileTrue(t, func() (bool, error) {
		if serverTr == nil {
			return true, fmt.Errorf("timed-out waiting for server transport to initialize")
		}
		return false, nil
	})
	st := serverTr.(*http2Server)

	cstream, err := clientTr.NewStream(context.Background(), &CallHdr{Flush: true})
	if err != nil {
		t.Fatalf("Client failed to create stream. Err: %v", err)
	}
	var sstream *Stream
	waitWhileTrue(t, func() (bool, error) {
		st.mu.Lock()
		defer st.mu.Unlock()
		if len(st.activeStreams) == 1 {
			for _, v := range st.activeStreams {
				sstream = v
			}
			return false, nil
		}
		return true, fmt.Errorf("timed-out waiting for server to create a new stream")
	})

	// Launch client reader.
	read(t, cstream, messageSize)

	// Launch client writer.
	go func() {
		msg := make([]byte, messageSize)
		opts := &Options{}
		for {
			select {
			case <-writeDone:
				return
			default:
				if err := clientTr.Write(cstream, msg, opts); err != nil && err != io.EOF {
					t.Errorf("Client failed to write. Err: %v", err)
					return
				}
			}
		}
	}()

	timer := time.NewTimer(duration)
	<-timer.C
	close(writeDone)

	// Print the windows on client and server
	// after sleeping for a sec
	time.Sleep(time.Second)

	ct.fc.mu.Lock()
	fmt.Println("Client incoming connection window:", ct.fc.limit)
	ct.fc.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	sendQuota, err := wait(ctx, nil, nil, nil, st.sendQuotaPool.acquire())
	if err != nil {
		cancel()
		t.Fatalf("Error while acquiring sendQuota on server. Err: %v", err)
	}
	cancel()
	st.sendQuotaPool.add(sendQuota)
	fmt.Println("Server outgoing connection window:", sendQuota)
	fmt.Println()

	st.fc.mu.Lock()
	fmt.Println("Server incoming connection window:", st.fc.limit)
	st.fc.mu.Unlock()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	sendQuota, err = wait(ctx, nil, nil, nil, ct.sendQuotaPool.acquire())
	if err != nil {
		cancel()
		t.Fatalf("Error acquiring sendQota on client. Err: %v", err)
	}
	cancel()
	ct.sendQuotaPool.add(sendQuota)
	fmt.Println("Client outgoing conneciton window:", sendQuota)
	fmt.Println()

	cstream.fc.mu.Lock()
	fmt.Println("Client incoming stream window:", cstream.fc.limit)
	cstream.fc.mu.Unlock()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	sendQuota, err = wait(ctx, nil, nil, nil, sstream.sendQuotaPool.acquire())
	if err != nil {
		cancel()
		t.Fatalf("Error acquiring sendQuota on server stream. Err: %v", err)
	}
	cancel()
	sstream.sendQuotaPool.add(sendQuota)
	fmt.Println("Server outgoing stream window:", sendQuota)
	fmt.Println()

	sstream.fc.mu.Lock()
	fmt.Println("Server incoming stream window:", sstream.fc.limit)
	sstream.fc.mu.Unlock()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	sendQuota, err = wait(ctx, nil, nil, nil, cstream.sendQuotaPool.acquire())
	if err != nil {
		cancel()
		t.Fatalf("Error while acquiring sendQuota on client stream. Err: %v", err)
	}
	cancel()
	cstream.sendQuotaPool.add(sendQuota)
	fmt.Println("Client outgoing stream window:", sendQuota)
}

func read(t *testing.T, s *Stream, messageSize int) {
	buff := make([]byte, messageSize)
	go func() {
		for {
			if _, err := s.Read(buff); err != nil {
				if err == io.EOF {
					return
				}
				if err, ok := err.(StreamError); ok {
					if err.Code == codes.Canceled {
						return
					}
				}
				t.Errorf("Error while reading. Err: %v", err)
				return
			}
		}
	}()
}

# Flow Control

Flow control is a feature in gRPC that prevents senders from writing more data
on a stream than a receiver is capable of handling.  This feature behaves the
same for both clients and servers.  Because gRPC-Go uses a blocking-style API
for stream operations, flow control pushback is implemented by simply blocking
send operations on a stream when that stream's flow control limits have been
reached.  When the receiver has read enough data from the stream, the send
operation will unblock automatically.  Flow control is configured automatically
based on a connection's Bandwidth Delay Product (BDP) to ensure the buffer is
the minimum size necessary to allow for maximum throughput on the stream if the
receiver is reading at its maximum speed.

## Try it

```
go run ./server
```

```
go run ./client
```

## Example explanation

The example client and server are written to demonstrate the blocking by
intentionally sending messages while the other side is not receiving.  The
bidirectional echo stream in the example begins by having the client send
messages until it detects it has blocked (utilizing another goroutine).  The
server sleeps for 2 seconds to allow this to occur.  Then the server will read
all of these messages, and the roles of the client and server are swapped so the
server attempts to send continuously while the client sleeps.  After the client
sleeps for 2 seconds, it will read again to unblock the server.  The server will
detect that it has blocked, and end the stream once it has unblocked.

### Expected Output

The client output should look like:
```
2023/09/19 15:49:49 New stream began.
2023/09/19 15:49:50 Sending is blocked.
2023/09/19 15:49:51 Sent 25 messages.
2023/09/19 15:49:53 Read 25 messages.
2023/09/19 15:49:53 Stream ended successfully.
```

while the server should output the following logs:

```
2023/09/19 15:49:49 New stream began.
2023/09/19 15:49:51 Read 25 messages.
2023/09/19 15:49:52 Sending is blocked.
2023/09/19 15:49:53 Sent 25 messages.
2023/09/19 15:49:53 Stream ended successfully.
```

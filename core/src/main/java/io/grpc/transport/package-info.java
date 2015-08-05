/**
 * The transport layer that does the heavy lifting of putting and taking bytes off the wire.
 *
 * <p>Note the transport layer API is considered internal to gRPC and has weaker API guarantees than
 * the core API under package {@code io.grpc}.
 *
 * <p>The interfaces to it are abstract just enough to allow plugging in of different
 * implementations.  Transports are modeled as {@code Stream} factories. The variation in interface
 * between a server stream and a client stream exists to codify their differing semantics for
 * cancellation and error reporting.
 */
package io.grpc.transport;

package com.google.net.stubby.newtransport;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

/**
 * Extension of {@link Stream} to support server-side termination semantics.
 */
public interface ServerStream extends Stream {

  /**
   * Closes the local side of this stream. A status code of
   * {@link com.google.net.stubby.transport.Transport.Code#OK} implies normal termination of the
   * local side of the stream (i.e. half-closed). Any other value implies abnormal termination.
   *
   * @param status details for the closure of the local-side of this stream.
   * @param trailers an additional block of headers to pass to the client on stream closure.
   */
  void close(Status status, Metadata.Trailers trailers);
}

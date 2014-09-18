package com.google.net.stubby.newtransport;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

/**
 * Extension of {@link Stream} to support server-side termination semantics.
 */
public interface ServerStream extends Stream {

  /**
   * Closes the stream for both reading and writing. A status code of
   * {@link com.google.net.stubby.transport.Transport.Code#OK} implies normal termination of the
   * stream. Any other value implies abnormal termination.
   *
   * @param status details of the closure
   * @param trailers an additional block of headers to pass to the client on stream closure.
   */
  void close(Status status, Metadata.Trailers trailers);
}

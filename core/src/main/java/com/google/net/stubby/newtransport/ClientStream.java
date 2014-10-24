package com.google.net.stubby.newtransport;

/**
 * Extension of {@link Stream} to support client-side termination semantics.
 */
public interface ClientStream extends Stream {

  /**
   * Used to abnormally terminate the stream. After calling this method, no further messages will be
   * sent or received, however it may still be possible to receive buffered messages for a brief
   * period until {@link ClientStreamListener#closed} is called.
   */
  void cancel();

  /**
   * Closes the local side of this stream and flushes any remaining messages. After this is called,
   * no further messages may be sent on this stream, but additional messages may be received until
   * the remote end-point is closed.
   */
  void halfClose();

}

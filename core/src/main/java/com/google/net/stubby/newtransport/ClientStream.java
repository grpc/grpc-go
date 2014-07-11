package com.google.net.stubby.newtransport;

/**
 * Extension of {@link Stream} to support client-side termination semantics.
 */
public interface ClientStream extends Stream {

  /**
   * Used to abnormally terminate the stream. After calling this method, no further messages will be
   * sent or received, however it may still be possible to receive buffered messages for a brief
   * period until {@link StreamListener#closed} is called.
   */
  void cancel();
}

package com.google.net.stubby.newtransport;


/**
 * Extension of {@link Stream} to support client-side termination semantics.
 */
public interface ClientStream extends Stream<ClientStream> {

  /**
   * Used to abnormally terminate the stream. Any internally buffered messages are dropped. After
   * this is called, no further messages may be sent and no further {@link StreamListener} callbacks
   * (with the exception of onClosed) will be invoked for this stream. Any frames received for this
   * stream after returning from this method will be discarded.
   */
  void cancel();
}

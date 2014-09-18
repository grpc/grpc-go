package com.google.net.stubby.newtransport;

import com.google.net.stubby.Status;

/** An observer of server-side stream events. */
public interface ServerStreamListener extends StreamListener {
  /**
   * Called when the remote side of the transport gracefully closed, indicating the client had no
   * more data to send. No further messages will be received on the stream.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   */
  void halfClosed();
}

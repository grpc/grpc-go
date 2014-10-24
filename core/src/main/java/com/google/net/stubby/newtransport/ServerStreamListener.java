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

  /**
   * Called when the stream is fully closed. A status code of {@link
   * com.google.net.stubby.Status.Code#OK} implies normal termination of the stream.
   * Any other value implies abnormal termination. Since clients cannot send status, the passed
   * status is always library-generated and only is concerned with transport-level stream shutdown
   * (the call itself may have had a failing status, but if the stream terminated cleanly with the
   * status appearing to have been sent, then the passed status here would be OK). This is
   * guaranteed to always be the final call on a listener. No further callbacks will be issued.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param status details about the remote closure
   */
  void closed(Status status);
}

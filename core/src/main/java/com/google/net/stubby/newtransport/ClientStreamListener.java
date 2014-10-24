package com.google.net.stubby.newtransport;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;

import javax.annotation.Nullable;

/** An observer of client-side stream events. */
public interface ClientStreamListener extends StreamListener {
  /**
   * Called upon receiving all header information from the remote end-point.
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param headers the fully buffered received headers.
   * @return a processing completion future, or {@code null} to indicate that processing of the
   *         headers is immediately complete.
   */
  @Nullable
  ListenableFuture<Void> headersRead(Metadata.Headers headers);

  /**
   * Called when the stream is fully closed. {@link
   * com.google.net.stubby.Status.Code#OK} is the only status code that is guaranteed
   * to have been sent from the remote server. Any other status code may have been caused by
   * abnormal stream termination. This is guaranteed to always be the final call on a listener. No
   * further callbacks will be issued.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param status details about the remote closure
   * @param trailers trailing metadata
   */
  void closed(Status status, Metadata.Trailers trailers);
}

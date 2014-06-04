package com.google.net.stubby.newtransport;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.net.stubby.transport.Transport.Status;

import java.io.InputStream;

/**
 * An observer of {@link Stream} events. It is guaranteed to only have one concurrent callback at a
 * time.
 */
public interface StreamListener {

  /**
   * Called upon receiving context information from the remote end-point. The {@link InputStream} is
   * non-blocking and contains the entire context.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param name the unique name of the context
   * @param value the value of the context.
   * @param length the length of the value {@link InputStream}.
   * @return a future that can be observed by flow control to determine when the context has been
   *         processed by the application. If {@code null}, processing of this context is assumed to
   *         be complete upon returning from this method.
   */
  ListenableFuture<Void> contextRead(String name, InputStream value, int length);

  /**
   * Called upon receiving a message from the remote end-point. The {@link InputStream} is
   * non-blocking and contains the entire message.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param message the bytes of the message.
   * @param length the length of the message {@link InputStream}.
   * @return a future that can be observed by flow control to determine when the message has been
   *         processed by the application. If {@code null}, processing of this message is assumed to
   *         be complete upon returning from this method.
   */
  ListenableFuture<Void> messageRead(InputStream message, int length);

  /**
   * Called when the remote side of the transport closed. A status code of
   * {@link com.google.net.stubby.transport.Transport.Code#OK} implies normal termination of the
   * remote side of the stream (i.e. half-closed). Any other value implies abnormal termination. If
   * the remote end-point was abnormally terminated, no further messages will be received on the
   * stream.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param status details of the remote stream closure.
   */
  void closed(Status status);
}

package com.google.net.stubby.transport;

import com.google.common.util.concurrent.ListenableFuture;

import java.io.InputStream;

import javax.annotation.Nullable;

/**
 * An observer of {@link Stream} events. It is guaranteed to only have one concurrent callback at a
 * time.
 */
public interface StreamListener {
  /**
   * Called upon receiving a message from the remote end-point. The {@link InputStream} is
   * non-blocking and contains the entire message.
   *
   * <p>The method optionally returns a future that can be observed by flow control to determine
   * when the message has been processed by the application. If {@code null} is returned, processing
   * of this message is assumed to be complete upon returning from this method.
   *
   * <p>The {@code message} {@link InputStream} will be closed when the returned future completes.
   * If no future is returned, the stream will be closed immediately after returning from this
   * method.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param message the bytes of the message.
   * @param length the length of the message {@link InputStream}.
   * @return a processing completion future, or {@code null} to indicate that processing of the
   *         message is immediately complete.
   */
  @Nullable
  ListenableFuture<Void> messageRead(InputStream message, int length);
}

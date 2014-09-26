package com.google.net.stubby;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

import javax.annotation.Nullable;

/**
 * Extension to {@link InputStream} to allow for deferred production of data. Allows for
 * zero-copy conversions when the goal is to copy the contents of a resource to a
 * stream or buffer.
 */
public abstract class DeferredInputStream<T> extends InputStream {

  /**
   * Produce the entire contents of this stream to the specified target
   *
   * @return number of bytes written
   */
  public abstract int flushTo(OutputStream target) throws IOException;

  /**
   *  Returns the object that backs the stream. If any bytes have been read from the stream
   *  then {@code null} is returned.
   */
  @Nullable
  public abstract T getDeferred();
}

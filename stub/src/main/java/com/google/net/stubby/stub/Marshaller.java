package com.google.net.stubby.stub;

import java.io.InputStream;

/**
 * An typed abstraction over message serialization.
 */
public interface Marshaller<T> {

  /**
   * Given a message produce an {@link InputStream} for it.
   */
  // TODO(user): Switch to ByteSource equivalent when ready
  public InputStream stream(T value);

  /**
   * Given an {@link InputStream} parse it into an instance of the declared type.
   */
  public T parse(InputStream stream);
}

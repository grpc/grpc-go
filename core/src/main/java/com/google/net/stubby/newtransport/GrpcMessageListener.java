package com.google.net.stubby.newtransport;

import com.google.net.stubby.Status;

import java.io.InputStream;

/**
 * A listener of GRPC messages.
 */
public interface GrpcMessageListener {
  /**
   * Provides a GRPC context frame to this listener.
   *
   * @param name the unique name identifying the type of context element stored in {@code value}.
   * @param value the serialized value of the context element. Ownership of this {@link InputStream}
   *        is transferred to this {@link GrpcMessageListener}, which must close the stream when it
   *        is no longer needed via {@link InputStream#close}.
   * @param length the number of bytes in the {@link InputStream}.
   */
  void onContext(String name, InputStream value, int length);

  /**
   * Provides a GRPC payload frame to this listener.
   *
   * @param payload the payload data. Ownership of this {@link InputStream} is transferred to this
   *        {@link GrpcMessageListener}, which must close the stream when it is no longer needed via
   *        {@link InputStream#close}.
   * @param length the number of bytes in the {@link InputStream}.
   */
  void onPayload(InputStream payload, int length);

  /**
   * Provides a GRPC status frame to this listener.
   *
   * @param status a status for the end of this stream.
   */
  void onStatus(Status status);
}

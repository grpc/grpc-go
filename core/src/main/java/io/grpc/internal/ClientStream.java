/*
 * Copyright 2014, gRPC Authors All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.internal;

import io.grpc.Attributes;
import io.grpc.Status;

/**
 * Extension of {@link Stream} to support client-side termination semantics.
 *
 * <p>An implementation doesn't need to be thread-safe. All methods are expected to execute quickly.
 */
public interface ClientStream extends Stream {

  /**
   * Abnormally terminates the stream. After calling this method, no further messages will be
   * sent or received, however it may still be possible to receive buffered messages for a brief
   * period until {@link ClientStreamListener#closed} is called. This method may only be called
   * after {@link #start}, but else is safe to be called at any time and multiple times and
   * from any thread.
   *
   * @param reason must be non-OK
   */
  void cancel(Status reason);

  /**
   * Closes the local side of this stream and flushes any remaining messages. After this is called,
   * no further messages may be sent on this stream, but additional messages may be received until
   * the remote end-point is closed. This method may only be called once, and only after
   * {@link #start}.
   */
  void halfClose();

  /**
   * Override the default authority with {@code authority}. May only be called before {@link
   * #start}.
   */
  void setAuthority(String authority);

  /**
   * Starts stream. This method may only be called once.  It is safe to do latent initialization of
   * the stream up until {@link #start} is called.
   *
   * <p>This method should not throw any exceptions.
   *
   * @param listener non-{@code null} listener of stream events
   */
  void start(ClientStreamListener listener);

  /**
   * Sets the max size accepted from the remote endpoint.
   */
  void setMaxInboundMessageSize(int maxSize);

  /**
   * Sets the max size sent to the remote endpoint.
   */
  void setMaxOutboundMessageSize(int maxSize);

  /**
   * Attributes that the stream holds at the current moment.
   */
  Attributes getAttributes();
}

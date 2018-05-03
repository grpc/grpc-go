/*
 * Copyright 2014 The gRPC Authors
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
import io.grpc.Decompressor;
import io.grpc.Metadata;
import io.grpc.Status;
import javax.annotation.Nullable;

/**
 * Extension of {@link Stream} to support server-side termination semantics.
 *
 * <p>An implementation doesn't need to be thread-safe. All methods are expected to execute quickly.
 */
public interface ServerStream extends Stream {

  /**
   * Writes custom metadata as headers on the response stream sent to the client. This method may
   * only be called once and cannot be called after calls to {@link Stream#writeMessage}
   * or {@link #close}.
   *
   * @param headers to send to client.
   */
  void writeHeaders(Metadata headers);

  /**
   * Closes the stream for both reading and writing. A status code of
   * {@link io.grpc.Status.Code#OK} implies normal termination of the
   * stream. Any other value implies abnormal termination.
   *
   * <p>Attempts to read from or write to the stream after closing
   * should be ignored by implementations, and should not throw
   * exceptions.
   *
   * @param status details of the closure
   * @param trailers an additional block of metadata to pass to the client on stream closure.
   */
  void close(Status status, Metadata trailers);


  /**
   * Tears down the stream, typically in the event of a timeout. This method may be called multiple
   * times and from any thread.
   */
  void cancel(Status status);

  /**
   * Sets the decompressor on the deframer. If the transport does not support compression, this may
   * do nothing.
   *
   * @param decompressor the decompressor to use.
   */
  void setDecompressor(Decompressor decompressor);

  /**
   * Attributes describing stream.  This is inherited from the transport attributes, and used
   * as the basis of {@link io.grpc.ServerCall#getAttributes}.
   *
   * @return Attributes container
   */
  Attributes getAttributes();

  /**
   * Gets the authority this stream is addressed to.
   * @return the authority string. {@code null} if not available.
   */
  @Nullable
  String getAuthority();

  /**
   * Sets the server stream listener.
   */
  void setListener(ServerStreamListener serverStreamListener);

  /**
   * The context for recording stats and traces for this stream.
   */
  StatsTraceContext statsTraceContext();
}

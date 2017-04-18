/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.internal;

import io.grpc.Attributes;
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

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

package io.grpc.transport;

import io.grpc.Metadata;
import io.grpc.Status;

/** An observer of client-side stream events. */
public interface ClientStreamListener extends StreamListener {
  /**
   * Called upon receiving all header information from the remote end-point. Note that transports
   * are not required to call this method if no header information is received, this would occur
   * when a stream immediately terminates with an error and only
   * {@link #closed(io.grpc.Status, Metadata.Trailers)} is called.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param headers the fully buffered received headers.
   */
  void headersRead(Metadata.Headers headers);

  /**
   * Called when the stream is fully closed. {@link
   * io.grpc.Status.Code#OK} is the only status code that is guaranteed
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

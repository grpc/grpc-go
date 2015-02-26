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

import io.grpc.Status;

/** An observer of server-side stream events. */
public interface ServerStreamListener extends StreamListener {
  /**
   * Called when the remote side of the transport gracefully closed, indicating the client had no
   * more data to send. No further messages will be received on the stream.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   */
  void halfClosed();

  /**
   * Called when the stream is fully closed. A status code of {@link
   * io.grpc.Status.Code#OK} implies normal termination of the stream.
   * Any other value implies abnormal termination. Since clients cannot send status, the passed
   * status is always library-generated and only is concerned with transport-level stream shutdown
   * (the call itself may have had a failing status, but if the stream terminated cleanly with the
   * status appearing to have been sent, then the passed status here would be {@code OK}). This is
   * guaranteed to always be the final call on a listener. No further callbacks will be issued.
   *
   * <p>This method should return quickly, as the same thread may be used to process other streams.
   *
   * @param status details about the remote closure
   */
  void closed(Status status);
}

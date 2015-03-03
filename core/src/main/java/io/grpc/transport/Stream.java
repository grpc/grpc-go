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

import java.io.InputStream;

import javax.annotation.Nullable;

/**
 * A single stream of communication between two end-points within a transport.
 *
 * <p>An implementation doesn't need to be thread-safe.
 */
public interface Stream {
  /**
   * Requests up to the given number of messages from the call to be delivered to
   * {@link StreamListener#messageRead(java.io.InputStream)}. No additional messages will be
   * delivered.
   *
   * @param numMessages the requested number of messages to be delivered to the listener.
   */
  void request(int numMessages);

  /**
   * Writes a message payload to the remote end-point. The bytes from the stream are immediate read
   * by the Transport. This method will always return immediately and will not wait for the write to
   * complete.
   *
   * <p>When the write is "accepted" by the transport, the given callback (if provided) will be
   * called. The definition of what it means to be "accepted" is up to the transport implementation,
   * but this is a general indication that the transport is capable of handling more out-bound data
   * on the stream. If the stream/connection is closed for any reason before the write could be
   * accepted, the callback will never be invoked.
   *
   * @param message stream containing the serialized message to be sent
   * @param length the length of the {@link InputStream}.
   * @param accepted an optional callback for when the transport has accepted the write.
   */
  void writeMessage(InputStream message, int length, @Nullable Runnable accepted);

  /**
   * Flushes any internally buffered messages to the remote end-point.
   */
  void flush();
}

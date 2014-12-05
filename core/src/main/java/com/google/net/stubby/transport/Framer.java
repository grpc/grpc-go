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

package com.google.net.stubby.transport;

import com.google.net.stubby.Status;

import java.io.InputStream;

/**
 * Implementations produce the GRPC byte sequence and then split it over multiple frames to be
 * delivered via the transport layer which implements {@link Framer.Sink}
 */
public interface Framer {

  /**
   * Sink implemented by the transport layer to receive frames and forward them to their destination
   */
  public interface Sink<T> {
    /**
     * Deliver a frame via the transport.
     *
     * @param frame the contents of the frame to deliver
     * @param endOfStream whether the frame is the last one for the GRPC stream
     */
    public void deliverFrame(T frame, boolean endOfStream);
  }

  /**
   * Write out a Payload message. {@code payload} will be completely consumed.
   * {@code payload.available()} must return the number of remaining bytes to be read.
   */
  public void writePayload(InputStream payload, int length);

  /**
   * Write out a Status message.
   */
  // TODO(user): change this signature when we actually start writing out the complete Status.
  public void writeStatus(Status status);

  /**
   * Flush any buffered data in the framer to the sink.
   */
  public void flush();

  /**
   * Indicates whether or not this {@link Framer} has been closed via a call to either
   * {@link #close()} or {@link #dispose()}.
   */
  public boolean isClosed();

  /**
   * Flushes and closes the framer and releases any buffers. After the {@link Framer} is closed or
   * disposed, additional calls to this method will have no affect.
   */
  public void close();

  /**
   * Closes the framer and releases any buffers, but does not flush. After the {@link Framer} is
   * closed or disposed, additional calls to this method will have no affect.
   */
  public void dispose();
}

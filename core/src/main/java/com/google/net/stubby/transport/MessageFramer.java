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

import com.google.net.stubby.GrpcFramingUtil;
import com.google.net.stubby.Status;

import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;

/**
 * Default {@link Framer} implementation.
 */
public class MessageFramer implements Framer {

  private CompressionFramer framer;
  private final ByteBuffer scratch = ByteBuffer.allocate(16);

  public MessageFramer(Sink<ByteBuffer> sink, int maxFrameSize) {
    // TODO(user): maxFrameSize should probably come from a 'Platform' class
    framer = new CompressionFramer(sink, maxFrameSize, false, maxFrameSize / 16);
  }

  /**
   * Sets whether compression is encouraged.
   */
  public void setAllowCompression(boolean enable) {
    verifyNotClosed();
    framer.setAllowCompression(enable);
  }

  @Override
  public void writePayload(InputStream message, int messageLength) {
    verifyNotClosed();
    try {
      scratch.clear();
      scratch.put(GrpcFramingUtil.PAYLOAD_FRAME);
      scratch.putInt(messageLength);
      framer.write(scratch.array(), 0, scratch.position());
      if (messageLength != framer.write(message)) {
        throw new RuntimeException("Message length was inaccurate");
      }
      framer.endOfMessage();
    } catch (IOException ioe) {
      throw new RuntimeException(ioe);
    }
  }


  @Override
  public void writeStatus(Status status) {
    verifyNotClosed();
    short code = (short) status.getCode().value();
    scratch.clear();
    scratch.put(GrpcFramingUtil.STATUS_FRAME);
    int length = 2;
    scratch.putInt(length);
    scratch.putShort(code);
    framer.write(scratch.array(), 0, scratch.position());
    framer.endOfMessage();
  }

  @Override
  public void flush() {
    verifyNotClosed();
    framer.flush();
  }

  @Override
  public boolean isClosed() {
    return framer == null;
  }

  @Override
  public void close() {
    if (!isClosed()) {
      // TODO(user): Returning buffer to a pool would go here
      framer.close();
      framer = null;
    }
  }

  @Override
  public void dispose() {
    // TODO(user): Returning buffer to a pool would go here
    framer = null;
  }

  private void verifyNotClosed() {
    if (isClosed()) {
      throw new IllegalStateException("Framer already closed");
    }
  }
}

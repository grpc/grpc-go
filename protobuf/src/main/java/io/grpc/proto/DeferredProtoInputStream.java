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

package io.grpc.proto;

import com.google.common.io.ByteStreams;
import com.google.protobuf.CodedOutputStream;
import com.google.protobuf.MessageLite;

import io.grpc.DeferredInputStream;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.OutputStream;

import javax.annotation.Nullable;

/**
 * Implementation of {@link io.grpc.DeferredInputStream} backed by a protobuf.
 */
public class DeferredProtoInputStream extends DeferredInputStream<MessageLite> {

  // DeferredProtoInputStream is first initialized with a *message*. *partial* is initially null.
  // Once there has been a read operation on this stream, *message* is serialized to *partial* and
  // set to null.
  @Nullable private MessageLite message;
  @Nullable private ByteArrayInputStream partial;

  public DeferredProtoInputStream(MessageLite message) {
    this.message = message;
  }

  /**
   * Returns the original protobuf message. Returns null after this stream has been read.
   */
  @Override
  @Nullable
  public MessageLite getDeferred() {
    return message;
  }

  @Override
  public int flushTo(OutputStream target) throws IOException {
    int written;
    if (message != null) {
      written = message.getSerializedSize();
      message.writeTo(target);
      message = null;
    } else {
      written = (int) ByteStreams.copy(partial, target);
      partial = null;
    }
    return written;
  }

  @Override
  public int read() throws IOException {
    if (message != null) {
      partial = new ByteArrayInputStream(message.toByteArray());
      message = null;
    }
    if (partial != null) {
      return partial.read();
    }
    return -1;
  }

  @Override
  public int read(byte[] b, int off, int len) throws IOException {
    if (message != null) {
      int size = message.getSerializedSize();
      if (size == 0) {
        message = null;
        partial = null;
        return -1;
      }
      if (len >= size) {
        // This is the only case that is zero-copy.
        CodedOutputStream stream = CodedOutputStream.newInstance(b, off, size);
        message.writeTo(stream);
        stream.flush();
        stream.checkNoSpaceLeft();

        message = null;
        partial = null;
        return size;
      }

      partial = new ByteArrayInputStream(message.toByteArray());
      message = null;
    }
    if (partial != null) {
      return partial.read(b, off, len);
    }
    return -1;
  }

  @Override
  public int available() throws IOException {
    if (message != null) {
      return message.getSerializedSize();
    } else if (partial != null) {
      return partial.available();
    }
    return 0;
  }
}

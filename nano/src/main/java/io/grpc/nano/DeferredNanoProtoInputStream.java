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

package io.grpc.nano;

import com.google.common.io.ByteStreams;
import com.google.protobuf.nano.CodedOutputByteBufferNano;
import com.google.protobuf.nano.MessageNano;

import io.grpc.DeferredInputStream;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.OutputStream;

import javax.annotation.Nullable;

/**
 * Implementation of {@link DeferredInputStream} backed by a nano proto.
 */
public class DeferredNanoProtoInputStream extends DeferredInputStream<MessageNano> {

  // DeferredNanoProtoInputStream is first initialized with a *message*. *partial* is initially
  // null.
  // Once there has been a read operation on this stream, *message* is serialized to *partial* and
  // set to null.
  @Nullable private MessageNano message;
  @Nullable private ByteArrayInputStream partial;

  public DeferredNanoProtoInputStream(MessageNano message) {
    this.message = message;
  }

  private void toPartial() {
    if (message != null) {
      partial = new ByteArrayInputStream(MessageNano.toByteArray(message));
      message = null;
    }
  }

  @Override
  public int flushTo(OutputStream target) throws IOException {
    // TODO(simonma): flushTo is an optimization of DeferredInputStream, for the implementations
    // that can write data directly to OutputStream, if we don't support flushTo (by not extending
    // DeferredInputStream), the caller will use ByteStreams.copy anyway. So consider extends
    // InputStream directly or make a real optimization here (like save the byte[] and use it for a
    // single target.write()).
    int written = 0;
    toPartial();
    if (partial != null) {
      written = (int) ByteStreams.copy(partial, target);
      partial = null;
    }
    return written;
  }

  @Override
  public int read() throws IOException {
    toPartial();
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
        CodedOutputByteBufferNano output = CodedOutputByteBufferNano.newInstance(b, off, size);
        message.writeTo(output);
        output.checkNoSpaceLeft();

        message = null;
        partial = null;
        return size;
      }

      toPartial();
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

  @Override
  @Nullable
  public MessageNano getDeferred() {
    return message;
  }
}

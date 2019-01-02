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

package io.grpc.protobuf.lite;

import com.google.protobuf.CodedOutputStream;
import com.google.protobuf.MessageLite;
import com.google.protobuf.Parser;
import io.grpc.Drainable;
import io.grpc.KnownLength;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import javax.annotation.Nullable;

/**
 * An {@link InputStream} backed by a protobuf.
 */
final class ProtoInputStream extends InputStream implements Drainable, KnownLength {

  // ProtoInputStream is first initialized with a *message*. *partial* is initially null.
  // Once there has been a read operation on this stream, *message* is serialized to *partial* and
  // set to null.
  @Nullable private MessageLite message;
  private final Parser<?> parser;
  @Nullable private ByteArrayInputStream partial;

  ProtoInputStream(MessageLite message, Parser<?> parser) {
    this.message = message;
    this.parser = parser;
  }

  @Override
  public int drainTo(OutputStream target) throws IOException {
    int written;
    if (message != null) {
      written = message.getSerializedSize();
      message.writeTo(target);
      message = null;
    } else if (partial != null) {
      written = (int) ProtoLiteUtils.copy(partial, target);
      partial = null;
    } else {
      written = 0;
    }
    return written;
  }

  @Override
  public int read() {
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
  public int available() {
    if (message != null) {
      return message.getSerializedSize();
    } else if (partial != null) {
      return partial.available();
    }
    return 0;
  }

  MessageLite message() {
    if (message == null) {
      throw new IllegalStateException("message not available");
    }
    return message;
  }

  Parser<?> parser() {
    return parser;
  }
}

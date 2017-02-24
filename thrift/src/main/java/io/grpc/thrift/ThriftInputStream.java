/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.thrift;

import com.google.common.io.ByteStreams;
import io.grpc.Drainable;
import io.grpc.KnownLength;
import io.grpc.Status;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import javax.annotation.Nullable;
import org.apache.thrift.TBase;
import org.apache.thrift.TException;
import org.apache.thrift.TSerializer;

/** InputStream for Thrift. */
@SuppressWarnings("InputStreamSlowMultibyteRead") // TODO(ejona): would be good to fix
final class ThriftInputStream extends InputStream implements Drainable, KnownLength {

  /**
   * ThriftInput stream is initialized with a *message* , *serializer*
   * *partial* is initially null.
   */
  @Nullable private TBase<?,?> message;
  @Nullable private ByteArrayInputStream partial;
  private final TSerializer serializer = new TSerializer();

  /** Initialize message with @param message. */
  public ThriftInputStream(TBase<?,?> message) {
    this.message = message;
  }

  @Override
  public int drainTo(OutputStream target) throws IOException {
    int written = 0;
    if (message != null) {
      try {
        byte[] bytes = serializer.serialize(message);
        written = bytes.length;
        target.write(bytes);
        message = null;
      } catch (TException e) {
        throw Status.INTERNAL.withDescription("failed to serialize thrift message")
            .withCause(e).asRuntimeException();
      }
    } else if (partial != null) {
      written = (int) ByteStreams.copy(partial, target);
      partial = null;
    } else {
      written = 0;
    }
    return written;
  }

  @Override
  public int read() throws IOException {
    if (message != null) {
      try {
        partial = new ByteArrayInputStream(serializer.serialize(message));
        message = null;
      } catch (TException e) {
        throw Status.INTERNAL.withDescription("failed to serialize thrift message")
            .withCause(e).asRuntimeException();
      }
    }
    if (partial != null) {
      return partial.read();
    }
    return -1;
  }

  @Override
  public int available() throws IOException {
    if (message != null) {
      try {
        partial = new ByteArrayInputStream(serializer.serialize(message));
        message = null;
        return partial.available();
      } catch (TException e) {
        throw Status.INTERNAL.withDescription("failed to serialize thrift message")
            .withCause(e).asRuntimeException();
      }
    } else if (partial != null) {
      return partial.available();
    }
    return 0;
  }

}

/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

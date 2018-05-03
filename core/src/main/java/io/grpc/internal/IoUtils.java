/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;

/** Common IoUtils for thrift and nanopb to convert inputstream to bytes. */
public final class IoUtils {

  /** maximum buffer to be read is 16 KB. */
  private static final int MAX_BUFFER_LENGTH = 16384;

  /** Returns the byte array. */
  public static byte[] toByteArray(InputStream in) throws IOException {
    ByteArrayOutputStream out = new ByteArrayOutputStream();
    copy(in, out);
    return out.toByteArray();
  }

  /** Copies the data from input stream to output stream. */
  public static long copy(InputStream from, OutputStream to) throws IOException {
    // Copied from guava com.google.common.io.ByteStreams because its API is unstable (beta)
    checkNotNull(from);
    checkNotNull(to);
    byte[] buf = new byte[MAX_BUFFER_LENGTH];
    long total = 0;
    while (true) {
      int r = from.read(buf);
      if (r == -1) {
        break;
      }
      to.write(buf, 0, r);
      total += r;
    }
    return total;
  }
}

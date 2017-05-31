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

package io.grpc.internal;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;

/** Common IoUtils for thrift and nanopb to convert inputstream to bytes. */
public final class IoUtils {

  /** maximum buffer to be read is 16 KB. */
  private static final int MAX_BUFFER_LENGTH = 16384; 
  
  /** Returns the byte array. */
  public static byte[] toByteArray(InputStream is) throws IOException {
    ByteArrayOutputStream buffer = new ByteArrayOutputStream();
    int nRead;
    byte[] bytes = new byte[MAX_BUFFER_LENGTH];

    while ((nRead = is.read(bytes, 0, bytes.length)) != -1) {
      buffer.write(bytes, 0, nRead);
    }

    buffer.flush();
    return buffer.toByteArray();
  }
}
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

package io.grpc;

import java.io.IOException;
import java.io.OutputStream;

/**
 * Extension to an {@link java.io.InputStream} or alike by adding a method that transfers all
 * content to an {@link OutputStream}.
 *
 * <p>This can be used for optimizing for the case where the content of the input stream will be
 * written to an {@link OutputStream} eventually. Instead of copying the content to a byte array
 * through {@code read()}, then writing the {@code OutputStream}, the implementation can write
 * the content directly to the {@code OutputStream}.
 */
public interface Drainable {

  /**
   * Transfers the entire contents of this stream to the specified target.
   *
   * @param target to write to.
   * @return number of bytes written.
   */
  int drainTo(OutputStream target) throws IOException;
}

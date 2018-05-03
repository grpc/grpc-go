/*
 * Copyright 2017 The gRPC Authors
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

import io.grpc.Compressor;
import java.io.InputStream;

/** Interface for framing gRPC messages. */
public interface Framer {
  /**
   * Writes out a payload message.
   *
   * @param message contains the message to be written out. It will be completely consumed.
   */
  void writePayload(InputStream message);

  /** Flush the buffered payload. */
  void flush();

  /** Returns whether the framer is closed. */
  boolean isClosed();

  /** Closes, with flush. */
  void close();

  /** Closes, without flush. */
  void dispose();

  /** Enable or disable compression. */
  Framer setMessageCompression(boolean enable);

  /** Set the compressor used for compression. */
  Framer setCompressor(Compressor compressor);

  /** Set a size limit for each outbound message. */ 
  void setMaxOutboundMessageSize(int maxSize);
}

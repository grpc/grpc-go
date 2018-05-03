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

package io.grpc.internal;

import io.grpc.Compressor;
import java.io.InputStream;

/**
 * A single stream of communication between two end-points within a transport.
 *
 * <p>An implementation doesn't need to be thread-safe. All methods are expected to execute quickly.
 */
public interface Stream {
  /**
   * Requests up to the given number of messages from the call to be delivered via
   * {@link StreamListener#messagesAvailable(StreamListener.MessageProducer)}. No additional
   * messages will be delivered.  If the stream has a {@code start()} method, it must be called
   * before requesting messages.
   *
   * @param numMessages the requested number of messages to be delivered to the listener.
   */
  void request(int numMessages);

  /**
   * Writes a message payload to the remote end-point. The bytes from the stream are immediately
   * read by the Transport. Where possible callers should use streams that are
   * {@link io.grpc.KnownLength} to improve efficiency. This method will always return immediately
   * and will not wait for the write to complete.  If the stream has a {@code start()} method, it
   * must be called before writing any messages.
   *
   * <p>It is recommended that the caller consult {@link #isReady()} before calling this method to
   * avoid excessive buffering in the transport.
   *
   * <p>This method takes ownership of the InputStream, and implementations are responsible for
   * calling {@link InputStream#close}.
   *
   * @param message stream containing the serialized message to be sent
   */
  void writeMessage(InputStream message);

  /**
   * Flushes any internally buffered messages to the remote end-point.
   */
  void flush();

  /**
   * If {@code true}, indicates that the transport is capable of sending additional messages without
   * requiring excessive buffering internally. Otherwise, {@link StreamListener#onReady()} will be
   * called when it turns {@code true}.
   *
   * <p>This is just a suggestion and the application is free to ignore it, however doing so may
   * result in excessive buffering within the transport.
   */
  boolean isReady();

  /**
   * Sets the compressor on the framer.
   *
   * @param compressor the compressor to use
   */
  void setCompressor(Compressor compressor);

  /**
   * Enables per-message compression, if an encoding type has been negotiated.  If no message
   * encoding has been negotiated, this is a no-op. By default per-message compression is enabled,
   * but may not have any effect if compression is not enabled on the call.
   */
  void setMessageCompression(boolean enable);
}

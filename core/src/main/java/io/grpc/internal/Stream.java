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

package io.grpc.internal;

import io.grpc.Compressor;
import io.grpc.Decompressor;

import java.io.InputStream;

/**
 * A single stream of communication between two end-points within a transport.
 *
 * <p>An implementation doesn't need to be thread-safe. All methods are expected to execute quickly.
 */
public interface Stream {
  /**
   * Requests up to the given number of messages from the call to be delivered to
   * {@link StreamListener#messageRead(java.io.InputStream)}. No additional messages will be
   * delivered.
   *
   * @param numMessages the requested number of messages to be delivered to the listener.
   */
  void request(int numMessages);

  /**
   * Writes a message payload to the remote end-point. The bytes from the stream are immediately
   * read by the Transport. Where possible callers should use streams that are
   * {@link io.grpc.KnownLength} to improve efficiency. This method will always return immediately
   * and will not wait for the write to complete.
   *
   * <p>It is recommended that the caller consult {@link #isReady()} before calling this method to
   * avoid excessive buffering in the transport.
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
   * Sets the default message encoder for messages on this stream.
   * @param c the compressor
   */
  void setCompressor(Compressor c);

  /**
   * Set the decompressor for this stream.  This may be called at most once.  Typically this is set
   * after the message encoding header is provided by the remote host, but before any messages are
   * received.
   */
  void setDecompressor(Decompressor d);

  /**
   * Looks up the decompressor by its message encoding name, and sets it for this stream.
   * Decompressors are registered with {@link io.grpc.DecompressorRegistry#register}.
   *
   * @param messageEncoding the name of the encoding provided by the remote host
   * @throws IllegalArgumentException if the provided message encoding cannot be found.
   */
  void setDecompressor(String messageEncoding);
}

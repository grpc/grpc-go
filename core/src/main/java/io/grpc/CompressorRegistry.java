/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc;

import static com.google.common.base.Preconditions.checkArgument;

import com.google.common.annotations.VisibleForTesting;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Encloses classes related to the compression and decompression of messages.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
@ThreadSafe
public final class CompressorRegistry {
  private static final CompressorRegistry DEFAULT_INSTANCE = new CompressorRegistry(
      new Codec.Gzip(),
      Codec.Identity.NONE);

  /**
   * Returns the default instance used by gRPC when the registry is not specified.
   * Currently the registry just contains support for gzip.
   */
  public static CompressorRegistry getDefaultInstance() {
    return DEFAULT_INSTANCE;
  }

  /**
   * Returns a new instance with no registered compressors.
   */
  public static CompressorRegistry newEmptyInstance() {
    return new CompressorRegistry();
  }

  private final ConcurrentMap<String, Compressor> compressors;

  @VisibleForTesting
  CompressorRegistry(Compressor ...cs) {
    compressors = new ConcurrentHashMap<String, Compressor>();
    for (Compressor c : cs) {
      compressors.put(c.getMessageEncoding(), c);
    }
  }

  @Nullable
  public Compressor lookupCompressor(String compressorName) {
    return compressors.get(compressorName);
  }

  /**
   * Registers a compressor for both decompression and message encoding negotiation.
   *
   * @param c The compressor to register
   */
  public void register(Compressor c) {
    String encoding = c.getMessageEncoding();
    checkArgument(!encoding.contains(","), "Comma is currently not allowed in message encoding");
    compressors.put(encoding, c);
  }
}

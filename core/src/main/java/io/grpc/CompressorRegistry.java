/*
 * Copyright 2015 The gRPC Authors
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
    compressors = new ConcurrentHashMap<>();
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

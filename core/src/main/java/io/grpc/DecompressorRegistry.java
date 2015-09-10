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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;

import java.util.Collections;
import java.util.HashSet;
import java.util.Map.Entry;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;

import javax.annotation.Nullable;

/**
 * Encloses classes related to the compression and decompression of messages.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/492")
public final class DecompressorRegistry {

  private static final DecompressorRegistry INSTANCE = new DecompressorRegistry();

  private final ConcurrentMap<String, DecompressorInfo> decompressors;

  /**
   * Registers a decompressor for both decompression and message encoding negotiation.
   *
   * @param d The decompressor to register
   * @param advertised If true, the message encoding will be listed in the Accept-Encoding header.
   * @throws IllegalArgumentException if another compressor by the same name is already registered.
   */
  public static void register(Decompressor d, boolean advertised) {
    INSTANCE.internalRegister(d, advertised);
  }

  @VisibleForTesting
  void internalRegister(Decompressor d, boolean advertised) {
    DecompressorInfo previousInfo = decompressors.putIfAbsent(
        d.getMessageEncoding(), new DecompressorInfo(d, advertised));
    if (previousInfo != null) {
      throw new IllegalArgumentException(
          "A decompressor was already registered: " + previousInfo.decompressor);
    }
  }

  /**
   * Provides a list of all message encodings that have decompressors available.
   */
  public static Set<String> getKnownMessageEncodings() {
    return INSTANCE.internalGetKnownMessageEncodings();
  }

  @VisibleForTesting
  Set<String> internalGetKnownMessageEncodings() {
    return Collections.unmodifiableSet(decompressors.keySet());
  }

  /**
   * Provides a list of all message encodings that have decompressors available and should be
   * advertised.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/492")
  public static Set<String> getAdvertisedMessageEncodings() {
    return INSTANCE.internalGetAdvertisedMessageEncodings();
  }

  @VisibleForTesting
  Set<String> internalGetAdvertisedMessageEncodings() {
    Set<String> advertisedDecompressors = new HashSet<String>();
    for (Entry<String, DecompressorInfo> entry : decompressors.entrySet()) {
      if (entry.getValue().advertised) {
        advertisedDecompressors.add(entry.getKey());
      }
    }
    return Collections.unmodifiableSet(advertisedDecompressors);
  }

  /**
   * Returns a decompressor for the given message encoding, or {@code null} if none has been
   * registered.
   *
   * <p>This ignores whether the compressor is advertised.  According to the spec, if we know how
   * to process this encoding, we attempt to, regardless of whether or not it is part of the
   * encodings sent to the remote host.
   */
  @Nullable
  public static Decompressor lookupDecompressor(String messageEncoding) {
    return INSTANCE.internalLookupDecompressor(messageEncoding);
  }

  @Nullable
  @VisibleForTesting
  Decompressor internalLookupDecompressor(String messageEncoding) {
    DecompressorInfo info = decompressors.get(messageEncoding);
    return info != null ? info.decompressor : null;
  }


  @VisibleForTesting
  DecompressorRegistry() {
    decompressors = new ConcurrentHashMap<String, DecompressorInfo>();
    Decompressor gzip = new Codec.Gzip();
    // By default, Gzip
    decompressors.put(gzip.getMessageEncoding(), new DecompressorInfo(gzip, false));
    decompressors.put(
        Codec.Identity.NONE.getMessageEncoding(), new DecompressorInfo(Codec.Identity.NONE, false));
  }

  /**
   * Information about a decompressor.
   */
  private static final class DecompressorInfo {
    private final Decompressor decompressor;
    private volatile boolean advertised;

    DecompressorInfo(Decompressor decompressor, boolean advertised) {
      this.decompressor = checkNotNull(decompressor);
      this.advertised = advertised;
    }
  }
}

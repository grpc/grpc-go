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
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.Joiner;
import java.nio.charset.Charset;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Map.Entry;
import java.util.Set;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Encloses classes related to the compression and decompression of messages.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
@ThreadSafe
public final class DecompressorRegistry {
  static final Joiner ACCEPT_ENCODING_JOINER = Joiner.on(',');

  public static DecompressorRegistry emptyInstance() {
    return new DecompressorRegistry();
  }

  private static final DecompressorRegistry DEFAULT_INSTANCE =
      emptyInstance()
      .with(new Codec.Gzip(), true)
      .with(Codec.Identity.NONE, false);

  public static DecompressorRegistry getDefaultInstance() {
    return DEFAULT_INSTANCE;
  }

  private final Map<String, DecompressorInfo> decompressors;
  private final byte[] advertisedDecompressors;

  /**
   * Registers a decompressor for both decompression and message encoding negotiation.  Returns a
   * new registry.
   *
   * @param d The decompressor to register
   * @param advertised If true, the message encoding will be listed in the Accept-Encoding header.
   */
  public DecompressorRegistry with(Decompressor d, boolean advertised) {
    return new DecompressorRegistry(d, advertised, this);
  }

  private DecompressorRegistry(Decompressor d, boolean advertised, DecompressorRegistry parent) {
    String encoding = d.getMessageEncoding();
    checkArgument(!encoding.contains(","), "Comma is currently not allowed in message encoding");

    int newSize = parent.decompressors.size();
    if (!parent.decompressors.containsKey(d.getMessageEncoding())) {
      newSize++;
    }
    Map<String, DecompressorInfo> newDecompressors =
        new LinkedHashMap<>(newSize);
    for (DecompressorInfo di : parent.decompressors.values()) {
      String previousEncoding = di.decompressor.getMessageEncoding();
      if (!previousEncoding.equals(encoding)) {
        newDecompressors.put(
            previousEncoding, new DecompressorInfo(di.decompressor, di.advertised));
      }
    }
    newDecompressors.put(encoding, new DecompressorInfo(d, advertised));

    decompressors = Collections.unmodifiableMap(newDecompressors);
    advertisedDecompressors = ACCEPT_ENCODING_JOINER.join(getAdvertisedMessageEncodings())
        .getBytes(Charset.forName("US-ASCII"));
  }

  private DecompressorRegistry() {
    decompressors = new LinkedHashMap<>(0);
    advertisedDecompressors = new byte[0];
  }

  /**
   * Provides a list of all message encodings that have decompressors available.
   */
  public Set<String> getKnownMessageEncodings() {
    return decompressors.keySet();
  }


  byte[] getRawAdvertisedMessageEncodings() {
    return advertisedDecompressors;
  }

  /**
   * Provides a list of all message encodings that have decompressors available and should be
   * advertised.
   *
   * <p>The specification doesn't say anything about ordering, or preference, so the returned codes
   * can be arbitrary.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
  public Set<String> getAdvertisedMessageEncodings() {
    Set<String> advertisedDecompressors = new HashSet<>(decompressors.size());
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
  public Decompressor lookupDecompressor(String messageEncoding) {
    DecompressorInfo info = decompressors.get(messageEncoding);
    return info != null ? info.decompressor : null;
  }

  /**
   * Information about a decompressor.
   */
  private static final class DecompressorInfo {
    final Decompressor decompressor;
    final boolean advertised;

    DecompressorInfo(Decompressor decompressor, boolean advertised) {
      this.decompressor = checkNotNull(decompressor, "decompressor");
      this.advertised = advertised;
    }
  }
}

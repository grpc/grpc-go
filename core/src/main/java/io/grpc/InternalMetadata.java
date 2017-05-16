/*
 * Copyright 2016, Google Inc. All rights reserved.
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

import io.grpc.Metadata.Key;
import java.nio.charset.Charset;

/**
 * Internal {@link Metadata} accessor. This is intended for use by io.grpc.internal, and the
 * specifically supported transport packages. If you *really* think you need to use this, contact
 * the gRPC team first.
 */
@Internal
public final class InternalMetadata {

  /**
   * A specialized plain ASCII marshaller. Both input and output are assumed to be valid header
   * ASCII.
   *
   * <p>Extended here to break the dependency.
   */
  @Internal
  public interface TrustedAsciiMarshaller<T> extends Metadata.TrustedAsciiMarshaller<T> {}

  /**
   * Copy of StandardCharsets, which is only available on Java 1.7 and above.
   */
  @Internal
  public static final Charset US_ASCII = Charset.forName("US-ASCII");

  @Internal
  public static <T> Key<T> keyOf(String name, TrustedAsciiMarshaller<T> marshaller) {
    return Metadata.Key.of(name, marshaller);
  }

  @Internal
  public static Metadata newMetadata(byte[]... binaryValues) {
    return new Metadata(binaryValues);
  }

  @Internal
  public static Metadata newMetadata(int usedNames, byte[]... binaryValues) {
    return new Metadata(usedNames, binaryValues);
  }

  @Internal
  public static byte[][] serialize(Metadata md) {
    return md.serialize();
  }

  @Internal
  public static int headerCount(Metadata md) {
    return md.headerCount();
  }
}

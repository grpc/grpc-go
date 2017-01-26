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

package io.grpc.netty;

import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.AsciiString;
import java.util.Iterator;
import java.util.Map.Entry;
import java.util.NoSuchElementException;

/**
 * A custom implementation of Http2Headers that only includes methods used by gRPC.
 */
final class GrpcHttp2OutboundHeaders extends AbstractHttp2Headers {

  private final AsciiString[] normalHeaders;
  private final AsciiString[] preHeaders;
  private static final AsciiString[] EMPTY = new AsciiString[]{};

  static GrpcHttp2OutboundHeaders clientRequestHeaders(byte[][] serializedMetadata,
      AsciiString authority, AsciiString path, AsciiString method, AsciiString scheme,
      AsciiString userAgent) {
    AsciiString[] preHeaders = new AsciiString[] {
        Http2Headers.PseudoHeaderName.AUTHORITY.value(), authority,
        Http2Headers.PseudoHeaderName.PATH.value(), path,
        Http2Headers.PseudoHeaderName.METHOD.value(), method,
        Http2Headers.PseudoHeaderName.SCHEME.value(), scheme,
        Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC,
        Utils.TE_HEADER, Utils.TE_TRAILERS,
        Utils.USER_AGENT, userAgent,
    };
    return new GrpcHttp2OutboundHeaders(preHeaders, serializedMetadata);
  }

  static GrpcHttp2OutboundHeaders serverResponseHeaders(byte[][] serializedMetadata) {
    AsciiString[] preHeaders = new AsciiString[] {
        Http2Headers.PseudoHeaderName.STATUS.value(), Utils.STATUS_OK,
        Utils.CONTENT_TYPE_HEADER, Utils.CONTENT_TYPE_GRPC,
    };
    return new GrpcHttp2OutboundHeaders(preHeaders, serializedMetadata);
  }

  static GrpcHttp2OutboundHeaders serverResponseTrailers(byte[][] serializedMetadata) {
    return new GrpcHttp2OutboundHeaders(EMPTY, serializedMetadata);
  }

  private GrpcHttp2OutboundHeaders(AsciiString[] preHeaders, byte[][] serializedMetadata) {
    normalHeaders = new AsciiString[serializedMetadata.length];
    for (int i = 0; i < normalHeaders.length; i++) {
      normalHeaders[i] = new AsciiString(serializedMetadata[i], false);
    }
    this.preHeaders = preHeaders;
  }

  @Override
  public Iterator<Entry<CharSequence, CharSequence>> iterator() {
    return new Itr();
  }

  @Override
  public int size() {
    return (normalHeaders.length + preHeaders.length) / 2;
  }

  private class Itr implements Entry<CharSequence, CharSequence>,
      Iterator<Entry<CharSequence, CharSequence>> {
    private int idx;
    private AsciiString[] current = preHeaders.length != 0 ? preHeaders : normalHeaders;
    private AsciiString key;
    private AsciiString value;

    @Override
    public boolean hasNext() {
      return idx < current.length;
    }

    /**
     * This function is ordered specifically to get ideal performance on OpenJDK.  If you decide to
     * change it, even in ways that don't seem possible to affect performance, please benchmark
     * speeds before and after.
     */
    @Override
    public Entry<CharSequence, CharSequence> next() {
      if (hasNext()) {
        key = current[idx];
        value = current[idx + 1];
        idx += 2;
        if (idx >= current.length && current == preHeaders) {
          current = normalHeaders;
          idx = 0;
        }
        return this;
      } else {
        throw new NoSuchElementException();
      }
    }

    @Override
    public CharSequence getKey() {
      return key;
    }

    @Override
    public CharSequence getValue() {
      return value;
    }

    @Override
    public CharSequence setValue(CharSequence value) {
      throw new UnsupportedOperationException();
    }

    @Override
    public void remove() {
      throw new UnsupportedOperationException();
    }
  }

  @Override
  public String toString() {
    StringBuilder builder = new StringBuilder(getClass().getSimpleName()).append('[');
    String separator = "";
    for (Entry<CharSequence, CharSequence> e : this) {
      CharSequence name = e.getKey();
      CharSequence value = e.getValue();
      builder.append(separator);
      builder.append(name).append(": ").append(value);
      separator = ", ";
    }
    builder.append(']');
    return builder.toString();
  }
}

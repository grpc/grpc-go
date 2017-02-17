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

import io.grpc.Metadata;
import io.grpc.Metadata.AsciiMarshaller;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.handler.codec.http2.DefaultHttp2HeadersEncoder;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2HeadersEncoder;
import io.netty.util.AsciiString;
import java.util.UUID;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Param;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;

/**
 * Header encoding benchmark.
 */
@State(Scope.Benchmark)
public class OutboundHeadersBenchmark {
  @Param({"1", "5", "10", "20"})
  public int headerCount;

  private final AsciiMarshaller<String> keyMarshaller = new AsciiMarshaller<String>() {
    @Override
    public String toAsciiString(String value) {
      return value;
    }

    @Override
    public String parseAsciiString(String serialized) {
      return serialized;
    }
  };

  private final Metadata metadata = new Metadata();
  private final AsciiString scheme = new AsciiString("https");
  private final AsciiString defaultPath = new AsciiString("/Service.MethodMethodMethod");
  private final AsciiString authority = new AsciiString("authority.googleapis.bogus");
  private final AsciiString userAgent = new AsciiString("grpc-java-netty");
  private final Http2HeadersEncoder headersEncoder = new DefaultHttp2HeadersEncoder();
  private final ByteBuf scratchBuffer = UnpooledByteBufAllocator.DEFAULT.buffer(4096);

  @Setup
  public void setUp() throws Exception {
    for (int i = 0; i < headerCount; i++) {
      metadata.put(Metadata.Key.of("key-" + i, keyMarshaller), UUID.randomUUID().toString());
    }
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public Http2Headers convertClientHeaders() {
    return Utils.convertClientHeaders(metadata, scheme, defaultPath, authority, Utils.HTTP_METHOD,
        userAgent);
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public Http2Headers convertServerHeaders() {
    return Utils.convertServerHeaders(metadata);
  }

  /**
   * This will encode the random metadata fields, and repeatedly lookup the default other headers.
   */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public ByteBuf encodeClientHeaders() throws Exception {
    scratchBuffer.clear();
    Http2Headers headers =
        Utils.convertClientHeaders(metadata, scheme, defaultPath, authority, Utils.HTTP_METHOD,
            userAgent);
    headersEncoder.encodeHeaders(1, headers, scratchBuffer);
    return scratchBuffer;
  }
}

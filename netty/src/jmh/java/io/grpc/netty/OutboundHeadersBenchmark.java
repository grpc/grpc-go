/*
 * Copyright 2016 The gRPC Authors
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

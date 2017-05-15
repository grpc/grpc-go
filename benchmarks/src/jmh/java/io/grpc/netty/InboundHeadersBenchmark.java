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

import static io.grpc.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.netty.Utils.TE_TRAILERS;
import static io.netty.util.AsciiString.of;

import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2RequestHeaders;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2ResponseHeaders;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.AsciiString;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.CompilerControl;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.infra.Blackhole;

/**
 * Benchmarks for {@link GrpcHttp2RequestHeaders} and {@link GrpcHttp2ResponseHeaders}.
 */
@State(Scope.Thread)
public class InboundHeadersBenchmark {

  private static AsciiString[] requestHeaders;
  private static AsciiString[] responseHeaders;

  static {
    setupRequestHeaders();
    setupResponseHeaders();
  }

  // Headers taken from the gRPC spec.
  private static void setupRequestHeaders() {
    requestHeaders = new AsciiString[18];
    int i = 0;
    requestHeaders[i++] = of(":method");
    requestHeaders[i++] = of("POST");
    requestHeaders[i++] = of(":scheme");
    requestHeaders[i++] = of("http");
    requestHeaders[i++] = of(":path");
    requestHeaders[i++] = of("/google.pubsub.v2.PublisherService/CreateTopic");
    requestHeaders[i++] = of(":authority");
    requestHeaders[i++] = of("pubsub.googleapis.com");
    requestHeaders[i++] = of("te");
    requestHeaders[i++] = of("trailers");
    requestHeaders[i++] = of("grpc-timeout");
    requestHeaders[i++] = of("1S");
    requestHeaders[i++] = of("content-type");
    requestHeaders[i++] = of("application/grpc+proto");
    requestHeaders[i++] = of("grpc-encoding");
    requestHeaders[i++] = of("gzip");
    requestHeaders[i++] = of("authorization");
    requestHeaders[i] = of("Bearer y235.wef315yfh138vh31hv93hv8h3v");
  }

  private static void setupResponseHeaders() {
    responseHeaders = new AsciiString[4];
    int i = 0;
    responseHeaders[i++] = of(":status");
    responseHeaders[i++] = of("200");
    responseHeaders[i++] = of("grpc-encoding");
    responseHeaders[i] = of("gzip");
  }

  /**
   * Checkstyle.
   */
  @Benchmark
  @BenchmarkMode(Mode.AverageTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void grpcHeaders_serverHandler(Blackhole bh) {
    serverHandler(bh, new GrpcHttp2RequestHeaders(4));
  }

  /**
   * Checkstyle.
   */
  @Benchmark
  @BenchmarkMode(Mode.AverageTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void defaultHeaders_serverHandler(Blackhole bh) {
    serverHandler(bh, new DefaultHttp2Headers(true, 9));
  }

  /**
   *  Checkstyle.
   */
  @Benchmark
  @BenchmarkMode(Mode.AverageTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void grpcHeaders_clientHandler(Blackhole bh) {
    clientHandler(bh, new GrpcHttp2ResponseHeaders(2));
  }

  /**
   * Checkstyle.
   */
  @Benchmark
  @BenchmarkMode(Mode.AverageTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void defaultHeaders_clientHandler(Blackhole bh) {
    clientHandler(bh, new DefaultHttp2Headers(true, 2));
  }

  @CompilerControl(CompilerControl.Mode.INLINE)
  private static void serverHandler(Blackhole bh, Http2Headers headers) {
    for (int i = 0; i < requestHeaders.length; i += 2) {
      bh.consume(headers.add(requestHeaders[i], requestHeaders[i + 1]));
    }

    // Sequence of headers accessed in NettyServerHandler
    bh.consume(headers.get(TE_TRAILERS));
    bh.consume(headers.get(CONTENT_TYPE_HEADER));
    bh.consume(headers.method());
    bh.consume(headers.get(CONTENT_TYPE_HEADER));
    bh.consume(headers.path());

    bh.consume(Utils.convertHeaders(headers));
  }

  @CompilerControl(CompilerControl.Mode.INLINE)
  private static void clientHandler(Blackhole bh, Http2Headers headers) {
    // NettyClientHandler does not directly access headers, but convert to Metadata immediately.

    bh.consume(headers.add(responseHeaders[0], responseHeaders[1]));
    bh.consume(headers.add(responseHeaders[2], responseHeaders[3]));

    bh.consume(Utils.convertHeaders(headers));
  }

//  /**
//   * Prints the size of the header objects in bytes. Needs JOL (Java Object Layout) as a
//   * dependency.
//   */
//  public static void main(String... args) {
//    Http2Headers grpcRequestHeaders = new GrpcHttp2RequestHeaders(4);
//    Http2Headers defaultRequestHeaders = new DefaultHttp2Headers(true, 9);
//    for (int i = 0; i < requestHeaders.length; i += 2) {
//      grpcRequestHeaders.add(requestHeaders[i], requestHeaders[i + 1]);
//      defaultRequestHeaders.add(requestHeaders[i], requestHeaders[i + 1]);
//    }
//    long c = 10L;
//    int m = ((int) c) / 20;
//
//    long grpcRequestHeadersBytes = GraphLayout.parseInstance(grpcRequestHeaders).totalSize();
//    long defaultRequestHeadersBytes =
//        GraphLayout.parseInstance(defaultRequestHeaders).totalSize();
//
//    System.out.printf("gRPC Request Headers: %d bytes%nNetty Request Headers: %d bytes%n",
//        grpcRequestHeadersBytes, defaultRequestHeadersBytes);
//
//    Http2Headers grpcResponseHeaders = new GrpcHttp2RequestHeaders(4);
//    Http2Headers defaultResponseHeaders = new DefaultHttp2Headers(true, 9);
//    for (int i = 0; i < responseHeaders.length; i += 2) {
//      grpcResponseHeaders.add(responseHeaders[i], responseHeaders[i + 1]);
//      defaultResponseHeaders.add(responseHeaders[i], responseHeaders[i + 1]);
//    }
//
//    long grpcResponseHeadersBytes = GraphLayout.parseInstance(grpcResponseHeaders).totalSize();
//    long defaultResponseHeadersBytes =
//        GraphLayout.parseInstance(defaultResponseHeaders).totalSize();
//
//    System.out.printf("gRPC Response Headers: %d bytes%nNetty Response Headers: %d bytes%n",
//        grpcResponseHeadersBytes, defaultResponseHeadersBytes);
//  }
}

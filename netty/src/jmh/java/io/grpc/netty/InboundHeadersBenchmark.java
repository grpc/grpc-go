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

import static io.grpc.netty.Utils.CONTENT_TYPE_HEADER;
import static io.grpc.netty.Utils.TE_TRAILERS;

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
    requestHeaders[i++] = AsciiString.of(":method");
    requestHeaders[i++] = AsciiString.of("POST");
    requestHeaders[i++] = AsciiString.of(":scheme");
    requestHeaders[i++] = AsciiString.of("http");
    requestHeaders[i++] = AsciiString.of(":path");
    requestHeaders[i++] = AsciiString.of("/google.pubsub.v2.PublisherService/CreateTopic");
    requestHeaders[i++] = AsciiString.of(":authority");
    requestHeaders[i++] = AsciiString.of("pubsub.googleapis.com");
    requestHeaders[i++] = AsciiString.of("te");
    requestHeaders[i++] = AsciiString.of("trailers");
    requestHeaders[i++] = AsciiString.of("grpc-timeout");
    requestHeaders[i++] = AsciiString.of("1S");
    requestHeaders[i++] = AsciiString.of("content-type");
    requestHeaders[i++] = AsciiString.of("application/grpc+proto");
    requestHeaders[i++] = AsciiString.of("grpc-encoding");
    requestHeaders[i++] = AsciiString.of("gzip");
    requestHeaders[i++] = AsciiString.of("authorization");
    requestHeaders[i] = AsciiString.of("Bearer y235.wef315yfh138vh31hv93hv8h3v");
  }

  private static void setupResponseHeaders() {
    responseHeaders = new AsciiString[4];
    int i = 0;
    responseHeaders[i++] = AsciiString.of(":status");
    responseHeaders[i++] = AsciiString.of("200");
    responseHeaders[i++] = AsciiString.of("grpc-encoding");
    responseHeaders[i] = AsciiString.of("gzip");
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

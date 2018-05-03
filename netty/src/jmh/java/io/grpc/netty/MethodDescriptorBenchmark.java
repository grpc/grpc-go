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

import io.grpc.InternalKnownTransport;
import io.grpc.InternalMethodDescriptor;
import io.grpc.MethodDescriptor;
import io.netty.util.AsciiString;
import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.State;

/**
 * Benchmark for Method Descriptors.
 */
@State(Scope.Benchmark)
public class MethodDescriptorBenchmark {

  private static final MethodDescriptor.Marshaller<Void> marshaller =
      new MethodDescriptor.Marshaller<Void>() {
    @Override
    public InputStream stream(Void value) {
      return new ByteArrayInputStream(new byte[]{});
    }

    @Override
    public Void parse(InputStream stream) {
      return null;
    }
  };

  MethodDescriptor<Void, Void> method = MethodDescriptor.<Void, Void>newBuilder()
      .setType(MethodDescriptor.MethodType.UNARY)
      .setFullMethodName("Service/Method")
      .setRequestMarshaller(marshaller)
      .setResponseMarshaller(marshaller)
      .build();

  InternalMethodDescriptor imd = new InternalMethodDescriptor(InternalKnownTransport.NETTY);

  byte[] directBytes = new AsciiString("/" + method.getFullMethodName()).toByteArray();

  /** Foo bar. */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public AsciiString old() {
    return new AsciiString("/" + method.getFullMethodName());
  }

  /** Foo bar. */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public AsciiString transportSpecific() {
    AsciiString path;
    if ((path = (AsciiString) imd.geRawMethodName(method)) != null) {
      path = new AsciiString("/" + method.getFullMethodName());
      imd.setRawMethodName(method, path);
    }
    return path;
  }

  /** Foo bar. */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public AsciiString direct() {
    return new AsciiString(directBytes, false);
  }
}


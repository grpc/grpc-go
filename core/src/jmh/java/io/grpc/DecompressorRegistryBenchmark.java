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

package io.grpc;

import io.grpc.internal.GrpcUtil;
import io.grpc.internal.TransportFrameUtil;
import java.io.IOException;
import java.io.InputStream;
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
 * Decomressor Registry encoding benchmark.
 */
@State(Scope.Benchmark)
public class DecompressorRegistryBenchmark {

  @Param({"0", "1", "2"})
  public int extraEncodings;

  private DecompressorRegistry reg = DecompressorRegistry.getDefaultInstance();

  @Setup
  public void setUp() throws Exception {
    reg = DecompressorRegistry.getDefaultInstance();
    for (int i = 0; i < extraEncodings; i++) {
      reg = reg.with(new Decompressor() {

        @Override
        public String getMessageEncoding() {
          return UUID.randomUUID().toString();

        }

        @Override
        public InputStream decompress(InputStream is) throws IOException {
          return null;

        }
      }, true);
    }
  }

  /**
   * Javadoc comment.
   */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public byte[][] marshalOld() {
    Metadata m = new Metadata();
    m.put(
        GrpcUtil.MESSAGE_ACCEPT_ENCODING_KEY,
        InternalDecompressorRegistry.getRawAdvertisedMessageEncodings(reg));
    return TransportFrameUtil.toHttp2Headers(m);
  }
}


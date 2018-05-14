/*
 * Copyright 2017 The gRPC Authors
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
 * Javadoc.
 */
@State(Scope.Benchmark)
public class AttributesBenchmark {

  public Attributes base = Attributes.EMPTY;

  public Attributes.Key<Object>[] keys;
  public Attributes withValue = base;

  /**
   * Javadoc.
   */
  @Setup
  @SuppressWarnings({"unchecked", "rawtypes"})
  public void setUp() {
    keys = new Attributes.Key[iterations];
    for (int i = 0; i < iterations; i++) {
      keys[i] = Attributes.Key.create("any");
      withValue = withValue.toBuilder().set(keys[i], "yes").build();
    }
  }

  @Param({"1", "2", "10"})
  public int iterations;

  /**
   * Javadoc.
   */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public Attributes chain() {
    Attributes attr = base;
    for (int i = 0; i < iterations; i++) {
      attr = attr.toBuilder().set(keys[i], new Object()).build();
    }
    return attr;
  }

  /**
   * Javadoc.
   */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public Object lookup() {
    return withValue.get(keys[0]);
  }
}

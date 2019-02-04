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

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Random;
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
 * Call options benchmark.
 */
@State(Scope.Benchmark)
public class CallOptionsBenchmark {

  @Param({"1", "2", "4", "8"})
  public int customOptionsCount;

  private List<CallOptions.Key<String>> customOptions;

  private CallOptions allOpts;
  private List<CallOptions.Key<String>> shuffledCustomOptions;

  /**
   * Setup.
   */
  @Setup
  public void setUp() throws Exception {
    customOptions = new ArrayList<>(customOptionsCount);
    for (int i = 0; i < customOptionsCount; i++) {
      customOptions.add(CallOptions.Key.createWithDefault("name " + i, "defaultvalue"));
    }

    allOpts = CallOptions.DEFAULT;
    for (int i = 0; i < customOptionsCount; i++) {
      allOpts = allOpts.withOption(customOptions.get(i), "value");
    }

    shuffledCustomOptions = new ArrayList<>(customOptions);
    // Make the shuffling deterministic
    Collections.shuffle(shuffledCustomOptions, new Random(1));
  }

  /**
   * Adding custom call options without duplicate keys.
   */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public CallOptions withOption() {
    CallOptions opts = CallOptions.DEFAULT;
    for (int i = 0; i < customOptions.size(); i++) {
      opts = opts.withOption(customOptions.get(i), "value");
    }
    return opts;
  }

  /**
   * Adding custom call options, overwritting existing keys.
   */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public CallOptions withOptionDuplicates() {
    CallOptions opts = allOpts;
    for (int i = 1; i < shuffledCustomOptions.size(); i++) {
      opts = opts.withOption(shuffledCustomOptions.get(i), "value2");
    }
    return opts;
  }
}

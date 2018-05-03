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
import org.openjdk.jmh.infra.Blackhole;

/** Write benchmark. */
public class WriteBenchmark {
  @State(Scope.Thread)
  public static class ContextState {
    @Param({"0", "10", "25", "100"})
    public int preexistingKeys;

    Context.Key<Object> key1 = Context.key("key1");
    Context.Key<Object> key2 = Context.key("key2");
    Context.Key<Object> key3 = Context.key("key3");
    Context.Key<Object> key4 = Context.key("key4");
    Context context;
    Object val = new Object();

    @Setup
    public void setup() {
      context = Context.ROOT;
      for (int i = 0; i < preexistingKeys; i++) {
        context = context.withValue(Context.key("preexisting_key" + i), val);
      }
    }
  }

  /** Perform the write operation. */
  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public Context doWrite(ContextState state, Blackhole bh) {
    return state.context.withValues(
        state.key1, state.val,
        state.key2, state.val,
        state.key3, state.val,
        state.key4, state.val);
  }
}

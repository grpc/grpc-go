/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.benchmarks.netty;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.ServerServiceDefinition;
import io.grpc.testing.TestMethodDescriptors;
import io.grpc.util.MutableHandlerRegistry;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.Fork;
import org.openjdk.jmh.annotations.Level;
import org.openjdk.jmh.annotations.Param;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.infra.Blackhole;

/**
 * Benchmark for {@link MutableHandlerRegistry}.
 */
@State(Scope.Benchmark)
@Fork(1)
public class HandlerRegistryBenchmark {

  private static final String VALID_CHARACTERS =
          "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.";

  @Param({"50"})
  public int nameLength;

  @Param({"100"})
  public int serviceCount;

  @Param({"100"})
  public int methodCountPerService;

  private MutableHandlerRegistry registry;
  private List<String> fullMethodNames;

  /**
   * Set up the registry.
   */
  @Setup(Level.Trial)
  public void setup() throws Exception {
    registry = new MutableHandlerRegistry();
    fullMethodNames = new ArrayList<>(serviceCount * methodCountPerService);
    for (int serviceIndex = 0; serviceIndex < serviceCount; ++serviceIndex) {
      String serviceName = randomString();
      ServerServiceDefinition.Builder serviceBuilder = ServerServiceDefinition.builder(serviceName);
      for (int methodIndex = 0; methodIndex < methodCountPerService; ++methodIndex) {
        String methodName = randomString();

        MethodDescriptor<Void, Void> methodDescriptor = MethodDescriptor.<Void, Void>newBuilder()
            .setType(MethodDescriptor.MethodType.UNKNOWN)
            .setFullMethodName(MethodDescriptor.generateFullMethodName(serviceName, methodName))
            .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
            .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
            .build();
        serviceBuilder.addMethod(methodDescriptor,
            new ServerCallHandler<Void, Void>() {
              @Override
              public Listener<Void> startCall(ServerCall<Void, Void> call,
                  Metadata headers) {
                return null;
              }
            });
        fullMethodNames.add(methodDescriptor.getFullMethodName());
      }
      registry.addService(serviceBuilder.build());
    }
  }

  /**
   * Benchmark the {@link MutableHandlerRegistry#lookupMethod(String)} throughput.
   */
  @Benchmark
  public void lookupMethod(Blackhole bh) {
    for (String fullMethodName : fullMethodNames) {
      bh.consume(registry.lookupMethod(fullMethodName));
    }
  }

  private String randomString() {
    Random r = new Random();
    char[] bytes = new char[nameLength];
    for (int ix = 0; ix < nameLength; ++ix) {
      int charIx = r.nextInt(VALID_CHARACTERS.length());
      bytes[ix] = VALID_CHARACTERS.charAt(charIx);
    }
    return new String(bytes);
  }
}

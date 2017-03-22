/*
 * Copyright 2015, Google Inc. All rights reserved.
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
    fullMethodNames = new ArrayList<String>(serviceCount * methodCountPerService);
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

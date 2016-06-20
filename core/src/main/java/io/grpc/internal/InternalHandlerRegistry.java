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

package io.grpc.internal;

import com.google.common.collect.ImmutableMap;

import io.grpc.ServerServiceDefinition;
import io.grpc.ServerServiceDefinition.ServerMethodDefinition;

import java.util.HashMap;
import javax.annotation.Nullable;

final class InternalHandlerRegistry {
  private final ImmutableMap<String, ServerMethodDefinition<?, ?>> methods;

  private InternalHandlerRegistry(ImmutableMap<String, ServerMethodDefinition<?, ?>> methods) {
    this.methods = methods;
  }

  @Nullable
  ServerMethodDefinition<?, ?> lookupMethod(String methodName) {
    return methods.get(methodName);
  }

  static class Builder {
    // Store per-service first, to make sure services are added/replaced atomically.
    private final HashMap<String, ServerServiceDefinition> services =
        new HashMap<String, ServerServiceDefinition>();

    Builder addService(ServerServiceDefinition service) {
      services.put(service.getServiceDescriptor().getName(), service);
      return this;
    }

    InternalHandlerRegistry build() {
      ImmutableMap.Builder<String, ServerMethodDefinition<?, ?>> mapBuilder =
          ImmutableMap.builder();
      for (ServerServiceDefinition service : services.values()) {
        for (ServerMethodDefinition<?, ?> method : service.getMethods()) {
          mapBuilder.put(method.getMethodDescriptor().getFullMethodName(), method);
        }
      }
      return new InternalHandlerRegistry(mapBuilder.build());
    }
  }
}

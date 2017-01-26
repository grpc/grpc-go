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

import io.grpc.ServerMethodDefinition;
import io.grpc.ServerServiceDefinition;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import javax.annotation.Nullable;

final class InternalHandlerRegistry {

  private final List<ServerServiceDefinition> services;
  private final Map<String, ServerMethodDefinition<?, ?>> methods;

  private InternalHandlerRegistry(
      List<ServerServiceDefinition> services, Map<String, ServerMethodDefinition<?, ?>> methods) {
    this.services = services;
    this.methods = methods;
  }

  /**
   * Returns the service definitions in this registry.
   */
  public List<ServerServiceDefinition> getServices() {
    return services;
  }

  @Nullable
  ServerMethodDefinition<?, ?> lookupMethod(String methodName) {
    return methods.get(methodName);
  }

  static class Builder {

    // Store per-service first, to make sure services are added/replaced atomically.
    private final HashMap<String, ServerServiceDefinition> services =
        new LinkedHashMap<String, ServerServiceDefinition>();

    Builder addService(ServerServiceDefinition service) {
      services.put(service.getServiceDescriptor().getName(), service);
      return this;
    }

    InternalHandlerRegistry build() {
      Map<String, ServerMethodDefinition<?, ?>> map =
          new HashMap<String, ServerMethodDefinition<?, ?>>();
      for (ServerServiceDefinition service : services.values()) {
        for (ServerMethodDefinition<?, ?> method : service.getMethods()) {
          map.put(method.getMethodDescriptor().getFullMethodName(), method);
        }
      }
      return new InternalHandlerRegistry(
          Collections.unmodifiableList(new ArrayList<ServerServiceDefinition>(services.values())),
          Collections.unmodifiableMap(map));
    }
  }
}

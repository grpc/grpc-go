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

package io.grpc.internal;

import io.grpc.HandlerRegistry;
import io.grpc.ServerMethodDefinition;
import io.grpc.ServerServiceDefinition;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import javax.annotation.Nullable;

final class InternalHandlerRegistry extends HandlerRegistry {

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
  @Override
  public List<ServerServiceDefinition> getServices() {
    return services;
  }

  @Nullable
  @Override
  public ServerMethodDefinition<?, ?> lookupMethod(String methodName, @Nullable String authority) {
    // TODO (carl-mastrangelo): honor authority header.
    return methods.get(methodName);
  }

  static final class Builder {

    // Store per-service first, to make sure services are added/replaced atomically.
    private final HashMap<String, ServerServiceDefinition> services =
        new LinkedHashMap<>();

    Builder addService(ServerServiceDefinition service) {
      services.put(service.getServiceDescriptor().getName(), service);
      return this;
    }

    InternalHandlerRegistry build() {
      Map<String, ServerMethodDefinition<?, ?>> map =
          new HashMap<>();
      for (ServerServiceDefinition service : services.values()) {
        for (ServerMethodDefinition<?, ?> method : service.getMethods()) {
          map.put(method.getMethodDescriptor().getFullMethodName(), method);
        }
      }
      return new InternalHandlerRegistry(
          Collections.unmodifiableList(new ArrayList<>(services.values())),
          Collections.unmodifiableMap(map));
    }
  }
}

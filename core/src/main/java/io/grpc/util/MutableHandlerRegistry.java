/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.util;

import io.grpc.BindableService;
import io.grpc.ExperimentalApi;
import io.grpc.HandlerRegistry;
import io.grpc.MethodDescriptor;
import io.grpc.ServerMethodDefinition;
import io.grpc.ServerServiceDefinition;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Default implementation of {@link MutableHandlerRegistry}.
 *
 * <p>Uses {@link ConcurrentHashMap} to avoid service registration excessively
 * blocking method lookup.
 */
@ThreadSafe
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/933")
public final class MutableHandlerRegistry extends HandlerRegistry {
  private final ConcurrentMap<String, ServerServiceDefinition> services
      = new ConcurrentHashMap<String, ServerServiceDefinition>();

  /**
   * Registers a service.
   *
   * @return the previously registered service with the same service descriptor name if exists,
   *         otherwise {@code null}.
   */
  @Nullable
  public ServerServiceDefinition addService(ServerServiceDefinition service) {
    return services.put(service.getServiceDescriptor().getName(), service);
  }

  /**
   * Registers a service.
   *
   * @return the previously registered service with the same service descriptor name if exists,
   *         otherwise {@code null}.
   */
  @Nullable
  public ServerServiceDefinition addService(BindableService bindableService) {
    return addService(bindableService.bindService());
  }

  /**
   * Removes a registered service
   *
   * @return true if the service was found to be removed.
   */
  public boolean removeService(ServerServiceDefinition service) {
    return services.remove(service.getServiceDescriptor().getName(), service);
  }

  /**
   *  Note: This does not necessarily return a consistent view of the map.
   */
  @Override
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
  public List<ServerServiceDefinition> getServices() {
    return Collections.unmodifiableList(new ArrayList<ServerServiceDefinition>(services.values()));
  }

  /**
   * Note: This does not actually honor the authority provided.  It will, eventually in the future.
   */
  @Override
  @Nullable
  public ServerMethodDefinition<?, ?> lookupMethod(String methodName, @Nullable String authority) {
    String serviceName = MethodDescriptor.extractFullServiceName(methodName);
    if (serviceName == null) {
      return null;
    }
    ServerServiceDefinition service = services.get(serviceName);
    if (service == null) {
      return null;
    }
    return service.getMethod(methodName);
  }
}

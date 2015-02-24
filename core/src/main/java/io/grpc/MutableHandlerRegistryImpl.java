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

package io.grpc;

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
public final class MutableHandlerRegistryImpl extends MutableHandlerRegistry {
  private final ConcurrentMap<String, ServerServiceDefinition> services
      = new ConcurrentHashMap<String, ServerServiceDefinition>();

  @Override
  @Nullable
  public ServerServiceDefinition addService(ServerServiceDefinition service) {
    return services.put(service.getName(), service);
  }

  @Override
  public boolean removeService(ServerServiceDefinition service) {
    return services.remove(service.getName(), service);
  }

  @Override
  @Nullable
  public Method lookupMethod(String methodName) {
    if (!methodName.startsWith("/")) {
      return null;
    }
    methodName = methodName.substring(1);
    int index = methodName.lastIndexOf("/");
    if (index == -1) {
      return null;
    }
    ServerServiceDefinition service = services.get(methodName.substring(0, index));
    if (service == null) {
      return null;
    }
    ServerMethodDefinition<?, ?> method = service.getMethod(methodName.substring(index + 1));
    if (method == null) {
      return null;
    }
    return new Method(service, method);
  }
}

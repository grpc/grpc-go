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

import com.google.common.base.Preconditions;
import com.google.common.collect.ImmutableList;
import com.google.common.collect.ImmutableMap;

import java.util.HashMap;
import java.util.Map;

/** Definition of a service to be exposed via a Server. */
public final class ServerServiceDefinition {
  public static Builder builder(String serviceName) {
    return new Builder(serviceName);
  }

  private final String name;
  private final ImmutableList<ServerMethodDefinition<?, ?>> methods;
  private final ImmutableMap<String, ServerMethodDefinition<?, ?>> methodLookup;

  private ServerServiceDefinition(String name, ImmutableList<ServerMethodDefinition<?, ?>> methods,
      Map<String, ServerMethodDefinition<?, ?>> methodLookup) {
    this.name = name;
    this.methods = methods;
    this.methodLookup = ImmutableMap.copyOf(methodLookup);
  }

  /** Simple name of the service. It is not an absolute path. */
  public String getName() {
    return name;
  }

  public ImmutableList<ServerMethodDefinition<?, ?>> getMethods() {
    return methods;
  }

  public ServerMethodDefinition<?, ?> getMethod(String name) {
    return methodLookup.get(name);
  }

  /** Builder for constructing Service instances. */
  public static final class Builder {
    private final String serviceName;
    private final ImmutableList.Builder<ServerMethodDefinition<?, ?>> methods
        = ImmutableList.builder();
    private final Map<String, ServerMethodDefinition<?, ?>> methodLookup
        = new HashMap<String, ServerMethodDefinition<?, ?>>();

    private Builder(String serviceName) {
      this.serviceName = serviceName;
    }

    /**
     * Add a method to be supported by the service.
     *
     * @param name simple name of the method, without the service prefix
     * @param requestMarshaller marshaller for deserializing incoming requests
     * @param responseMarshaller marshaller for serializing outgoing responses
     * @param handler handler for incoming calls
     */
    public <ReqT, RespT> Builder addMethod(String name, Marshaller<ReqT> requestMarshaller,
        Marshaller<RespT> responseMarshaller, ServerCallHandler<ReqT, RespT> handler) {
      return addMethod(new ServerMethodDefinition<ReqT, RespT>(
          Preconditions.checkNotNull(name, "name must not be null"),
          Preconditions.checkNotNull(requestMarshaller, "requestMarshaller must not be null"),
          Preconditions.checkNotNull(responseMarshaller, "responseMarshaller must not be null"),
          Preconditions.checkNotNull(handler, "handler must not be null")));
    }

    /** Add a method to be supported by the service. */
    public <ReqT, RespT> Builder addMethod(ServerMethodDefinition<ReqT, RespT> def) {
      if (methodLookup.containsKey(def.getName())) {
        throw new IllegalStateException("Method by same name already registered");
      }
      methodLookup.put(def.getName(), def);
      methods.add(def);
      return this;
    }

    /** Construct new ServerServiceDefinition. */
    public ServerServiceDefinition build() {
      return new ServerServiceDefinition(serviceName, methods.build(), methodLookup);
    }
  }
}

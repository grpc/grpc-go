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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.collect.ImmutableMap;

import java.util.Collection;
import java.util.HashMap;
import java.util.Map;

/** Definition of a service to be exposed via a Server. */
// TODO(zhangkun83): since the handler map uses fully qualified names as keys, we should
// consider removing ServerServiceDefinition to and let the registry to have a big map of
// handlers.
public final class ServerServiceDefinition {
  @ExperimentalApi
  public static Builder builder(String serviceName) {
    return new Builder(serviceName);
  }

  private final String name;
  private final ImmutableMap<String, ServerMethodDefinition<?, ?>> methods;

  private ServerServiceDefinition(
      String name, Map<String, ServerMethodDefinition<?, ?>> methods) {
    this.name = checkNotNull(name);
    this.methods = ImmutableMap.copyOf(methods);
  }

  /** Simple name of the service. It is not an absolute path. */
  public String getName() {
    return name;
  }

  @ExperimentalApi
  public Collection<ServerMethodDefinition<?, ?>> getMethods() {
    return methods.values();
  }

  /**
   * Look up a method by its fuly qualified name.
   *
   * @param name the fully qualified name without leading slash. E.g., "com.foo.Foo/Bar"
   */
  ServerMethodDefinition<?, ?> getMethod(String name) {
    return methods.get(name);
  }

  /** Builder for constructing Service instances. */
  public static final class Builder {
    private final String serviceName;
    private final Map<String, ServerMethodDefinition<?, ?>> methods = 
        new HashMap<String, ServerMethodDefinition<?, ?>>();

    private Builder(String serviceName) {
      this.serviceName = serviceName;
    }

    /**
     * Add a method to be supported by the service.
     *
     * @param method the {@link MethodDescriptor} of this method.
     * @param handler handler for incoming calls
     */
    @ExperimentalApi
    public <ReqT, RespT> Builder addMethod(
        MethodDescriptor<ReqT, RespT> method, ServerCallHandler<ReqT, RespT> handler) {
      return addMethod(ServerMethodDefinition.create(
          checkNotNull(method, "method must not be null"),
          checkNotNull(handler, "handler must not be null")));
    }

    /** Add a method to be supported by the service. */
    @ExperimentalApi
    public <ReqT, RespT> Builder addMethod(ServerMethodDefinition<ReqT, RespT> def) {
      MethodDescriptor<ReqT, RespT> method = def.getMethodDescriptor();
      checkArgument(
          serviceName.equals(MethodDescriptor.extractFullServiceName(method.getFullMethodName())),
          "Service name mismatch. Expected service name: '%s'. Actual method name: '%s'.",
          this.serviceName, method.getFullMethodName());
      String name = method.getFullMethodName();
      checkState(!methods.containsKey(name), "Method by same name already registered: %s", name);
      methods.put(name, def);
      return this;
    }

    /** Construct new ServerServiceDefinition. */
    public ServerServiceDefinition build() {
      return new ServerServiceDefinition(serviceName, methods);
    }
  }
}

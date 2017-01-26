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

package io.grpc;

import static com.google.common.base.Preconditions.checkArgument;

import com.google.common.base.Preconditions;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.Set;
import javax.annotation.Nullable;

/**
 * Descriptor for a service.
 */
public final class ServiceDescriptor {

  private final String name;
  private final Collection<MethodDescriptor<?, ?>> methods;
  private final Object marshallerDescriptor;

  public ServiceDescriptor(String name, MethodDescriptor<?, ?>... methods) {
    this(name, null, Arrays.asList(methods));
  }

  public ServiceDescriptor(String name, Collection<MethodDescriptor<?, ?>> methods) {
    this(name, null, methods);
  }

  public ServiceDescriptor(String name, Object marshallerDescriptor,
                           MethodDescriptor<?, ?>... methods) {
    this(name, marshallerDescriptor, Arrays.asList(methods));
  }

  /** Creates a new ServiceDescriptor. */
  public ServiceDescriptor(String name, Object marshallerDescriptor,
                           Collection<MethodDescriptor<?, ?>> methods) {
    this.name = Preconditions.checkNotNull(name, "name");
    Preconditions.checkNotNull(methods, "methods");
    validateMethodNames(name, methods);
    this.marshallerDescriptor = marshallerDescriptor;
    this.methods = Collections.unmodifiableList(new ArrayList<MethodDescriptor<?, ?>>(methods));
  }

  /** Simple name of the service. It is not an absolute path. */
  public String getName() {
    return name;
  }

  /**
   * A collection of {@link MethodDescriptor} instances describing the methods exposed by the
   * service.
   */
  public Collection<MethodDescriptor<?, ?>> getMethods() {
    return methods;
  }

  /**
   * Returns a marshaller-specific object that provides additional information about the service.
   * For example, when using Protobuf this should generally be a
   * {@link io.grpc.protobuf.ProtoFileDescriptorSupplier}, when present.
   */
  @Nullable
  public Object getMarshallerDescriptor() {
    return marshallerDescriptor;
  }

  static void validateMethodNames(String serviceName, Collection<MethodDescriptor<?, ?>> methods) {
    Set<String> allNames = new HashSet<String>(methods.size());
    for (MethodDescriptor<?, ?> method : methods) {
      String methodServiceName =
          MethodDescriptor.extractFullServiceName(method.getFullMethodName());
      checkArgument(serviceName.equals(methodServiceName),
          "service names %s != %s", methodServiceName, serviceName);
      checkArgument(allNames.add(method.getFullMethodName()),
          "duplicate name %s", method.getFullMethodName());
    }
  }
}
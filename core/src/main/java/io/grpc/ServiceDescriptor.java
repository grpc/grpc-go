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

package io.grpc;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.MoreObjects;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import javax.annotation.Nullable;

/**
 * Descriptor for a service.
 *
 * @since 1.0.0
 */
public final class ServiceDescriptor {

  private final String name;
  private final Collection<MethodDescriptor<?, ?>> methods;
  private final Object schemaDescriptor;

  /**
   * Constructs a new Service Descriptor.  Users are encouraged to use {@link #newBuilder}
   * instead.
   *
   * @param name The name of the service
   * @param methods The methods that are part of the service
   * @since 1.0.0
   */
  public ServiceDescriptor(String name, MethodDescriptor<?, ?>... methods) {
    this(name, Arrays.asList(methods));
  }

  /**
   * Constructs a new Service Descriptor.  Users are encouraged to use {@link #newBuilder}
   * instead.
   *
   * @param name The name of the service
   * @param methods The methods that are part of the service
   * @since 1.0.0
   */
  public ServiceDescriptor(String name, Collection<MethodDescriptor<?, ?>> methods) {
    this(newBuilder(name).addAllMethods(checkNotNull(methods, "methods")));
  }

  private ServiceDescriptor(Builder b) {
    this.name = b.name;
    validateMethodNames(name, b.methods);
    this.methods = Collections.unmodifiableList(new ArrayList<MethodDescriptor<?, ?>>(b.methods));
    this.schemaDescriptor = b.schemaDescriptor;
  }

  /**
   * Simple name of the service. It is not an absolute path.
   *
   * @since 1.0.0
   */
  public String getName() {
    return name;
  }

  /**
   * A collection of {@link MethodDescriptor} instances describing the methods exposed by the
   * service.
   *
   * @since 1.0.0
   */
  public Collection<MethodDescriptor<?, ?>> getMethods() {
    return methods;
  }

  /**
   * Returns the schema descriptor for this service.  A schema descriptor is an object that is not
   * used by gRPC core but includes information related to the service.  The type of the object
   * is specific to the consumer, so both the code setting the schema descriptor and the code
   * calling {@link #getSchemaDescriptor()} must coordinate.  For example, protobuf generated code
   * sets this value, in order to be consumed by the server reflection service.  See also:
   * {@code io.grpc.protobuf.ProtoFileDescriptorSupplier}.
   *
   * @since 1.1.0
   */
  @Nullable
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
  public Object getSchemaDescriptor() {
    return schemaDescriptor;
  }

  static void validateMethodNames(String serviceName, Collection<MethodDescriptor<?, ?>> methods) {
    Set<String> allNames = new HashSet<String>(methods.size());
    for (MethodDescriptor<?, ?> method : methods) {
      checkNotNull(method, "method");
      String methodServiceName =
          MethodDescriptor.extractFullServiceName(method.getFullMethodName());
      checkArgument(serviceName.equals(methodServiceName),
          "service names %s != %s", methodServiceName, serviceName);
      checkArgument(allNames.add(method.getFullMethodName()),
          "duplicate name %s", method.getFullMethodName());
    }
  }

  /**
   * Creates a new builder for a {@link ServiceDescriptor}.
   *
   * @since 1.1.0
   */
  public static Builder newBuilder(String name) {
    return new Builder(name);
  }

  /**
   * A builder for a {@link ServiceDescriptor}.
   *
   * @since 1.1.0
   */
  public static final class Builder {
    private Builder(String name) {
      setName(name);
    }

    private String name;
    private List<MethodDescriptor<?, ?>> methods = new ArrayList<MethodDescriptor<?, ?>>();
    private Object schemaDescriptor;

    /**
     * Sets the name.  This should be non-{@code null}.
     *
     * @param name The name of the service.
     * @return this builder.
     * @since 1.1.0
     */
    @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2666")
    public Builder setName(String name) {
      this.name = checkNotNull(name, "name");
      return this;
    }

    /**
     * Adds a method to this service.  This should be non-{@code null}.
     *
     * @param method the method to add to the descriptor.
     * @return this builder.
     * @since 1.1.0
     */
    public Builder addMethod(MethodDescriptor<?, ?> method) {
      methods.add(checkNotNull(method, "method"));
      return this;
    }

    /**
     * Currently not exposed.  Bulk adds methods to this builder.
     *
     * @param methods the methods to add.
     * @return this builder.
     */
    private Builder addAllMethods(Collection<MethodDescriptor<?, ?>> methods) {
      this.methods.addAll(methods);
      return this;
    }

    /**
     * Sets the schema descriptor for this builder.  A schema descriptor is an object that is not
     * used by gRPC core but includes information related to the service.  The type of the object
     * is specific to the consumer, so both the code calling this and the code calling
     * {@link ServiceDescriptor#getSchemaDescriptor()} must coordinate.  For example, protobuf
     * generated code sets this value, in order to be consumed by the server reflection service.
     *
     * @param schemaDescriptor an object that describes the service structure.  Should be immutable.
     * @return this builder.
     * @since 1.1.0
     */
    public Builder setSchemaDescriptor(@Nullable Object schemaDescriptor) {
      this.schemaDescriptor = schemaDescriptor;
      return this;
    }

    /**
     * Constructs a new {@link ServiceDescriptor}.  {@link #setName} should have been called with a
     * non-{@code null} value before calling this.
     *
     * @return a new ServiceDescriptor
     * @since 1.1.0
     */
    public ServiceDescriptor build() {
      return new ServiceDescriptor(this);
    }
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("name", name)
        .add("schemaDescriptor", schemaDescriptor)
        .add("methods", methods)
        .omitNullValues()
        .toString();
  }
}
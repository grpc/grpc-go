/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

package io.grpc.testing;

import io.grpc.ExperimentalApi;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import java.io.ByteArrayInputStream;
import java.io.InputStream;

/**
 * A collection of method descriptor constructors useful for tests.  These are useful if you need
 * a descriptor, but don't really care how it works.
 *
 * @since 1.1.0
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/2600")
public final class TestMethodDescriptors {
  private TestMethodDescriptors() {}

  /**
   * Creates a new method descriptor that always creates zero length messages, and always parses to
   * null objects.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2600")
  public static MethodDescriptor<Void, Void> voidMethod() {
    return TestMethodDescriptors.<Void, Void>noopMethod();
  }

  /**
   * Creates a new method descriptor that always creates zero length messages, and always parses to
   * null objects.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2600")
  public static <ReqT, RespT> MethodDescriptor<ReqT, RespT> noopMethod() {
    return noopMethod("service_foo", "method_bar");
  }

  private static <ReqT, RespT> MethodDescriptor<ReqT, RespT> noopMethod(
      String serviceName, String methodName) {
    return MethodDescriptor.<ReqT, RespT>newBuilder()
        .setType(MethodType.UNARY)
        .setFullMethodName(MethodDescriptor.generateFullMethodName(serviceName, methodName))
        .setRequestMarshaller(TestMethodDescriptors.<ReqT>noopMarshaller())
        .setResponseMarshaller(TestMethodDescriptors.<RespT>noopMarshaller())
        .build();
  }

  /**
   * Creates a new marshaller that does nothing.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2600")
  public static MethodDescriptor.Marshaller<Void> voidMarshaller() {
    return TestMethodDescriptors.<Void>noopMarshaller();
  }

  /**
   * Creates a new marshaller that does nothing.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2600")
  public static <T> MethodDescriptor.Marshaller<T> noopMarshaller() {
    return new NoopMarshaller<T>();
  }

  private static final class NoopMarshaller<T> implements MethodDescriptor.Marshaller<T> {
    @Override
    public InputStream stream(T value) {
      return new ByteArrayInputStream(new byte[]{});
    }

    @Override
    public T parse(InputStream stream) {
      return null;
    }
  }
}

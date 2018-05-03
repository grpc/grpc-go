/*
 * Copyright 2017 The gRPC Authors
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
    return MethodDescriptor.<Void, Void>newBuilder()
        .setType(MethodType.UNARY)
        .setFullMethodName(MethodDescriptor.generateFullMethodName("service_foo", "method_bar"))
        .setRequestMarshaller(TestMethodDescriptors.voidMarshaller())
        .setResponseMarshaller(TestMethodDescriptors.voidMarshaller())
        .build();
  }

  /**
   * Creates a new marshaller that does nothing.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2600")
  public static MethodDescriptor.Marshaller<Void> voidMarshaller() {
    return new NoopMarshaller();
  }

  private static final class NoopMarshaller implements MethodDescriptor.Marshaller<Void> {
    @Override
    public InputStream stream(Void value) {
      return new ByteArrayInputStream(new byte[]{});
    }

    @Override
    public Void parse(InputStream stream) {
      return null;
    }
  }
}

/*
 * Copyright 2017, Google Inc. All rights reserved.
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

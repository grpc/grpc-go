/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.stub.annotations;

import io.grpc.MethodDescriptor;
import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * {@link RpcMethod} contains a limited subset of information about the RPC to assist
 * <a href="https://docs.oracle.com/javase/6/docs/api/javax/annotation/processing/Processor.html">
 * Java Annotation Processors.</a>
 *
 * <p>
 *   This annotation is used by the gRPC stub compiler to annotate {@link MethodDescriptor}
 *   getters.  Users should not annotate their own classes with this annotation.  Not all stubs may
 *   have this annotation, so consumers should not assume that it is present.
 * </p>
 *
 * @since 1.14.0
 */
@Retention(RetentionPolicy.CLASS)
@Target(ElementType.METHOD)
public @interface RpcMethod {

  /**
   * The fully qualified method name.  This should match the name as returned by
   * {@link MethodDescriptor#generateFullMethodName(String, String)}.
   */
  String fullMethodName();

  /**
   * The request type of the method.  The request type class should be assignable from (i.e.
   * {@link Class#isAssignableFrom(Class)} the request type {@code ReqT} of the
   * {@link MethodDescriptor}.  Additionally, if the request {@code MethodDescriptor.Marshaller}
   * is a {@code MethodDescriptor.ReflectableMarshaller}, the request type should be assignable
   * from {@code MethodDescriptor.ReflectableMarshaller#getMessageClass()}.
   */
  Class<?> requestType();

  /**
   * The response type of the method.  The response type class should be assignable from (i.e.
   * {@link Class#isAssignableFrom(Class)} the response type {@code RespT} of the
   * {@link MethodDescriptor}.  Additionally, if the response {@code MethodDescriptor.Marshaller}
   * is a {@code MethodDescriptor.ReflectableMarshaller}, the response type should be assignable
   * from {@code MethodDescriptor.ReflectableMarshaller#getMessageClass()}.
   */
  Class<?> responseType();

  /**
   * The call type of the method.
   */
  MethodDescriptor.MethodType methodType();
}

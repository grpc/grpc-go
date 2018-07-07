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

import io.grpc.MethodDescriptor.MethodType;
import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Annotates a method descriptor method to provide metadata for annotation processing.
 */
@Retention(RetentionPolicy.CLASS)
@Target(ElementType.METHOD)
public @interface RpcMethod {

  /**
   * The full service name for the method
   */
  String fullServiceName();

  /**
   * The method name for the method
   */
  String methodName();

  /**
   * The input type of the method
   */
  Class<?> inputType();

  /**
   * The output type of the method
   */
  Class<?> outputType();

  /**
   * The call type of the method
   */
  MethodType methodType();
}

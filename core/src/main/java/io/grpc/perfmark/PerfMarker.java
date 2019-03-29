/*
 * Copyright 2019 The gRPC Authors
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

package io.grpc.perfmark;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Annotation to add PerfMark instrumentation points surrounding method invocation.
 *
 * <p>This class is {@link io.grpc.Internal} and {@link io.grpc.ExperimentalApi}.  Do not use this
 * yet.
 */
@Retention(RetentionPolicy.RUNTIME)
@Target(ElementType.METHOD)
// TODO(carl-mastrangelo): Add this line back in and make it okay on Android
//@IncompatibleModifiers(value = {Modifier.ABSTRACT, Modifier.NATIVE})
public @interface PerfMarker {

  /**
   * The name of the task; e.g. `parseHeaders`.
   */
  String taskName();

  /**
   * An optional computed tag.
   *
   * <p>There are 3 supported references that can be used
   * <ul>
   *     <li>{@code "this"}:  Then the tag will be the {@link Object#toString} of the current class.
   *     Only valid for instance methods.
   *     <li>{@code "someFieldName"}: Then the tag will be the result of
   *     calling {@link String#valueOf(Object)} on the field.  The field cannot be a primitive or
   *     and array type. (Though we may revisit this in the future).
   *     <li>{@code "$N"}: Then the tag will be the result of calling {@link String#valueOf(Object)}
   *     on the Nth method parameter.  Parameters are {@code 0} indexed so {@code "$1"} is the
   *     second parameter. The referenced parameter cannot be a primitive or an array type.
   *     (Though we may revisit this in the future).
   * </ul>
   *
   * <p>In general you should reference either {@code "this"} or {@code final} fields since
   * in these cases we can cache the operations to decrease the cost of computing the tags.  A side
   * effect of this is that for such references we will not have their tags recalculated after the
   * first time. Thus it is best to use immutable objects for tags.
   */
  String computedTag() default "";

  /**
   * True if class with annotation is immutable and instrumentation must adhere to this restriction.
   * If enableSampling is passed as argument to the agent, instrumentation points with <code>
   * immutable = true </code> are ignored.
   */
  boolean immutable() default false;
}


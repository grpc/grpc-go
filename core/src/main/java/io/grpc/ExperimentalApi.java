/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

import java.lang.annotation.Documented;
import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Indicates a public API that can change at any time, and has no guarantee of API stability and
 * backward-compatibility.
 *
 * <p>Usage guidelines:
 * <ol>
 * <li>This annotation is used only on public API. Internal interfaces should not use it.</li>
 * <li>After gRPC has gained API stability, this annotation can only be added to new API. Adding it
 * to an existing API is considered API-breaking.</li>
 * <li>Removing this annotation from an API gives it stable status.</li>
 * </ol>
 */
@Internal
@Retention(RetentionPolicy.SOURCE)
@Target({
    ElementType.ANNOTATION_TYPE,
    ElementType.CONSTRUCTOR,
    ElementType.FIELD,
    ElementType.METHOD,
    ElementType.PACKAGE,
    ElementType.TYPE})
@Documented
public @interface ExperimentalApi {
  /**
   * Context information such as links to discussion thread, tracking issue etc.
   */
  String value() default "";
}

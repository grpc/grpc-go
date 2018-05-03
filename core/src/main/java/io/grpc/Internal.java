/*
 * Copyright 2015 The gRPC Authors
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
 * Annotates a program element (class, method, package, etc) which is internal to gRPC, not part of
 * the public API, and should not be used by users of gRPC.
 *
 * <p>However, if you want to implement an alternative transport you may use the internal parts.
 * Please consult the gRPC team first, because internal APIs don't have the same API stability
 * guarantee as the public APIs do.
 *
 * <p>Note: This annotation is intended only for gRPC library code. Users should not attach this
 * annotation to their own code.
 *
 * <p>See: <a href="https://github.com/grpc/grpc-java-api-checker">grpc-java-api-checker</a>, an
 * Error Prone plugin to automatically check for usages of this API.
 */
@Retention(RetentionPolicy.CLASS)
@Target({
    ElementType.ANNOTATION_TYPE,
    ElementType.CONSTRUCTOR,
    ElementType.FIELD,
    ElementType.METHOD,
    ElementType.PACKAGE,
    ElementType.TYPE})
@Documented
public @interface Internal {
}

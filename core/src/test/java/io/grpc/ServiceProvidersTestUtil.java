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

package io.grpc;

import static com.google.common.truth.Truth.assertWithMessage;

import com.google.common.collect.Iterators;
import java.lang.reflect.Constructor;
import java.lang.reflect.Method;
import java.util.HashSet;
import java.util.Iterator;
import java.util.Set;
import java.util.regex.Pattern;

final class ServiceProvidersTestUtil {
  /**
   * Creates an iterator from the callable class via reflection, and checks that all expected
   * classes were loaded.
   *
   * <p>{@code callableClassName} is a {@code Callable<Iterable<Class<?>>} rather than the iterable
   * class name itself so that the iterable class can be non-public.
   *
   * <p>We accept class names as input so that we can test against classes not in the
   * testing class path.
   */
  static void testHardcodedClasses(
      String callableClassName,
      ClassLoader cl,
      Set<String> hardcodedClassNames) throws Exception {
    final Set<String> notLoaded = new HashSet<>(hardcodedClassNames);
    cl = new ClassLoader(cl) {
      @Override
      public Class<?> loadClass(String name, boolean resolve) throws ClassNotFoundException {
        if (notLoaded.remove(name)) {
          throw new ClassNotFoundException();
        } else {
          return super.loadClass(name, resolve);
        }
      }
    };
    cl = new StaticTestingClassLoader(cl, Pattern.compile("io\\.grpc\\.[^.]*"));
    // Some classes fall back to the context class loader.
    // Ensure that the context class loader is not an accidental backdoor.
    ClassLoader ccl = Thread.currentThread().getContextClassLoader();
    try {
      Thread.currentThread().setContextClassLoader(cl);
      Object[] results = Iterators.toArray(
          invokeIteratorCallable(callableClassName, cl), Object.class);
      assertWithMessage("The Iterable loaded a class that was not in hardcodedClassNames")
          .that(results).isEmpty();
      assertWithMessage(
          "The Iterable did not attempt to load some classes from hardcodedClassNames")
          .that(notLoaded).isEmpty();
    } finally {
      Thread.currentThread().setContextClassLoader(ccl);
    }
  }

  private static Iterator<?> invokeIteratorCallable(
      String callableClassName, ClassLoader cl) throws Exception {
    Class<?> klass = Class.forName(callableClassName, true, cl);
    Constructor<?> ctor = klass.getDeclaredConstructor();
    Object instance = ctor.newInstance();
    Method callMethod = klass.getMethod("call");
    return (Iterator<?>) callMethod.invoke(instance);
  }
}

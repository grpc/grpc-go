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

package io.grpc;

import static junit.framework.TestCase.assertFalse;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mockingDetails;
import static org.mockito.Mockito.verify;

import com.google.common.base.Defaults;
import com.google.common.base.MoreObjects;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.Collection;
import javax.annotation.Nullable;

/**
 * A util class to help test forwarding classes.
 */
public final class ForwardingTestUtil {
  /**
   * Use reflection to perform a basic sanity test. The forwarding class should forward all public
   * methods to the delegate, except for those in skippedMethods.  This does NOT verify that
   * arguments or return values are forwarded properly.
   *
   * @param delegateClass The class whose methods should be forwarded.
   * @param mockDelegate The mockito mock of the delegate class.
   * @param forwarder The forwarder object that forwards to the mockDelegate.
   * @param skippedMethods A collection of methods that are skipped by the test.
   */
  public static <T> void testMethodsForwarded(
      Class<T> delegateClass,
      T mockDelegate,
      T forwarder,
      Collection<Method> skippedMethods) throws Exception {
    testMethodsForwarded(
        delegateClass, mockDelegate, forwarder, skippedMethods,
        new ArgumentProvider() {
          @Override
          public Object get(Method method, int argPos, Class<?> clazz) {
            return null;
          }
        });
  }

  /**
   * Use reflection to perform a basic sanity test. The forwarding class should forward all public
   * methods to the delegate, except for those in skippedMethods.  This does NOT verify that return
   * values are forwarded properly, and can only verify the propagation of arguments for which
   * {@code argProvider} returns distinctive non-null values.
   *
   * @param delegateClass The class whose methods should be forwarded.
   * @param mockDelegate The mockito mock of the delegate class.
   * @param forwarder The forwarder object that forwards to the mockDelegate.
   * @param skippedMethods A collection of methods that are skipped by the test.
   * @param argProvider provides argument to be passed to tested forwarding methods.
   */
  public static <T> void testMethodsForwarded(
      Class<T> delegateClass,
      T mockDelegate,
      T forwarder,
      Collection<Method> skippedMethods,
      ArgumentProvider argProvider) throws Exception {
    assertTrue(mockingDetails(mockDelegate).isMock());
    assertFalse(mockingDetails(forwarder).isMock());

    for (Method method : delegateClass.getDeclaredMethods()) {
      if (Modifier.isStatic(method.getModifiers())
          || Modifier.isPrivate(method.getModifiers())
          || skippedMethods.contains(method)) {
        continue;
      }
      Class<?>[] argTypes = method.getParameterTypes();
      Object[] args = new Object[argTypes.length];
      for (int i = 0; i < argTypes.length; i++) {
        if ((args[i] = argProvider.get(method, i, argTypes[i])) == null) {
          args[i] = Defaults.defaultValue(argTypes[i]);
        }
      }
      method.invoke(forwarder, args);
      try {
        method.invoke(verify(mockDelegate), args);
      } catch (InvocationTargetException e) {
        throw new AssertionError(String.format("Method was not forwarded: %s", method));
      }
    }

    boolean skipToString = false;
    for (Method method : skippedMethods) {
      if (method.getName().equals("toString")) {
        skipToString = true;
        break;
      }
    }
    if (!skipToString) {
      String actual = forwarder.toString();
      String expected =
          MoreObjects.toStringHelper(forwarder).add("delegate", mockDelegate).toString();
      assertEquals("Method toString() was not forwarded properly", expected, actual);
    }
  }

  /**
   * Provides arguments for forwarded methods tested in {@link #testMethodsForwarded}.
   */
  public interface ArgumentProvider {
    /**
     * Return an instance of the given class to be used as an argument passed to one method call.
     * If one method has multiple arguments with the same type, each occurrence will call this
     * method once.  It is recommended that each invocation returns a distinctive object for the
     * same type, in order to verify that arguments are passed by the tested class correctly.
     *
     * @return a value to be passed as an argument.  If {@code null}, {@link Default#defaultValue}
     *         will be used.
     */
    @Nullable Object get(Method method, int argPos, Class<?> clazz);
  }
}

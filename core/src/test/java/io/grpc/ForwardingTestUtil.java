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

/**
 * A util class to help test forwarding classes.
 */
public final class ForwardingTestUtil {
  /**
   * Use reflection to perform a basic sanity test. The forwarding class should forward all public
   * methods to the delegate, except for those in skippedMethods.
   * This does NOT verify that arguments or return values are forwarded properly. It only alerts
   * the developer if a forward method is missing.
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
        args[i] = Defaults.defaultValue(argTypes[i]);
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
}

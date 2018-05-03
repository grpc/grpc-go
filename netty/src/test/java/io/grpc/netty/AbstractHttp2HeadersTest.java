/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.netty;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;

import com.google.common.base.Defaults;
import io.netty.handler.codec.http2.Http2Headers;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AbstractHttp2Headers}. */
@RunWith(JUnit4.class)
public class AbstractHttp2HeadersTest {
  @Test
  public void allMethodsAreUnsupported() {
    Http2Headers headers = new AbstractHttp2Headers() {};
    for (Method method : Http2Headers.class.getMethods()) {
      // Avoid Java 8 default methods, without requiring Java 8 with isDefault()
      if (!Modifier.isAbstract(method.getModifiers())) {
        continue;
      }
      Class<?>[] params = method.getParameterTypes();
      Object[] args = new Object[params.length];
      for (int i = 0; i < params.length; i++) {
        args[i] = Defaults.defaultValue(params[i]);
      }
      try {
        method.invoke(headers, args);
        fail("Expected exception for method: " + method);
      } catch (InvocationTargetException ex) {
        assertEquals("For method: " + method,
            UnsupportedOperationException.class, ex.getCause().getClass());
      } catch (Exception ex) {
        throw new AssertionError("Failure with method: " + method, ex);
      }
    }
  }
}

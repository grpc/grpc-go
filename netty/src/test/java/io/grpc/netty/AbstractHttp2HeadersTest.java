/*
 * Copyright 2016, Google Inc. All rights reserved.
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
      } catch (Throwable t) {
        throw new RuntimeException("Failure with method: " + method, t);
      }
    }
  }
}

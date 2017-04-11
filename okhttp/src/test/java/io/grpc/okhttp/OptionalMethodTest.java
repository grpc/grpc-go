/*
 * Copyright 2017, Google Inc. All rights reserved.
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

package io.grpc.okhttp;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import io.grpc.okhttp.internal.OptionalMethod;
import java.lang.reflect.InvocationTargetException;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for OptionalMethod.
 */
@RunWith(JUnit4.class)
public class OptionalMethodTest {

  public static class DefaultClass {
    public String testMethod(String arg) {
      return arg;
    }
  }

  public abstract static class PublicParent {
    public abstract String testMethod(String arg);
  }

  private static class PrivateImpl extends PublicParent {
    @Override
    public String testMethod(String arg) {
      return arg;
    }
  }

  private static class PrivateClass {
    public String testMethod(String arg) {
      return arg;
    }
  }

  @Test
  public void isSupported() {
    OptionalMethod<DefaultClass> defaultClassMethod = new OptionalMethod<DefaultClass>(
        String.class, "testMethod", String.class);
    assertTrue(defaultClassMethod.isSupported(new DefaultClass()));

    OptionalMethod<PublicParent> privateImpl = new OptionalMethod<PublicParent>(
        String.class, "testMethod", String.class);
    assertTrue(privateImpl.isSupported(new PrivateImpl()));

    OptionalMethod<PrivateClass> privateClass = new OptionalMethod<PrivateClass>(
        String.class, "testMethod", String.class);
    assertFalse(privateClass.isSupported(new PrivateClass()));
  }

  @Test
  public void invokeOptional() throws InvocationTargetException {
    OptionalMethod<DefaultClass> defaultClassMethod = new OptionalMethod<DefaultClass>(
        String.class, "testMethod", String.class);
    assertEquals("testArg", defaultClassMethod.invokeOptional(new DefaultClass(), "testArg"));

    OptionalMethod<PublicParent> privateImpl = new OptionalMethod<PublicParent>(
        String.class, "testMethod", String.class);
    assertEquals("testArg", privateImpl.invokeOptional(new PrivateImpl(), "testArg"));

    OptionalMethod<PrivateClass> privateClass = new OptionalMethod<PrivateClass>(
        String.class, "testMethod", String.class);
    assertEquals(null, privateClass.invokeOptional(new PrivateClass(), "testArg"));
  }
}

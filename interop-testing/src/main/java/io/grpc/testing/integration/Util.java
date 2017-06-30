/*
 * Copyright 2014, gRPC Authors All rights reserved.
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

package io.grpc.testing.integration;

import com.google.protobuf.MessageLite;
import io.grpc.Metadata;
import io.grpc.protobuf.ProtoUtils;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.security.Provider;
import java.security.Security;
import java.util.List;
import org.junit.Assert;

/**
 * Utility methods to support integration testing.
 */
public class Util {

  public static final Metadata.Key<Messages.SimpleContext> METADATA_KEY =
      ProtoUtils.keyForProto(Messages.SimpleContext.getDefaultInstance());
  public static final Metadata.Key<String> ECHO_INITIAL_METADATA_KEY
      = Metadata.Key.of("x-grpc-test-echo-initial", Metadata.ASCII_STRING_MARSHALLER);
  public static final Metadata.Key<byte[]> ECHO_TRAILING_METADATA_KEY
      = Metadata.Key.of("x-grpc-test-echo-trailing-bin", Metadata.BINARY_BYTE_MARSHALLER);

  /** Assert that two messages are equal, producing a useful message if not. */
  public static void assertEquals(MessageLite expected, MessageLite actual) {
    if (expected == null || actual == null) {
      Assert.assertEquals(expected, actual);
    } else {
      if (!expected.equals(actual)) {
        // This assertEquals should always complete.
        Assert.assertEquals(expected.toString(), actual.toString());
        // But if it doesn't, then this should.
        Assert.assertEquals(expected, actual);
        Assert.fail("Messages not equal, but assertEquals didn't throw");
      }
    }
  }

  /** Assert that two lists of messages are equal, producing a useful message if not. */
  public static void assertEquals(List<? extends MessageLite> expected,
      List<? extends MessageLite> actual) {
    if (expected == null || actual == null) {
      Assert.assertEquals(expected, actual);
    } else if (expected.size() != actual.size()) {
      Assert.assertEquals(expected, actual);
    } else {
      for (int i = 0; i < expected.size(); i++) {
        assertEquals(expected.get(i), actual.get(i));
      }
    }
  }

  private static boolean conscryptInstallAttempted;

  /**
   * Add Conscrypt to the list of security providers, if it is available. If it appears to be
   * available but fails to load, this method will throw an exception. Since the list of security
   * providers is static, this method does nothing if the provider is not available or succeeded
   * previously.
   */
  public static void installConscryptIfAvailable() {
    if (conscryptInstallAttempted) {
      return;
    }
    Class<?> conscrypt;
    try {
      conscrypt = Class.forName("org.conscrypt.Conscrypt");
    } catch (ClassNotFoundException ex) {
      conscryptInstallAttempted = true;
      return;
    }
    Method newProvider;
    try {
      newProvider = conscrypt.getMethod("newProvider");
    } catch (NoSuchMethodException ex) {
      throw new RuntimeException("Could not find newProvider method on Conscrypt", ex);
    }
    Provider provider;
    try {
      provider = (Provider) newProvider.invoke(null);
    } catch (IllegalAccessException ex) {
      throw new RuntimeException("Could not invoke Conscrypt.newProvider", ex);
    } catch (InvocationTargetException ex) {
      throw new RuntimeException("Could not invoke Conscrypt.newProvider", ex);
    }
    Security.addProvider(provider);
    conscryptInstallAttempted = true;
  }
}

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

package io.grpc.auth;

import static com.google.common.base.Charsets.US_ASCII;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.auth.Credentials;
import com.google.auth.oauth2.AccessToken;
import com.google.auth.oauth2.OAuth2Credentials;
import com.google.auth.oauth2.ServiceAccountCredentials;
import com.google.common.collect.Iterables;
import com.google.common.collect.LinkedListMultimap;
import com.google.common.collect.ListMultimap;
import com.google.common.collect.Multimaps;
import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.CallCredentials.MetadataApplier;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import java.io.IOException;
import java.net.URI;
import java.security.KeyPair;
import java.security.KeyPairGenerator;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Date;
import java.util.List;
import java.util.concurrent.Executor;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Tests for {@link GoogleAuthLibraryCallCredentials}.
 */
@RunWith(JUnit4.class)
public class GoogleAuthLibraryCallCredentialsTest {

  private static final Metadata.Key<String> AUTHORIZATION = Metadata.Key.of("Authorization",
      Metadata.ASCII_STRING_MARSHALLER);
  private static final Metadata.Key<byte[]> EXTRA_AUTHORIZATION = Metadata.Key.of(
      "Extra-Authorization-bin", Metadata.BINARY_BYTE_MARSHALLER);

  @Mock
  private MethodDescriptor.Marshaller<String> stringMarshaller;

  @Mock
  private MethodDescriptor.Marshaller<Integer> intMarshaller;

  @Mock
  private Credentials credentials;

  @Mock
  private MetadataApplier applier;

  @Mock
  private Executor executor;

  @Captor
  private ArgumentCaptor<Metadata> headersCaptor;

  @Captor
  private ArgumentCaptor<Status> statusCaptor;

  private MethodDescriptor<String, Integer> method;
  private URI expectedUri;

  private final String authority = "testauthority";
  private final Attributes attrs = Attributes.newBuilder()
      .set(CallCredentials.ATTR_AUTHORITY, authority)
      .set(CallCredentials.ATTR_SECURITY_LEVEL, SecurityLevel.PRIVACY_AND_INTEGRITY)
      .build();

  private ArrayList<Runnable> pendingRunnables = new ArrayList<Runnable>();

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    method = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodDescriptor.MethodType.UNKNOWN)
        .setFullMethodName("a.service/method")
        .setRequestMarshaller(stringMarshaller)
        .setResponseMarshaller(intMarshaller)
        .build();
    expectedUri = new URI("https://testauthority/a.service");
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) {
          Runnable r = (Runnable) invocation.getArguments()[0];
          pendingRunnables.add(r);
          return null;
        }
      }).when(executor).execute(any(Runnable.class));
  }

  @Test
  public void copyCredentialsToHeaders() throws Exception {
    ListMultimap<String, String> values = LinkedListMultimap.create();
    values.put("Authorization", "token1");
    values.put("Authorization", "token2");
    values.put("Extra-Authorization-bin", "dG9rZW4z");  // bytes "token3" in base64
    values.put("Extra-Authorization-bin", "dG9rZW40");  // bytes "token4" in base64
    when(credentials.getRequestMetadata(eq(expectedUri))).thenReturn(Multimaps.asMap(values));

    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    assertEquals(1, runPendingRunnables());

    verify(credentials).getRequestMetadata(eq(expectedUri));
    verify(applier).apply(headersCaptor.capture());
    Metadata headers = headersCaptor.getValue();
    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    assertArrayEquals(new String[]{"token1", "token2"},
        Iterables.toArray(authorization, String.class));
    Iterable<byte[]> extraAuthorization = headers.getAll(EXTRA_AUTHORIZATION);
    assertEquals(2, Iterables.size(extraAuthorization));
    assertArrayEquals("token3".getBytes(US_ASCII), Iterables.get(extraAuthorization, 0));
    assertArrayEquals("token4".getBytes(US_ASCII), Iterables.get(extraAuthorization, 1));
  }

  @Test
  public void invalidBase64() throws Exception {
    ListMultimap<String, String> values = LinkedListMultimap.create();
    values.put("Extra-Authorization-bin", "dG9rZW4z1");  // invalid base64
    when(credentials.getRequestMetadata(eq(expectedUri))).thenReturn(Multimaps.asMap(values));

    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    assertEquals(1, runPendingRunnables());

    verify(credentials).getRequestMetadata(eq(expectedUri));
    verify(applier).fail(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNAUTHENTICATED, status.getCode());
    assertEquals(IllegalArgumentException.class, status.getCause().getClass());
  }

  @Test
  public void credentialsThrows() throws Exception {
    IOException exception = new IOException("Broken");
    when(credentials.getRequestMetadata(eq(expectedUri))).thenThrow(exception);

    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    assertEquals(1, runPendingRunnables());

    verify(credentials).getRequestMetadata(eq(expectedUri));
    verify(applier).fail(statusCaptor.capture());
    Status status = statusCaptor.getValue();
    assertEquals(Status.Code.UNAUTHENTICATED, status.getCode());
    assertEquals(exception, status.getCause());
  }

  @Test
  @SuppressWarnings("unchecked")
  public void credentialsReturnNullMetadata() throws Exception {
    ListMultimap<String, String> values = LinkedListMultimap.create();
    values.put("Authorization", "token1");
    when(credentials.getRequestMetadata(eq(expectedUri)))
        .thenReturn(null, Multimaps.<String, String>asMap(values), null);

    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    for (int i = 0; i < 3; i++) {
      callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    }

    assertEquals(3, runPendingRunnables());
    verify(credentials, times(3)).getRequestMetadata(eq(expectedUri));

    verify(applier, times(3)).apply(headersCaptor.capture());
    List<Metadata> headerList = headersCaptor.getAllValues();
    assertEquals(3, headerList.size());

    assertEquals(0, headerList.get(0).keys().size());

    Iterable<String> authorization = headerList.get(1).getAll(AUTHORIZATION);
    assertArrayEquals(new String[]{"token1"}, Iterables.toArray(authorization, String.class));

    assertEquals(0, headerList.get(2).keys().size());
  }

  @Test
  public void oauth2Credential() {
    final AccessToken token = new AccessToken("allyourbase", new Date(Long.MAX_VALUE));
    final OAuth2Credentials credentials = new OAuth2Credentials() {
      @Override
      public AccessToken refreshAccessToken() throws IOException {
        return token;
      }
    };

    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    assertEquals(1, runPendingRunnables());

    verify(applier).apply(headersCaptor.capture());
    Metadata headers = headersCaptor.getValue();
    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    assertArrayEquals(new String[]{"Bearer allyourbase"},
        Iterables.toArray(authorization, String.class));
  }

  @Test
  public void serviceUri() throws Exception {
    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    callCredentials.applyRequestMetadata(method,
        Attributes.newBuilder()
            .setAll(attrs)
            .set(CallCredentials.ATTR_AUTHORITY, "example.com:443")
            .build(),
        executor, applier);
    assertEquals(1, runPendingRunnables());
    verify(credentials).getRequestMetadata(eq(new URI("https://example.com/a.service")));

    callCredentials.applyRequestMetadata(method,
        Attributes.newBuilder()
            .setAll(attrs)
            .set(CallCredentials.ATTR_AUTHORITY, "example.com:123")
            .build(),
        executor, applier);
    assertEquals(1, runPendingRunnables());
    verify(credentials).getRequestMetadata(eq(new URI("https://example.com:123/a.service")));
  }

  @Test
  public void serviceAccountToJwt() throws Exception {
    KeyPair pair = KeyPairGenerator.getInstance("RSA").generateKeyPair();
    ServiceAccountCredentials credentials = new ServiceAccountCredentials(
        null, "email@example.com", pair.getPrivate(), null, null) {
      @Override
      public AccessToken refreshAccessToken() {
        throw new AssertionError();
      }
    };

    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    assertEquals(1, runPendingRunnables());

    verify(applier).apply(headersCaptor.capture());
    Metadata headers = headersCaptor.getValue();
    String[] authorization = Iterables.toArray(headers.getAll(AUTHORIZATION), String.class);
    assertEquals(1, authorization.length);
    assertTrue(authorization[0], authorization[0].startsWith("Bearer "));
    // JWT is reasonably long. Normal tokens aren't.
    assertTrue(authorization[0], authorization[0].length() > 300);
  }

  @Test
  public void serviceAccountWithScopeNotToJwt() throws Exception {
    final AccessToken token = new AccessToken("allyourbase", new Date(Long.MAX_VALUE));
    KeyPair pair = KeyPairGenerator.getInstance("RSA").generateKeyPair();
    ServiceAccountCredentials credentials = new ServiceAccountCredentials(
        null, "email@example.com", pair.getPrivate(), null, Arrays.asList("somescope")) {
      @Override
      public AccessToken refreshAccessToken() {
        return token;
      }
    };

    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials);
    callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    assertEquals(1, runPendingRunnables());

    verify(applier).apply(headersCaptor.capture());
    Metadata headers = headersCaptor.getValue();
    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    assertArrayEquals(new String[]{"Bearer allyourbase"},
        Iterables.toArray(authorization, String.class));
  }

  @Test
  public void oauthClassesNotInClassPath() throws Exception {
    ListMultimap<String, String> values = LinkedListMultimap.create();
    values.put("Authorization", "token1");
    when(credentials.getRequestMetadata(eq(expectedUri))).thenReturn(Multimaps.asMap(values));

    assertNull(GoogleAuthLibraryCallCredentials.createJwtHelperOrNull(null));
    GoogleAuthLibraryCallCredentials callCredentials =
        new GoogleAuthLibraryCallCredentials(credentials, null);
    callCredentials.applyRequestMetadata(method, attrs, executor, applier);
    assertEquals(1, runPendingRunnables());

    verify(credentials).getRequestMetadata(eq(expectedUri));
    verify(applier).apply(headersCaptor.capture());
    Metadata headers = headersCaptor.getValue();
    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    assertArrayEquals(new String[]{"token1"},
        Iterables.toArray(authorization, String.class));
  }

  private int runPendingRunnables() {
    ArrayList<Runnable> savedPendingRunnables = pendingRunnables;
    pendingRunnables = new ArrayList<Runnable>();
    for (Runnable r : savedPendingRunnables) {
      r.run();
    }
    return savedPendingRunnables.size();
  }
}

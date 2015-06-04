/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import static org.mockito.Mockito.isA;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.auth.Credentials;
import com.google.auth.oauth2.AccessToken;
import com.google.auth.oauth2.OAuth2Credentials;
import com.google.common.collect.Iterables;
import com.google.common.collect.LinkedListMultimap;
import com.google.common.collect.ListMultimap;
import com.google.common.collect.Multimaps;

import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;

import org.junit.Assert;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

import java.io.IOException;
import java.util.Date;
import java.util.concurrent.Executors;

/**
 * Tests for {@link ClientAuthInterceptor}.
 */
@RunWith(JUnit4.class)
public class ClientAuthInterceptorTests {

  public static final Metadata.Key<String> AUTHORIZATION = Metadata.Key.of("Authorization",
      Metadata.ASCII_STRING_MARSHALLER);
  public static final Metadata.Key<String> EXTRA_AUTHORIZATION = Metadata.Key.of(
      "Extra-Authorization", Metadata.ASCII_STRING_MARSHALLER);

  @Mock
  Credentials credentials;

  @Mock
  MethodDescriptor<String, Integer> descriptor;

  @Mock
  ClientCall.Listener<Integer> listener;

  @Mock
  Channel channel;

  @Mock
  ClientCall<String, Integer> call;

  ClientAuthInterceptor interceptor;

  /** Set up for test. */
  @Before
  public void startUp() throws IOException {
    MockitoAnnotations.initMocks(this);
    when(channel.newCall(descriptor)).thenReturn(call);
    interceptor = new ClientAuthInterceptor(credentials,
        Executors.newSingleThreadExecutor());
  }

  @Test
  public void testCopyCredentialToHeaders() throws IOException {
    ListMultimap<String, String> values = LinkedListMultimap.create();
    values.put("Authorization", "token1");
    values.put("Authorization", "token2");
    values.put("Extra-Authorization", "token3");
    values.put("Extra-Authorization", "token4");
    when(credentials.getRequestMetadata()).thenReturn(Multimaps.asMap(values));
    ClientCall<String, Integer> interceptedCall = interceptor.interceptCall(descriptor, channel);
    Metadata.Headers headers = new Metadata.Headers();
    interceptedCall.start(listener, headers);
    verify(call).start(listener, headers);

    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    Assert.assertArrayEquals(new String[]{"token1", "token2"},
        Iterables.toArray(authorization, String.class));
    Iterable<String> extraAuthorization = headers.getAll(EXTRA_AUTHORIZATION);
    Assert.assertArrayEquals(new String[]{"token3", "token4"},
        Iterables.toArray(extraAuthorization, String.class));
  }

  @Test
  public void testCredentialsThrows() throws IOException {
    when(credentials.getRequestMetadata()).thenThrow(new IOException("Broken"));
    ClientCall<String, Integer> interceptedCall = interceptor.interceptCall(descriptor, channel);
    Metadata.Headers headers = new Metadata.Headers();
    interceptedCall.start(listener, headers);
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    Mockito.verify(listener).onClose(statusCaptor.capture(), isA(Metadata.Trailers.class));
    Assert.assertNull(headers.getAll(AUTHORIZATION));
    Mockito.verify(call, never()).start(listener, headers);
    Assert.assertEquals(Status.Code.UNKNOWN, statusCaptor.getValue().getCode());
    Assert.assertNotNull(statusCaptor.getValue().getCause());
  }

  @Test
  public void testWithOAuth2Credential() throws IOException {
    final AccessToken token = new AccessToken("allyourbase", new Date(Long.MAX_VALUE));
    final OAuth2Credentials oAuth2Credentials = new OAuth2Credentials() {
      @Override
      public AccessToken refreshAccessToken() throws IOException {
        return token;
      }
    };
    interceptor = new ClientAuthInterceptor(oAuth2Credentials, Executors.newSingleThreadExecutor());
    ClientCall<String, Integer> interceptedCall = interceptor.interceptCall(descriptor, channel);
    Metadata.Headers headers = new Metadata.Headers();
    interceptedCall.start(listener, headers);
    verify(call).start(listener, headers);
    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    Assert.assertArrayEquals(new String[]{"Bearer allyourbase"},
        Iterables.toArray(authorization, String.class));
  }
}

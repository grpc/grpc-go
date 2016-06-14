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

import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.verify;

import com.google.auth.Credentials;

import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.util.concurrent.Executor;

/**
 * Tests for {@link ClientAuthInterceptor}.
 */
@RunWith(JUnit4.class)
@Deprecated
public class ClientAuthInterceptorTests {
  @Mock
  Executor executor;

  @Mock
  Credentials credentials;

  @Mock
  Marshaller<String> stringMarshaller;

  @Mock
  Marshaller<Integer> intMarshaller;

  MethodDescriptor<String, Integer> descriptor;

  @Mock
  Channel channel;

  @Captor
  ArgumentCaptor<CallOptions> callOptionsCaptor;

  ClientAuthInterceptor interceptor;

  /** Set up for test. */
  @Before
  public void startUp() {
    MockitoAnnotations.initMocks(this);
    descriptor = MethodDescriptor.create(
        MethodDescriptor.MethodType.UNKNOWN, "a.service/method", stringMarshaller, intMarshaller);
    interceptor = new ClientAuthInterceptor(credentials, executor);
  }

  @Test
  public void callCredentialsSet() throws Exception {
    ClientCall<String, Integer> interceptedCall =
        interceptor.interceptCall(descriptor, CallOptions.DEFAULT, channel);

    verify(channel).newCall(same(descriptor), callOptionsCaptor.capture());
    GoogleAuthLibraryCallCredentials callCredentials =
        (GoogleAuthLibraryCallCredentials) callOptionsCaptor.getValue().getCredentials();
    assertSame(credentials, callCredentials.creds);
  }
}

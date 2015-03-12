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

import io.grpc.Call;
import io.grpc.Channel;
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
  Call.Listener<Integer> listener;

  @Mock
  Channel channel;

  @Mock
  Call<String, Integer> call;

  ClientAuthInterceptor interceptor;

  @Before
  public void startup() throws IOException {
    MockitoAnnotations.initMocks(this);
    when(channel.newCall(descriptor)).thenReturn(call);
    interceptor = new ClientAuthInterceptor(credentials,
        Executors.newSingleThreadExecutor());
  }

  @Test
  @SuppressWarnings("unchecked")
  public void testCopyCredentialToHeaders() throws IOException {
    ListMultimap<String, String> values = LinkedListMultimap.create();
    values.put("Authorization", "token1");
    values.put("Authorization", "token2");
    values.put("Extra-Authorization", "token3");
    values.put("Extra-Authorization", "token4");
    when(credentials.getRequestMetadata()).thenReturn(Multimaps.asMap(values));
    Call interceptedCall = interceptor.interceptCall(descriptor, channel);
    Metadata.Headers headers = new Metadata.Headers();
    interceptedCall.start(listener, headers);
    verify(call).start(listener, headers);

    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    Assert.assertArrayEquals(new String[]{"token1", "token2"},
        Iterables.toArray(authorization, String.class));
    Iterable<String> extraAuthrization = headers.getAll(EXTRA_AUTHORIZATION);
    Assert.assertArrayEquals(new String[]{"token3", "token4"},
        Iterables.toArray(extraAuthrization, String.class));
  }

  @Test
  @SuppressWarnings("unchecked")
  public void testCredentialsThrows() throws IOException {
    when(credentials.getRequestMetadata()).thenThrow(new IOException("Broken"));
    Call interceptedCall = interceptor.interceptCall(descriptor, channel);
    Metadata.Headers headers = new Metadata.Headers();
    interceptedCall.start(listener, headers);
    ArgumentCaptor<Status> statusCaptor = ArgumentCaptor.forClass(Status.class);
    Mockito.verify(listener).onClose(statusCaptor.capture(), isA(Metadata.Trailers.class));
    Assert.assertNull(headers.getAll(AUTHORIZATION));
    Mockito.verify(call, never()).start(listener, headers);
    Assert.assertEquals(Status.Code.INTERNAL, statusCaptor.getValue().getCode());
    Assert.assertNotNull(statusCaptor.getValue().getCause());
  }

  @Test
  @SuppressWarnings("unchecked")
  public void testWithOAuth2Credential() throws IOException {
    final AccessToken token = new AccessToken("allyourbase", new Date(Long.MAX_VALUE));
    final OAuth2Credentials oAuth2Credentials = new OAuth2Credentials() {
      @Override
      public AccessToken refreshAccessToken() throws IOException {
        return token;
      }
    };
    interceptor = new ClientAuthInterceptor(oAuth2Credentials, Executors.newSingleThreadExecutor());
    Call<String, Integer> interceptedCall = interceptor.interceptCall(descriptor, channel);
    Metadata.Headers headers = new Metadata.Headers();
    interceptedCall.start(listener, headers);
    verify(call).start(listener, headers);
    Iterable<String> authorization = headers.getAll(AUTHORIZATION);
    Assert.assertArrayEquals(new String[]{"Bearer allyourbase"},
        Iterables.toArray(authorization, String.class));
  }
}

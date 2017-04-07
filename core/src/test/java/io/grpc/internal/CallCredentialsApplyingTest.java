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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.CallCredentials.MetadataApplier;
import io.grpc.CallOptions;
import io.grpc.IntegerMarshaller;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import io.grpc.StringMarshaller;
import java.net.SocketAddress;
import java.util.concurrent.Executor;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Unit test for {@link CallCredentials} applying functionality implemented by {@link
 * CallCredentialsApplyingTransportFactory} and {@link MetadataApplierImpl}.
 */
@RunWith(JUnit4.class)
public class CallCredentialsApplyingTest {
  @Mock
  private ClientTransportFactory mockTransportFactory;

  @Mock
  private ConnectionClientTransport mockTransport;

  @Mock
  private ClientStream mockStream;

  @Mock
  private CallCredentials mockCreds;

  @Mock
  private Executor mockExecutor;

  @Mock
  private SocketAddress address;

  private static final String AUTHORITY = "testauthority";
  private static final String USER_AGENT = "testuseragent";
  private static final Attributes.Key<String> ATTR_KEY = Attributes.Key.of("somekey");
  private static final String ATTR_VALUE = "somevalue";
  private static final MethodDescriptor<String, Integer> method =
      MethodDescriptor.<String, Integer>newBuilder()
          .setType(MethodDescriptor.MethodType.UNKNOWN)
          .setFullMethodName("/service/method")
          .setRequestMarshaller(new StringMarshaller())
          .setResponseMarshaller(new IntegerMarshaller())
          .build();
  private static final Metadata.Key<String> ORIG_HEADER_KEY =
      Metadata.Key.of("header1", Metadata.ASCII_STRING_MARSHALLER);
  private static final String ORIG_HEADER_VALUE = "some original header value";
  private static final Metadata.Key<String> CREDS_KEY =
      Metadata.Key.of("test-creds", Metadata.ASCII_STRING_MARSHALLER);
  private static final String CREDS_VALUE = "some credentials";

  private final Metadata origHeaders = new Metadata();
  private ForwardingConnectionClientTransport transport;
  private CallOptions callOptions;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    origHeaders.put(ORIG_HEADER_KEY, ORIG_HEADER_VALUE);
    when(mockTransportFactory.newClientTransport(address, AUTHORITY, USER_AGENT))
        .thenReturn(mockTransport);
    when(mockTransport.newStream(same(method), any(Metadata.class), any(CallOptions.class)))
        .thenReturn(mockStream);
    ClientTransportFactory transportFactory = new CallCredentialsApplyingTransportFactory(
        mockTransportFactory, mockExecutor);
    transport = (ForwardingConnectionClientTransport) transportFactory.newClientTransport(
        address, AUTHORITY, USER_AGENT);
    callOptions = CallOptions.DEFAULT.withCallCredentials(mockCreds);
    verify(mockTransportFactory).newClientTransport(address, AUTHORITY, USER_AGENT);
    assertSame(mockTransport, transport.delegate());
  }

  @Test
  public void parameterPropagation_base() {
    Attributes transportAttrs = Attributes.newBuilder().set(ATTR_KEY, ATTR_VALUE).build();
    when(mockTransport.getAttributes()).thenReturn(transportAttrs);

    transport.newStream(method, origHeaders, callOptions);

    ArgumentCaptor<Attributes> attrsCaptor = ArgumentCaptor.forClass(null);
    verify(mockCreds).applyRequestMetadata(same(method), attrsCaptor.capture(), same(mockExecutor),
        any(MetadataApplier.class));
    Attributes attrs = attrsCaptor.getValue();
    assertSame(ATTR_VALUE, attrs.get(ATTR_KEY));
    assertSame(AUTHORITY, attrs.get(CallCredentials.ATTR_AUTHORITY));
    assertSame(SecurityLevel.NONE, attrs.get(CallCredentials.ATTR_SECURITY_LEVEL));
  }

  @Test
  public void parameterPropagation_overrideByTransport() {
    Attributes transportAttrs = Attributes.newBuilder()
        .set(ATTR_KEY, ATTR_VALUE)
        .set(CallCredentials.ATTR_AUTHORITY, "transport-override-authority")
        .set(CallCredentials.ATTR_SECURITY_LEVEL, SecurityLevel.INTEGRITY)
        .build();
    when(mockTransport.getAttributes()).thenReturn(transportAttrs);

    transport.newStream(method, origHeaders, callOptions);

    ArgumentCaptor<Attributes> attrsCaptor = ArgumentCaptor.forClass(null);
    verify(mockCreds).applyRequestMetadata(same(method), attrsCaptor.capture(), same(mockExecutor),
        any(MetadataApplier.class));
    Attributes attrs = attrsCaptor.getValue();
    assertSame(ATTR_VALUE, attrs.get(ATTR_KEY));
    assertEquals("transport-override-authority", attrs.get(CallCredentials.ATTR_AUTHORITY));
    assertSame(SecurityLevel.INTEGRITY, attrs.get(CallCredentials.ATTR_SECURITY_LEVEL));
  }

  @Test
  public void parameterPropagation_overrideByCallOptions() {
    Attributes transportAttrs = Attributes.newBuilder()
        .set(ATTR_KEY, ATTR_VALUE)
        .set(CallCredentials.ATTR_AUTHORITY, "transport-override-authority")
        .set(CallCredentials.ATTR_SECURITY_LEVEL, SecurityLevel.INTEGRITY)
        .build();
    when(mockTransport.getAttributes()).thenReturn(transportAttrs);
    Executor anotherExecutor = mock(Executor.class);

    transport.newStream(method, origHeaders,
        callOptions.withAuthority("calloptions-authority").withExecutor(anotherExecutor));

    ArgumentCaptor<Attributes> attrsCaptor = ArgumentCaptor.forClass(null);
    verify(mockCreds).applyRequestMetadata(same(method), attrsCaptor.capture(),
        same(anotherExecutor), any(MetadataApplier.class));
    Attributes attrs = attrsCaptor.getValue();
    assertSame(ATTR_VALUE, attrs.get(ATTR_KEY));
    assertEquals("calloptions-authority", attrs.get(CallCredentials.ATTR_AUTHORITY));
    assertSame(SecurityLevel.INTEGRITY, attrs.get(CallCredentials.ATTR_SECURITY_LEVEL));
  }

  @Test
  public void applyMetadata_inline() {
    when(mockTransport.getAttributes()).thenReturn(Attributes.EMPTY);
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) throws Throwable {
          MetadataApplier applier = (MetadataApplier) invocation.getArguments()[3];
          Metadata headers = new Metadata();
          headers.put(CREDS_KEY, CREDS_VALUE);
          applier.apply(headers);
          return null;
        }
      }).when(mockCreds).applyRequestMetadata(same(method), any(Attributes.class),
          same(mockExecutor), any(MetadataApplier.class));

    ClientStream stream = transport.newStream(method, origHeaders, callOptions);

    verify(mockTransport).newStream(method, origHeaders, callOptions);
    assertSame(mockStream, stream);
    assertEquals(CREDS_VALUE, origHeaders.get(CREDS_KEY));
    assertEquals(ORIG_HEADER_VALUE, origHeaders.get(ORIG_HEADER_KEY));
  }

  @Test
  public void fail_inline() {
    final Status error = Status.FAILED_PRECONDITION.withDescription("channel not secure for creds");
    when(mockTransport.getAttributes()).thenReturn(Attributes.EMPTY);
    doAnswer(new Answer<Void>() {
        @Override
        public Void answer(InvocationOnMock invocation) throws Throwable {
          MetadataApplier applier = (MetadataApplier) invocation.getArguments()[3];
          applier.fail(error);
          return null;
        }
      }).when(mockCreds).applyRequestMetadata(same(method), any(Attributes.class),
          same(mockExecutor), any(MetadataApplier.class));

    FailingClientStream stream =
        (FailingClientStream) transport.newStream(method, origHeaders, callOptions);

    verify(mockTransport, never()).newStream(method, origHeaders, callOptions);
    assertSame(error, stream.getError());
  }

  @Test
  public void applyMetadata_delayed() {
    when(mockTransport.getAttributes()).thenReturn(Attributes.EMPTY);

    // Will call applyRequestMetadata(), which is no-op.
    DelayedStream stream = (DelayedStream) transport.newStream(method, origHeaders, callOptions);

    ArgumentCaptor<MetadataApplier> applierCaptor = ArgumentCaptor.forClass(null);
    verify(mockCreds).applyRequestMetadata(same(method), any(Attributes.class),
        same(mockExecutor), applierCaptor.capture());
    verify(mockTransport, never()).newStream(method, origHeaders, callOptions);

    Metadata headers = new Metadata();
    headers.put(CREDS_KEY, CREDS_VALUE);
    applierCaptor.getValue().apply(headers);

    verify(mockTransport).newStream(method, origHeaders, callOptions);
    assertSame(mockStream, stream.getRealStream());
    assertEquals(CREDS_VALUE, origHeaders.get(CREDS_KEY));
    assertEquals(ORIG_HEADER_VALUE, origHeaders.get(ORIG_HEADER_KEY));
  }

  @Test
  public void fail_delayed() {
    when(mockTransport.getAttributes()).thenReturn(Attributes.EMPTY);

    // Will call applyRequestMetadata(), which is no-op.
    DelayedStream stream = (DelayedStream) transport.newStream(method, origHeaders, callOptions);

    ArgumentCaptor<MetadataApplier> applierCaptor = ArgumentCaptor.forClass(null);
    verify(mockCreds).applyRequestMetadata(same(method), any(Attributes.class),
        same(mockExecutor), applierCaptor.capture());

    Status error = Status.FAILED_PRECONDITION.withDescription("channel not secure for creds");
    applierCaptor.getValue().fail(error);

    verify(mockTransport, never()).newStream(method, origHeaders, callOptions);
    FailingClientStream failingStream = (FailingClientStream) stream.getRealStream();
    assertSame(error, failingStream.getError());
  }

  @Test
  public void noCreds() {
    callOptions = callOptions.withCallCredentials(null);
    ClientStream stream = transport.newStream(method, origHeaders, callOptions);

    verify(mockTransport).newStream(method, origHeaders, callOptions);
    assertSame(mockStream, stream);
    assertNull(origHeaders.get(CREDS_KEY));
    assertEquals(ORIG_HEADER_VALUE, origHeaders.get(ORIG_HEADER_KEY));
  }
}

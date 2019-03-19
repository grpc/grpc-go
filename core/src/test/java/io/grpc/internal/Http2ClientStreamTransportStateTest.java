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

package io.grpc.internal;

import static com.google.common.base.Charsets.US_ASCII;
import static io.grpc.internal.ClientStreamListener.RpcProgress.PROCESSED;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.same;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.Status.Code;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.ArgumentMatchers;
import org.mockito.Captor;
import org.mockito.Mock;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.junit.MockitoJUnit;
import org.mockito.junit.MockitoRule;
import org.mockito.stubbing.Answer;

/** Unit tests for {@link Http2ClientStreamTransportState}. */
@RunWith(JUnit4.class)
public class Http2ClientStreamTransportStateTest {

  @Rule
  public final MockitoRule mocks = MockitoJUnit.rule();

  private final Metadata.Key<String> testStatusMashaller =
      InternalMetadata.keyOf(":status", Metadata.ASCII_STRING_MARSHALLER);

  private TransportTracer transportTracer;
  @Mock private ClientStreamListener mockListener;
  @Captor private ArgumentCaptor<Status> statusCaptor;

  @Before
  public void setUp() {
    transportTracer = new TransportTracer();

    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        StreamListener.MessageProducer producer =
            (StreamListener.MessageProducer) invocation.getArguments()[0];
        while (producer.next() != null) {}
        return null;
      }
    }).when(mockListener).messagesAvailable(ArgumentMatchers.<StreamListener.MessageProducer>any());
  }

  @Test
  public void transportHeadersReceived_notifiesListener() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportHeadersReceived(headers);

    verify(mockListener, never()).closed(any(Status.class), same(PROCESSED), any(Metadata.class));
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void transportHeadersReceived_doesntRequire200() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "500");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportHeadersReceived(headers);

    verify(mockListener, never()).closed(any(Status.class), same(PROCESSED), any(Metadata.class));
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void transportHeadersReceived_noHttpStatus() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportHeadersReceived(headers);
    state.transportDataReceived(ReadableBuffers.empty(), true);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(headers));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void transportHeadersReceived_wrongContentType_200() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER), "text/html");
    state.transportHeadersReceived(headers);
    state.transportDataReceived(ReadableBuffers.empty(), true);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(headers));
    assertEquals(Code.UNKNOWN, statusCaptor.getValue().getCode());
    assertTrue(statusCaptor.getValue().getDescription().contains("200"));
  }

  @Test
  public void transportHeadersReceived_wrongContentType_401() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "401");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER), "text/html");
    state.transportHeadersReceived(headers);
    state.transportDataReceived(ReadableBuffers.empty(), true);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(headers));
    assertEquals(Code.UNAUTHENTICATED, statusCaptor.getValue().getCode());
    assertTrue(statusCaptor.getValue().getDescription().contains("401"));
    assertTrue(statusCaptor.getValue().getDescription().contains("text/html"));
  }

  @Test
  public void transportHeadersReceived_handles_1xx() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);

    Metadata infoHeaders = new Metadata();
    infoHeaders.put(testStatusMashaller, "100");
    state.transportHeadersReceived(infoHeaders);
    Metadata infoHeaders2 = new Metadata();
    infoHeaders2.put(testStatusMashaller, "101");
    state.transportHeadersReceived(infoHeaders2);

    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportHeadersReceived(headers);

    verify(mockListener, never()).closed(any(Status.class), same(PROCESSED), any(Metadata.class));
    verify(mockListener).headersRead(headers);
  }

  @Test
  public void transportHeadersReceived_twice() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportHeadersReceived(headers);
    Metadata headersAgain = new Metadata();
    state.transportHeadersReceived(headersAgain);
    state.transportDataReceived(ReadableBuffers.empty(), true);

    verify(mockListener).headersRead(headers);
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(headersAgain));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
    assertTrue(statusCaptor.getValue().getDescription().contains("twice"));
  }

  @Test
  public void transportHeadersReceived_unknownAndTwiceLogsSecondHeaders() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER), "text/html");
    state.transportHeadersReceived(headers);
    Metadata headersAgain = new Metadata();
    String testString = "This is a test";
    headersAgain.put(Metadata.Key.of("key", Metadata.ASCII_STRING_MARSHALLER), testString);
    state.transportHeadersReceived(headersAgain);
    state.transportDataReceived(ReadableBuffers.empty(), true);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(headers));
    assertEquals(Code.UNKNOWN, statusCaptor.getValue().getCode());
    assertTrue(statusCaptor.getValue().getDescription().contains(testString));
  }

  @Test
  public void transportDataReceived_noHeaderReceived() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    String testString = "This is a test";
    state.transportDataReceived(ReadableBuffers.wrap(testString.getBytes(US_ASCII)), true);

    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), any(Metadata.class));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void transportDataReceived_debugData() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER), "text/html");
    state.transportHeadersReceived(headers);
    String testString = "This is a test";
    state.transportDataReceived(ReadableBuffers.wrap(testString.getBytes(US_ASCII)), true);

    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(headers));
    assertTrue(statusCaptor.getValue().getDescription().contains(testString));
  }

  @Test
  public void transportTrailersReceived_notifiesListener() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata trailers = new Metadata();
    trailers.put(testStatusMashaller, "200");
    trailers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    trailers.put(Metadata.Key.of("grpc-status", Metadata.ASCII_STRING_MARSHALLER), "0");
    state.transportTrailersReceived(trailers);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(Status.OK, PROCESSED, trailers);
  }

  @Test
  public void transportTrailersReceived_afterHeaders() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportHeadersReceived(headers);
    Metadata trailers = new Metadata();
    trailers.put(Metadata.Key.of("grpc-status", Metadata.ASCII_STRING_MARSHALLER), "0");
    state.transportTrailersReceived(trailers);

    verify(mockListener).headersRead(headers);
    verify(mockListener).closed(Status.OK, PROCESSED, trailers);
  }

  @Test
  public void transportTrailersReceived_observesStatus() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata trailers = new Metadata();
    trailers.put(testStatusMashaller, "200");
    trailers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    trailers.put(Metadata.Key.of("grpc-status", Metadata.ASCII_STRING_MARSHALLER), "1");
    state.transportTrailersReceived(trailers);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(Status.CANCELLED, PROCESSED, trailers);
  }

  @Test
  public void transportTrailersReceived_missingStatusUsesHttpStatus() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata trailers = new Metadata();
    trailers.put(testStatusMashaller, "401");
    trailers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportTrailersReceived(trailers);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(trailers));
    assertEquals(Code.UNAUTHENTICATED, statusCaptor.getValue().getCode());
    assertTrue(statusCaptor.getValue().getDescription().contains("401"));
  }

  @Test
  public void transportTrailersReceived_missingHttpStatus() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata trailers = new Metadata();
    trailers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    trailers.put(Metadata.Key.of("grpc-status", Metadata.ASCII_STRING_MARSHALLER), "0");
    state.transportTrailersReceived(trailers);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(trailers));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void transportTrailersReceived_missingStatusAndMissingHttpStatus() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata trailers = new Metadata();
    trailers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportTrailersReceived(trailers);

    verify(mockListener, never()).headersRead(any(Metadata.class));
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(trailers));
    assertEquals(Code.INTERNAL, statusCaptor.getValue().getCode());
  }

  @Test
  public void transportTrailersReceived_missingStatusAfterHeadersIgnoresHttpStatus() {
    BaseTransportState state = new BaseTransportState(transportTracer);
    state.setListener(mockListener);
    Metadata headers = new Metadata();
    headers.put(testStatusMashaller, "200");
    headers.put(Metadata.Key.of("content-type", Metadata.ASCII_STRING_MARSHALLER),
        "application/grpc");
    state.transportHeadersReceived(headers);
    Metadata trailers = new Metadata();
    trailers.put(testStatusMashaller, "401");
    state.transportTrailersReceived(trailers);

    verify(mockListener).headersRead(headers);
    verify(mockListener).closed(statusCaptor.capture(), same(PROCESSED), same(trailers));
    assertEquals(Code.UNKNOWN, statusCaptor.getValue().getCode());
  }

  private static class BaseTransportState extends Http2ClientStreamTransportState {
    public BaseTransportState(TransportTracer transportTracer) {
      super(DEFAULT_MAX_MESSAGE_SIZE, StatsTraceContext.NOOP, transportTracer);
    }

    @Override
    protected void http2ProcessingFailed(Status status, boolean stopDelivery, Metadata trailers) {
      transportReportStatus(status, stopDelivery, trailers);
    }

    @Override
    public void deframeFailed(Throwable cause) {}

    @Override
    public void bytesRead(int processedBytes) {}

    @Override
    public void runOnTransportThread(Runnable r) {
      r.run();
    }
  }
}

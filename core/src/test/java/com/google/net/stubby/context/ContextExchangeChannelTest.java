package com.google.net.stubby.context;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.mockito.Mockito.*;

import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.Call;
import com.google.net.stubby.Channel;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.Status;
import com.google.net.stubby.stub.Marshallers;
import com.google.net.stubby.testing.integration.Test.Payload;
import com.google.net.stubby.testing.integration.Test.PayloadType;
import com.google.net.stubby.testing.integration.Test.SimpleRequest;
import com.google.net.stubby.testing.integration.Test.SimpleResponse;
import com.google.net.stubby.testing.integration.TestServiceGrpc;
import com.google.protobuf.ByteString;

import org.hamcrest.BaseMatcher;
import org.hamcrest.Description;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

import javax.inject.Provider;

/**
 * Tests for {@link ContextExchangeChannel}
 */
@RunWith(JUnit4.class)
public class ContextExchangeChannelTest {

  private static final SimpleRequest REQ = SimpleRequest.newBuilder().setPayload(
      Payload.newBuilder().setPayloadCompressable("mary").
          setPayloadType(PayloadType.COMPRESSABLE).build())
      .build();

  private static final SimpleResponse RESP = SimpleResponse.newBuilder().setPayload(
      Payload.newBuilder().setPayloadCompressable("bob").
          setPayloadType(PayloadType.COMPRESSABLE).build())
      .build();

  @Mock
  Channel channel;

  @Mock
  Call call;

  @Before @SuppressWarnings("unchecked")
  public void setup() {
    MockitoAnnotations.initMocks(this);
    when(channel.newCall(Mockito.any(MethodDescriptor.class))).thenReturn(call);
  }

  @Test
  public void testReceive() throws Exception {
    ContextExchangeChannel exchange = new ContextExchangeChannel(channel);
    Provider<SimpleResponse> auth =
        exchange.receive("auth", Marshallers.forProto(SimpleResponse.PARSER));
    // Should be null, nothing has happened
    assertNull(auth.get());
    TestServiceGrpc.TestServiceBlockingStub stub =
        TestServiceGrpc.newBlockingStub(exchange);
    callStub(stub);
    assertEquals(RESP, auth.get());
    exchange.clearLastReceived();
    assertNull(auth.get());
  }

  @Test @SuppressWarnings("unchecked")
  public void testSend() throws Exception {
    ContextExchangeChannel exchange = new ContextExchangeChannel(channel);
    exchange.send("auth", RESP, Marshallers.forProto(SimpleResponse.PARSER));
    TestServiceGrpc.TestServiceBlockingStub stub =
        TestServiceGrpc.newBlockingStub(exchange);
    callStub(stub);
    verify(call).sendContext(eq("auth"),
        argThat(new BaseMatcher<InputStream>() {
          @Override
          public boolean matches(Object o) {
            try {
              // Just check the length, consuming the stream will fail the test and Mockito
              // calls this more than once.
              return ((InputStream) o).available() == RESP.getSerializedSize();
            } catch (IOException ioe) {
              throw new RuntimeException(ioe);
            }
          }

          @Override
          public void describeTo(Description description) {
          }
        }), (SettableFuture<Void>) isNull());
  }

  @SuppressWarnings("unchecked")
  private void callStub(final TestServiceGrpc.TestServiceBlockingStub stub) throws Exception {
    when(channel.newCall(Mockito.<MethodDescriptor>any())).thenReturn(call);

    // execute the call in another thread so we don't deadlock waiting for the
    // listener.onClose
    Future<?> pending = Executors.newSingleThreadExecutor().submit(new Runnable() {
      @Override
      public void run() {
        stub.unaryCall(REQ);
      }
    });
    ArgumentCaptor<Call.Listener> listenerCapture = ArgumentCaptor.forClass(Call.Listener.class);
    // Wait for the call to start to capture the listener
    verify(call, timeout(1000)).start(listenerCapture.capture());

    ByteString response = RESP.toByteString();
    Call.Listener listener = listenerCapture.getValue();
    // Respond with a context-value
    listener.onContext("auth", response.newInput());
    listener.onContext("something-else", response.newInput());
    // .. and single payload
    listener.onPayload(RESP);
    listener.onClose(Status.OK);
    pending.get();
  }
}

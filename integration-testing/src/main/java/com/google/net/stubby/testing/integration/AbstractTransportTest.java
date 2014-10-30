package com.google.net.stubby.testing.integration;

import static com.google.net.stubby.testing.integration.Messages.PayloadType.COMPRESSABLE;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import com.google.common.util.concurrent.Uninterruptibles;
import com.google.net.stubby.Call;
import com.google.net.stubby.ChannelImpl;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.proto.ProtoUtils;
import com.google.net.stubby.stub.MetadataUtils;
import com.google.net.stubby.stub.StreamObserver;
import com.google.net.stubby.stub.StreamRecorder;
import com.google.net.stubby.testing.integration.Messages.PayloadType;
import com.google.net.stubby.testing.integration.Messages.SimpleRequest;
import com.google.net.stubby.testing.integration.Messages.SimpleResponse;
import com.google.net.stubby.testing.integration.Messages.StreamingInputCallRequest;
import com.google.net.stubby.testing.integration.Messages.StreamingInputCallResponse;
import com.google.net.stubby.testing.integration.Messages.StreamingOutputCallRequest;
import com.google.net.stubby.testing.integration.Messages.StreamingOutputCallResponse;
import com.google.net.stubby.transport.AbstractStream;
import com.google.protobuf.ByteString;
import com.google.protobuf.EmptyProtos.Empty;

import org.junit.After;
import org.junit.Assert;
import org.junit.Assume;
import org.junit.Before;
import org.junit.Test;

import java.util.Arrays;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedList;
import java.util.List;
import java.util.Random;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Abstract base class for all GRPC transport tests.
 */
public abstract class AbstractTransportTest {

  public static final Metadata.Key<Messages.SimpleContext> METADATA_KEY =
      ProtoUtils.keyForProto(Messages.SimpleContext.getDefaultInstance());

  protected ChannelImpl channel;
  protected TestServiceGrpc.TestServiceBlockingStub blockingStub;
  protected TestServiceGrpc.TestService asyncStub;

  /**
   * Must be called by the subclass setup method.
   */
  @Before
  public void setup() throws Exception {
    channel = createChannel();
    channel.startAsync();
    channel.awaitRunning();
    blockingStub = TestServiceGrpc.newBlockingStub(channel);
    asyncStub = TestServiceGrpc.newStub(channel);
  }

  @After
  public void teardown() throws Exception {
    if (channel != null) {
      channel.stopAsync();
    }
  }

  protected abstract ChannelImpl createChannel();

  @Test
  public void emptyShouldSucceed() throws Exception {
    Empty response = blockingStub.emptyCall(Empty.getDefaultInstance());
    assertNotNull(response);
  }

  @Test
  public void unaryCallShouldSucceed() throws Exception {
    // Create the request.
    SimpleResponse response = blockingStub.unaryCall(unaryRequest());
    assertNotNull(response);
    assertEquals(COMPRESSABLE, response.getPayload().getType());
    assertEquals(10, response.getPayload().getBody().size());
  }

  @Test
  public void streamingOutputCallShouldSucceed() throws Exception {
    // Build the request.
    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    StreamingOutputCallRequest.Builder streamingOutputBuilder =
        StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    StreamingOutputCallRequest request = streamingOutputBuilder.build();

    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    asyncStub.streamingOutputCall(request, recorder);
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(responseSizes.size(), recorder.getValues().size());
    for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
      StreamingOutputCallResponse response = recorder.getValues().get(ix);
      assertEquals(COMPRESSABLE, response.getPayload().getType());
      int length = response.getPayload().getBody().size();
      assertEquals("comparison failed at index " + ix, responseSizes.get(ix).intValue(), length);
    }
  }

  @Test
  public void streamingInputCallShouldSucceed() throws Exception {
    // Build the request.
    String message = "hello world!";
    StreamingInputCallRequest.Builder streamingInputBuilder =
        StreamingInputCallRequest.newBuilder();
    streamingInputBuilder.getPayloadBuilder().setType(PayloadType.COMPRESSABLE)
        .setBody(ByteString.copyFromUtf8(message));
    final StreamingInputCallRequest request = streamingInputBuilder.build();

    StreamRecorder<StreamingInputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<StreamingInputCallRequest> requestStream =
        asyncStub.streamingInputCall(recorder);
    for (int ix = 10; ix > 0; --ix) {
      // Send the request and close the request stream.
      requestStream.onValue(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(1, recorder.getValues().size());
    assertEquals(10 * message.length(), recorder.getValues().get(0).getAggregatedPayloadSize());
  }

  @Test
  public void fullDuplexCallShouldSucceed() throws Exception {
    // Build the request.
    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    StreamingOutputCallRequest.Builder streamingOutputBuilder =
        StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    final StreamingOutputCallRequest request = streamingOutputBuilder.build();

    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<StreamingOutputCallRequest> requestStream =
        blockingStub.fullDuplexCall(recorder);

    final int numRequests = 10;
    for (int ix = numRequests; ix > 0; --ix) {
      requestStream.onValue(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(responseSizes.size() * numRequests, recorder.getValues().size());
    for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
      StreamingOutputCallResponse response = recorder.getValues().get(ix);
      assertEquals(COMPRESSABLE, response.getPayload().getType());
      int length = response.getPayload().getBody().size();
      int expectedSize = responseSizes.get(ix % responseSizes.size());
      assertEquals("comparison failed at index " + ix, expectedSize, length);
    }
  }

  @Test
  public void halfDuplexCallShouldSucceed() throws Exception {
    // Build the request.
    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    StreamingOutputCallRequest.Builder streamingOutputBuilder =
        StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    final StreamingOutputCallRequest request = streamingOutputBuilder.build();

    StreamRecorder<StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<StreamingOutputCallRequest> requestStream = asyncStub.halfDuplexCall(recorder);

    final int numRequests = 10;
    for (int ix = numRequests; ix > 0; --ix) {
      requestStream.onValue(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    assertEquals(responseSizes.size() * numRequests, recorder.getValues().size());
    for (int ix = 0; ix < recorder.getValues().size(); ++ix) {
      StreamingOutputCallResponse response = recorder.getValues().get(ix);
      assertEquals(COMPRESSABLE, response.getPayload().getType());
      int length = response.getPayload().getBody().size();
      int expectedSize = responseSizes.get(ix % responseSizes.size());
      assertEquals("comparison failed at index " + ix, expectedSize, length);
    }
  }

  @Test
  public void streamingOutputShouldBeFlowControlled() throws Exception {
    // Create the call object.
    Call<StreamingOutputCallRequest, StreamingOutputCallResponse> call =
        channel.newCall(TestServiceGrpc.CONFIG.streamingOutputCall);

    // Build the request.
    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    StreamingOutputCallRequest.Builder streamingOutputBuilder =
        StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    StreamingOutputCallRequest request = streamingOutputBuilder.build();

    // Start the call and prepare capture of results.
    final List<StreamingOutputCallResponse> results =
        Collections.synchronizedList(new ArrayList<StreamingOutputCallResponse>());
    final List<SettableFuture<Void>> processedFutures =
        Collections.synchronizedList(new LinkedList<SettableFuture<Void>>());
    final SettableFuture<Void> completionFuture = SettableFuture.create();
    call.start(new Call.Listener<StreamingOutputCallResponse>() {

      @Override
      public ListenableFuture<Void> onHeaders(Metadata.Headers headers) {
        return null;
      }

      @Override
      public ListenableFuture<Void> onPayload(final StreamingOutputCallResponse payload) {
        SettableFuture<Void> processedFuture = SettableFuture.create();
        results.add(payload);
        processedFutures.add(processedFuture);
        return processedFuture;
      }

      @Override
      public void onClose(Status status, Metadata.Trailers trailers) {
        if (status.isOk()) {
          completionFuture.set(null);
        } else {
          completionFuture.setException(status.asException());
        }
      }
    }, new Metadata.Headers());

    // Send the request.
    call.sendPayload(request);
    call.halfClose();

    // Slowly set completion on all of the futures.
    int expectedResults = responseSizes.size();
    int count = 0;
    while (count < expectedResults) {
      if (!processedFutures.isEmpty()) {
        assertEquals(1, processedFutures.size());
        assertEquals(count + 1, results.size());
        count++;

        // Remove and set the first future to allow receipt of additional messages
        // from flow control.
        processedFutures.remove(0).set(null);
      }

      // Sleep a bit.
      Uninterruptibles.sleepUninterruptibly(1, TimeUnit.SECONDS);
    }

    // Wait for successful completion of the response.
    completionFuture.get();

    assertEquals(responseSizes.size(), results.size());
    for (int ix = 0; ix < results.size(); ++ix) {
      StreamingOutputCallResponse response = results.get(ix);
      assertEquals(COMPRESSABLE, response.getPayload().getType());
      int length = response.getPayload().getBody().size();
      assertEquals("comparison failed at index " + ix, responseSizes.get(ix).intValue(), length);
    }
  }

  @org.junit.Test
  public void exchangeContextUnaryCall() throws Exception {
    Assume.assumeTrue(AbstractStream.GRPC_V2_PROTOCOL);
    TestServiceGrpc.TestServiceBlockingStub stub =
        TestServiceGrpc.newBlockingStub(channel);

    // Capture the context exchange
    Metadata.Headers fixedHeaders = new Metadata.Headers();
    // Send a context proto (as it's in the default extension registry)
    Messages.SimpleContext contextValue =
        Messages.SimpleContext.newBuilder().setValue("dog").build();
    fixedHeaders.put(METADATA_KEY, contextValue);
    stub = MetadataUtils.attachHeaders(stub, fixedHeaders);
    // .. and expect it to be echoed back in trailers
    AtomicReference<Metadata.Trailers> trailersCapture = new AtomicReference<>();
    AtomicReference<Metadata.Headers> headersCapture = new AtomicReference<>();
    stub = MetadataUtils.captureMetadata(stub, headersCapture, trailersCapture);

    Assert.assertNotNull(stub.unaryCall(unaryRequest()));

    // Assert that our side channel object is echoed back in both headers and trailers
    Assert.assertEquals(contextValue, headersCapture.get().get(METADATA_KEY));
    if (AbstractStream.GRPC_V2_PROTOCOL) {
      Assert.assertEquals(contextValue, trailersCapture.get().get(METADATA_KEY));
    }
  }

  @org.junit.Test
  public void exchangeContextStreamingCall() throws Exception {
    Assume.assumeTrue(AbstractStream.GRPC_V2_PROTOCOL);
    TestServiceGrpc.TestServiceBlockingStub stub =
        TestServiceGrpc.newBlockingStub(channel);

    // Capture the context exchange
    Metadata.Headers fixedHeaders = new Metadata.Headers();
    // Send a context proto (as it's in the default extension registry)
    Messages.SimpleContext contextValue =
        Messages.SimpleContext.newBuilder().setValue("dog").build();
    fixedHeaders.put(METADATA_KEY, contextValue);
    stub = MetadataUtils.attachHeaders(stub, fixedHeaders);
    // .. and expect it to be echoed back in trailers
    AtomicReference<Metadata.Trailers> trailersCapture = new AtomicReference<>();
    AtomicReference<Metadata.Headers> headersCapture = new AtomicReference<>();
    stub = MetadataUtils.captureMetadata(stub, headersCapture, trailersCapture);

    List<Integer> responseSizes = Arrays.asList(50, 100, 150, 200);
    Messages.StreamingOutputCallRequest.Builder streamingOutputBuilder =
        Messages.StreamingOutputCallRequest.newBuilder();
    streamingOutputBuilder.setResponseType(COMPRESSABLE);
    for (Integer size : responseSizes) {
      streamingOutputBuilder.addResponseParametersBuilder().setSize(size).setIntervalUs(0);
    }
    final Messages.StreamingOutputCallRequest request = streamingOutputBuilder.build();

    StreamRecorder<Messages.StreamingOutputCallResponse> recorder = StreamRecorder.create();
    StreamObserver<Messages.StreamingOutputCallRequest> requestStream =
        stub.fullDuplexCall(recorder);

    final int numRequests = 10;
    for (int ix = numRequests; ix > 0; --ix) {
      requestStream.onValue(request);
    }
    requestStream.onCompleted();
    recorder.awaitCompletion();
    assertSuccess(recorder);
    org.junit.Assert.assertEquals(responseSizes.size() * numRequests, recorder.getValues().size());

    // Assert that our side channel object is echoed back in both headers and trailers
    Assert.assertEquals(contextValue, headersCapture.get().get(METADATA_KEY));
    if (AbstractStream.GRPC_V2_PROTOCOL) {
      Assert.assertEquals(contextValue, trailersCapture.get().get(METADATA_KEY));
    }
  }


  protected int unaryPayloadLength() {
    // 10MiB.
    return 10485760;
  }

  protected SimpleRequest unaryRequest() {
    SimpleRequest.Builder unaryBuilder = SimpleRequest.newBuilder();
    unaryBuilder.getPayloadBuilder().setType(PayloadType.COMPRESSABLE);
    byte[] data = new byte[unaryPayloadLength()];
    new Random().nextBytes(data);
    unaryBuilder.getPayloadBuilder().setBody(ByteString.copyFrom(data));
    unaryBuilder.setResponseSize(10).setResponseType(PayloadType.COMPRESSABLE);
    return unaryBuilder.build();
  }

  protected static void assertSuccess(StreamRecorder<?> recorder) {
    if (recorder.getError() != null) {
      throw new AssertionError("Error in stream", recorder.getError());
    }
  }
}

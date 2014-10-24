package com.google.net.stubby;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isNull;
import static org.mockito.Matchers.notNull;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.AbstractService;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.Service;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.newtransport.ServerStream;
import com.google.net.stubby.newtransport.ServerStreamListener;
import com.google.net.stubby.newtransport.ServerTransportListener;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.BrokenBarrierException;
import java.util.concurrent.CyclicBarrier;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicReference;

/** Unit tests for {@link ServerImpl}. */
@RunWith(JUnit4.class)
public class ServerImplTest {
  private static final IntegerMarshaller INTEGER_MARSHALLER = new IntegerMarshaller();
  private static final StringMarshaller STRING_MARSHALLER = new StringMarshaller();

  private ExecutorService executor = Executors.newSingleThreadExecutor();
  private MutableHandlerRegistry registry = new MutableHandlerRegistryImpl();
  private Service transportServer = new NoopService();
  private ServerImpl server = new ServerImpl(executor, registry)
      .setTransportServer(transportServer);
  private ServerStream stream = Mockito.mock(ServerStream.class);

  @Mock
  private ServerCall.Listener<String> callListener;

  @Before
  public void startup() {
    MockitoAnnotations.initMocks(this);

    server.startAsync();
    server.awaitRunning();
  }

  @After
  public void teardown() {
    executor.shutdownNow();
  }

  @Test
  public void startStopImmediate() {
    Service transportServer = new NoopService();
    Server server = new ServerImpl(executor, registry).setTransportServer(transportServer);
    assertEquals(Service.State.NEW, server.state());
    assertEquals(Service.State.NEW, transportServer.state());
    server.startAsync();
    server.awaitRunning();
    assertEquals(Service.State.RUNNING, server.state());
    assertEquals(Service.State.RUNNING, transportServer.state());
    server.stopAsync();
    server.awaitTerminated();
    assertEquals(Service.State.TERMINATED, server.state());
    assertEquals(Service.State.TERMINATED, transportServer.state());
  }

  @Test
  public void transportServerFailureFailsServer() {
    class FailableService extends NoopService {
      public void doNotifyFailed(Throwable cause) {
        notifyFailed(cause);
      }
    }
    FailableService transportServer = new FailableService();
    Server server = new ServerImpl(executor, registry).setTransportServer(transportServer);
    server.startAsync();
    server.awaitRunning();
    RuntimeException ex = new RuntimeException("force failure");
    transportServer.doNotifyFailed(ex);
    assertEquals(Service.State.FAILED, server.state());
    assertEquals(ex, server.failureCause());
  }

  @Test
  public void transportServerFailsStartup() {
    class FailingStartupService extends NoopService {
      @Override
      public void doStart() {
        notifyFailed(new RuntimeException());
      }
    }
    FailingStartupService transportServer = new FailingStartupService();
    Server server = new ServerImpl(executor, registry).setTransportServer(transportServer);
    server.startAsync();
    assertEquals(Service.State.FAILED, server.state());
  }

  @Test
  public void transportServerFirstToShutdown() {
    class ManualStoppedService extends NoopService {
      public void doNotifyStopped() {
        notifyStopped();
      }

      @Override
      public void doStop() {} // Don't notify.
    }
    NoopService transportServer = new NoopService();
    ServerImpl server = new ServerImpl(executor, registry).setTransportServer(transportServer);
    server.startAsync();
    server.awaitRunning();
    ManualStoppedService transport = new ManualStoppedService();
    transport.startAsync();
    server.serverListener().transportCreated(transport);
    server.stopAsync();
    assertEquals(Service.State.STOPPING, transport.state());
    assertEquals(Service.State.TERMINATED, transportServer.state());
    assertEquals(Service.State.STOPPING, server.state());

    transport.doNotifyStopped();
    assertEquals(Service.State.TERMINATED, transport.state());
    assertEquals(Service.State.TERMINATED, server.state());
  }

  @Test
  public void transportServerLastToShutdown() {
    class ManualStoppedService extends NoopService {
      public void doNotifyStopped() {
        notifyStopped();
      }

      @Override
      public void doStop() {} // Don't notify.
    }
    ManualStoppedService transportServer = new ManualStoppedService();
    ServerImpl server = new ServerImpl(executor, registry).setTransportServer(transportServer);
    server.startAsync();
    server.awaitRunning();
    Service transport = new NoopService();
    transport.startAsync();
    server.serverListener().transportCreated(transport);
    server.stopAsync();
    assertEquals(Service.State.TERMINATED, transport.state());
    assertEquals(Service.State.STOPPING, transportServer.state());
    assertEquals(Service.State.STOPPING, server.state());

    transportServer.doNotifyStopped();
    assertEquals(Service.State.TERMINATED, transportServer.state());
    assertEquals(Service.State.TERMINATED, server.state());
  }

  @Test
  public void basicExchangeSuccessful() throws Exception {
    final Metadata.Key<Integer> metadataKey
        = Metadata.Key.of("inception", Metadata.INTEGER_MARSHALLER);
    final AtomicReference<ServerCall<Integer>> callReference
        = new AtomicReference<ServerCall<Integer>>();
    registry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod("serve", STRING_MARSHALLER, INTEGER_MARSHALLER,
          new ServerCallHandler<String, Integer>() {
            @Override
            public ServerCall.Listener<String> startCall(String fullMethodName,
                ServerCall<Integer> call, Metadata.Headers headers) {
              assertEquals("/Waiter/serve", fullMethodName);
              assertNotNull(call);
              assertNotNull(headers);
              assertEquals(0, headers.get(metadataKey).intValue());
              callReference.set(call);
              return callListener;
            }
          }).build());
    ServerTransportListener transportListener = newTransport(server);

    Metadata.Headers headers = new Metadata.Headers();
    headers.put(metadataKey, 0);
    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "/Waiter/serve", headers);
    assertNotNull(streamListener);

    executeBarrier(executor).await();
    ServerCall<Integer> call = callReference.get();
    assertNotNull(call);

    String order = "Lots of pizza, please";
    ListenableFuture<Void> future = streamListener.messageRead(STRING_MARSHALLER.stream(order), 1);
    future.get();
    verify(callListener).onPayload(order);

    call.sendPayload(314);
    ArgumentCaptor<InputStream> inputCaptor = ArgumentCaptor.forClass(InputStream.class);
    verify(stream).writeMessage(inputCaptor.capture(), eq(3), isNull(Runnable.class));
    verify(stream).flush();
    assertEquals(314, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());

    streamListener.halfClosed(); // All full; no dessert.
    executeBarrier(executor).await();
    verify(callListener).onHalfClose();

    call.sendPayload(50);
    verify(stream).writeMessage(inputCaptor.capture(), eq(2), isNull(Runnable.class));
    verify(stream, times(2)).flush();
    assertEquals(50, INTEGER_MARSHALLER.parse(inputCaptor.getValue()).intValue());

    Metadata.Trailers trailers = new Metadata.Trailers();
    trailers.put(metadataKey, 3);
    Status status = Status.OK.withDescription("A okay");
    call.close(status, trailers);
    verify(stream).close(status, trailers);

    streamListener.closed(Status.OK);
    executeBarrier(executor).await();
    verify(callListener).onComplete();

    verifyNoMoreInteractions(stream);
    verifyNoMoreInteractions(callListener);
  }

  @Test
  public void exceptionInStartCallPropagatesToStream() throws Exception {
    CyclicBarrier barrier = executeBarrier(executor);
    final Status status = Status.ABORTED.withDescription("Oh, no!");
    registry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod("serve", STRING_MARSHALLER, INTEGER_MARSHALLER,
          new ServerCallHandler<String, Integer>() {
            @Override
            public ServerCall.Listener<String> startCall(String fullMethodName,
                ServerCall<Integer> call, Metadata.Headers headers) {
              throw status.asRuntimeException();
            }
          }).build());
    ServerTransportListener transportListener = newTransport(server);

    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "/Waiter/serve", new Metadata.Headers());
    assertNotNull(streamListener);
    verifyNoMoreInteractions(stream);

    barrier.await();
    executeBarrier(executor).await();
    verify(stream).close(same(status), notNull(Metadata.Trailers.class));
    verifyNoMoreInteractions(stream);
  }

  @Test
  public void futureStatusIsPropagatedToTransport() throws Exception {
    final AtomicReference<ServerCall<Integer>> callReference
        = new AtomicReference<ServerCall<Integer>>();
    registry.addService(ServerServiceDefinition.builder("Waiter")
        .addMethod("serve", STRING_MARSHALLER, INTEGER_MARSHALLER,
          new ServerCallHandler<String, Integer>() {
            @Override
            public ServerCall.Listener<String> startCall(String fullMethodName,
                ServerCall<Integer> call, Metadata.Headers headers) {
              callReference.set(call);
              return callListener;
            }
          }).build());
    ServerTransportListener transportListener = newTransport(server);

    ServerStreamListener streamListener
        = transportListener.streamCreated(stream, "/Waiter/serve", new Metadata.Headers());
    assertNotNull(streamListener);

    executeBarrier(executor).await();
    ServerCall<Integer> call = callReference.get();
    assertNotNull(call);

    String delay = "No, I've not looked over the menu yet";
    SettableFuture<Void> appFuture = SettableFuture.create();
    when(callListener.onPayload(delay)).thenReturn(appFuture);
    ListenableFuture<Void> future = streamListener.messageRead(STRING_MARSHALLER.stream(delay), 1);
    executeBarrier(executor).await();
    verify(callListener).onPayload(delay);
    try {
      future.get(0, TimeUnit.SECONDS);
      fail();
    } catch (TimeoutException ex) {
      // Expected.
    }

    appFuture.set(null);
    // Shouldn't throw.
    future.get(0, TimeUnit.SECONDS);
  }

  private static ServerTransportListener newTransport(ServerImpl server) {
    Service transport = new NoopService();
    transport.startAsync();
    return server.serverListener().transportCreated(transport);
  }

  /**
   * Useful for plugging a single-threaded executor from processing tasks, or for waiting until a
   * single-threaded executor has processed queued tasks.
   */
  private static CyclicBarrier executeBarrier(Executor executor) {
    final CyclicBarrier barrier = new CyclicBarrier(2);
    executor.execute(new Runnable() {
      @Override
      public void run() {
        try {
          barrier.await();
        } catch (InterruptedException ex) {
          throw new RuntimeException(ex);
        } catch (BrokenBarrierException ex) {
          throw new RuntimeException(ex);
        }
      }
    });
    return barrier;
  }

  private static class NoopService extends AbstractService {
    @Override
    protected void doStart() {
      notifyStarted();
    }

    @Override
    protected void doStop() {
      notifyStopped();
    }
  }

  private static class StringMarshaller implements Marshaller<String> {
    @Override
    public InputStream stream(String value) {
      return new ByteArrayInputStream(value.getBytes(UTF_8));
    }

    @Override
    public String parse(InputStream stream) {
      try {
        return new String(ByteStreams.toByteArray(stream), UTF_8);
      } catch (IOException ex) {
        throw new RuntimeException(ex);
      }
    }
  }

  private static class IntegerMarshaller implements Marshaller<Integer> {
    @Override
    public InputStream stream(Integer value) {
      return STRING_MARSHALLER.stream(value.toString());
    }

    @Override
    public Integer parse(InputStream stream) {
      return Integer.valueOf(STRING_MARSHALLER.parse(stream));
    }
  }
}

/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.isNull;
import static org.mockito.Matchers.notNull;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.timeout;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import com.google.common.io.ByteStreams;
import com.google.common.util.concurrent.AbstractService;
import com.google.common.util.concurrent.Service;

import io.grpc.transport.ServerStream;
import io.grpc.transport.ServerStreamListener;
import io.grpc.transport.ServerTransportListener;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
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

  @Mock
  private ServerStream stream;

  @Mock
  private ServerCall.Listener<String> callListener;

  @Before
  public void startup() {
    MockitoAnnotations.initMocks(this);

    server.start();
  }

  @After
  public void teardown() {
    executor.shutdownNow();
  }

  @Test
  public void startStopImmediate() throws InterruptedException {
    Service transportServer = new NoopService();
    ServerImpl server = new ServerImpl(executor, registry).setTransportServer(transportServer);
    assertEquals(Service.State.NEW, transportServer.state());
    server.start();
    assertEquals(Service.State.RUNNING, transportServer.state());
    server.shutdown();
    assertTrue(server.awaitTerminated(100, TimeUnit.MILLISECONDS));
    assertEquals(Service.State.TERMINATED, transportServer.state());
  }

  @Test
  public void transportServerFailsStartup() {
    final Exception ex = new RuntimeException();
    class FailingStartupService extends NoopService {
      @Override
      public void doStart() {
        notifyFailed(ex);
      }
    }
    FailingStartupService transportServer = new FailingStartupService();
    ServerImpl server = new ServerImpl(executor, registry).setTransportServer(transportServer);
    try {
      server.start();
    } catch (Exception e) {
      assertSame(ex, e);
    }
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
    ServerImpl server = new ServerImpl(executor, registry).setTransportServer(transportServer)
        .start();
    ManualStoppedService transport = new ManualStoppedService();
    transport.startAsync();
    server.serverListener().transportCreated(transport);
    server.shutdown();
    assertEquals(Service.State.STOPPING, transport.state());
    assertEquals(Service.State.TERMINATED, transportServer.state());
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());

    transport.doNotifyStopped();
    assertEquals(Service.State.TERMINATED, transport.state());
    assertTrue(server.isTerminated());
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
    ServerImpl server = new ServerImpl(executor, registry).setTransportServer(transportServer)
        .start();
    Service transport = new NoopService();
    transport.startAsync();
    server.serverListener().transportCreated(transport);
    server.shutdown();
    assertEquals(Service.State.TERMINATED, transport.state());
    assertEquals(Service.State.STOPPING, transportServer.state());
    assertTrue(server.isShutdown());
    assertFalse(server.isTerminated());

    transportServer.doNotifyStopped();
    assertEquals(Service.State.TERMINATED, transportServer.state());
    assertTrue(server.isTerminated());
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
    streamListener.messageRead(STRING_MARSHALLER.stream(order));
    verify(callListener, timeout(2000)).onPayload(order);

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

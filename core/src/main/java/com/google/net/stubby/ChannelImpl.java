package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.common.util.concurrent.AbstractService;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.ClientTransportFactory;
import com.google.net.stubby.newtransport.StreamListener;

import java.io.IOException;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;

import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
public final class ChannelImpl extends AbstractService implements Channel {
  private final ClientTransportFactory transportFactory;
  private final ExecutorService executor;
  /**
   * All transports that are not stopped. At the very least {@link #activeTransport} will be
   * present, but previously used transports that still have streams or are stopping may also be
   * present.
   */
  @GuardedBy("this")
  private Collection<ClientTransport> transports = new ArrayList<ClientTransport>();
  /** The transport for new outgoing requests. */
  @GuardedBy("this")
  private ClientTransport activeTransport;

  public ChannelImpl(ClientTransportFactory transportFactory, ExecutorService executor) {
    this.transportFactory = transportFactory;
    this.executor = executor;
  }

  @Override
  protected void doStart() {
    notifyStarted();
  }

  @Override
  protected synchronized void doStop() {
    if (transports.isEmpty()) {
      notifyStopped();
    } else {
      // The last TransportListener will call notifyStopped().
      if (activeTransport != null) {
        activeTransport.stopAsync();
        activeTransport = null;
      }
    }
  }

  @Override
  public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method) {
    return new CallImpl<ReqT, RespT>(method, new SerializingExecutor(executor));
  }

  private synchronized ClientTransport obtainActiveTransport() {
    if (activeTransport == null) {
      if (state() != State.RUNNING) {
        throw new IllegalStateException("Not running");
      }
      ClientTransport newTransport = transportFactory.newClientTransport();
      activeTransport = newTransport;
      transports.add(newTransport);
      // activeTransport reference can be changed during calls to the transport, even if we hold the
      // lock, due to reentrancy.
      newTransport.addListener(
          new TransportListener(newTransport), MoreExecutors.directExecutor());
      newTransport.startAsync();
      return newTransport;
    }
    return activeTransport;
  }

  private synchronized void transportFailedOrStopped(ClientTransport transport) {
    if (activeTransport == transport) {
      activeTransport = null;
    }
    transports.remove(transport);
    if (state() != State.RUNNING && transports.isEmpty()) {
      notifyStopped();
    }
  }

  private class TransportListener extends Listener {
    private final ClientTransport transport;

    public TransportListener(ClientTransport transport) {
      this.transport = transport;
    }

    @Override
    public void stopping(State from) {
      synchronized (ChannelImpl.this) {
        if (activeTransport == transport) {
          activeTransport = null;
        }
      }
    }

    @Override
    public void failed(State from, Throwable failure) {
      transportFailedOrStopped(transport);
    }

    @Override
    public void terminated(State from) {
      transportFailedOrStopped(transport);
    }
  }

  private class CallImpl<ReqT, RespT> extends Call<ReqT, RespT> {
    private final MethodDescriptor<ReqT, RespT> method;
    private final SerializingExecutor callExecutor;
    // TODO(user): Consider moving flow control notification/management to Call itself.
    private final Collection<SettableFuture<Void>> inProcessFutures
        = Collections.synchronizedSet(new HashSet<SettableFuture<Void>>());
    private ClientStream stream;

    public CallImpl(MethodDescriptor<ReqT, RespT> method, SerializingExecutor executor) {
      this.method = method;
      this.callExecutor = executor;
    }

    @Override
    public void start(Listener<RespT> observer, Metadata.Headers headers) {
      Preconditions.checkState(stream == null, "Already started");
      headers.setPath(method.getName());
      headers.setAuthority("fixme");
      stream = obtainActiveTransport().newStream(method, headers,
          new StreamListenerImpl(observer));
    }

    @Override
    public void cancel() {
      // Cancel is called in exception handling cases, so it may be the case that the
      // stream was never successfully created.
      if (stream != null) {
        stream.cancel();
      }
    }

    @Override
    public void halfClose() {
      Preconditions.checkState(stream != null, "Not started");
      stream.halfClose();
    }

    private int available(InputStream is) {
      try {
        return is.available();
      } catch (IOException ex) {
        throw Throwables.propagate(ex);
      }
    }

    @Override
    public void sendPayload(ReqT payload, SettableFuture<Void> accepted) {
      Preconditions.checkState(stream != null, "Not started");
      InputStream payloadIs = method.streamRequest(payload);
      if (accepted == null) {
        stream.writeMessage(payloadIs, available(payloadIs), null);
      } else {
        inProcessFutures.add(accepted);
        stream.writeMessage(payloadIs, available(payloadIs), new AcceptedRunnable(accepted));
      }
    }

    private class AcceptedRunnable implements Runnable {
      private final SettableFuture<Void> future;

      public AcceptedRunnable(SettableFuture<Void> future) {
        this.future = future;
      }

      @Override
      public void run() {
        inProcessFutures.remove(future);
        future.set(null);
      }
    }

    private class StreamListenerImpl implements StreamListener {
      private final Listener<RespT> observer;

      public StreamListenerImpl(Listener<RespT> observer) {
        Preconditions.checkNotNull(observer);
        this.observer = observer;
      }

      private ListenableFuture<Void> dispatchCallable(
          final Callable<ListenableFuture<Void>> callable) {
        final SettableFuture<Void> ours = SettableFuture.create();
        callExecutor.execute(new Runnable() {
          @Override
          public void run() {
            try {
              ListenableFuture<Void> theirs = callable.call();
              if (theirs == null) {
                ours.set(null);
              } else {
                theirs.addListener(new Runnable() {
                  @Override
                  public void run() {
                    // TODO(user): If their Future fails, should we Call.cancel()?
                    ours.set(null);
                  }
                }, MoreExecutors.directExecutor());
              }
            } catch (Throwable t) {
              ours.set(null);
              CallImpl.this.cancel();
              Throwables.propagate(t);
            }
          }
        });
        return ours;
      }

      @Override
      public ListenableFuture<Void> headersRead(final Metadata.Headers headers) {
        return dispatchCallable(new Callable<ListenableFuture<Void>>() {
          @Override
          public ListenableFuture<Void> call() throws Exception {
            return observer.onHeaders(headers);
          }
        });
      }

      @Override
      public ListenableFuture<Void> messageRead(final InputStream message, final int length) {
        return dispatchCallable(new Callable<ListenableFuture<Void>>() {
          @Override
          public ListenableFuture<Void> call() {
            return observer.onPayload(method.parseResponse(message));
          }
        });
      }

      @Override
      public void closed(final Status status, final Metadata.Trailers trailers) {
        for (SettableFuture<Void> future : inProcessFutures) {
          future.cancel(false);
        }
        inProcessFutures.clear();
        callExecutor.execute(new Runnable() {
          @Override
          public void run() {
            observer.onClose(status, trailers);
          }
        });
      }
    }
  }
}

package com.google.net.stubby;

import static com.google.common.util.concurrent.Service.State.RUNNING;
import static com.google.common.util.concurrent.Service.State.STARTING;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.newtransport.ClientStream;
import com.google.net.stubby.newtransport.ClientStreamListener;
import com.google.net.stubby.newtransport.ClientTransport;
import com.google.net.stubby.newtransport.ClientTransportFactory;

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
    obtainActiveTransport(true);
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

  private synchronized ClientTransport obtainActiveTransport(boolean notifyWhenRunning) {
    if (activeTransport == null) {
      State state = state();
      if (state != RUNNING && state != STARTING) {
        throw new IllegalStateException("Not running");
      }
      ClientTransport newTransport = transportFactory.newClientTransport();
      activeTransport = newTransport;
      transports.add(newTransport);
      // activeTransport reference can be changed during calls to the transport, even if we hold the
      // lock, due to reentrancy.
      newTransport.addListener(
          new TransportListener(newTransport), MoreExecutors.directExecutor());
      if (notifyWhenRunning) {
        newTransport.addListener(new Listener() {
          @Override
          public void running() {
            notifyStarted();
          }
        }, executor);
      }
      newTransport.startAsync();
      return newTransport;
    }
    return activeTransport;
  }

  private synchronized void transportFailedOrStopped(ClientTransport transport, Throwable t) {
    if (activeTransport == transport) {
      activeTransport = null;
    }
    transports.remove(transport);
    if (state() != RUNNING && transports.isEmpty()) {
      if (t != null) {
        notifyFailed(t);
      } else {
        notifyStopped();
      }
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
      transportFailedOrStopped(transport, failure);
    }

    @Override
    public void terminated(State from) {
      transportFailedOrStopped(transport, null);
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
      stream = obtainActiveTransport(false).newStream(method, headers,
          new ClientStreamListenerImpl(observer));
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
        throw new RuntimeException(ex);
      }
    }

    @Override
    public void sendPayload(ReqT payload, SettableFuture<Void> accepted) {
      Preconditions.checkState(stream != null, "Not started");
      boolean failed = true;
      try {
        InputStream payloadIs = method.streamRequest(payload);
        if (accepted == null) {
          stream.writeMessage(payloadIs, available(payloadIs), null);
        } else {
          inProcessFutures.add(accepted);
          stream.writeMessage(payloadIs, available(payloadIs), new AcceptedRunnable(accepted));
        }
        failed = false;
      } finally {
        if (failed) {
          cancel();
        }
      }
      stream.flush();
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

    private class ClientStreamListenerImpl implements ClientStreamListener {
      private final Listener<RespT> observer;

      public ClientStreamListenerImpl(Listener<RespT> observer) {
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
                Futures.addCallback(theirs, new FutureCallback<Void>() {
                  @Override
                  public void onSuccess(Void result) {
                    ours.set(null);
                  }
                  @Override
                  public void onFailure(Throwable t) {
                    ours.setException(t);
                  }
                }, MoreExecutors.directExecutor());
              }
            } catch (Throwable t) {
              ours.setException(t);
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

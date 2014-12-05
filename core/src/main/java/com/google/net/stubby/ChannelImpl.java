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

package com.google.net.stubby;

import static com.google.common.util.concurrent.Service.State.RUNNING;
import static com.google.common.util.concurrent.Service.State.STARTING;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.transport.ClientStream;
import com.google.net.stubby.transport.ClientStreamListener;
import com.google.net.stubby.transport.ClientTransport;
import com.google.net.stubby.transport.ClientTransportFactory;

import java.io.IOException;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashSet;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.logging.Level;
import java.util.logging.Logger;

import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
public final class ChannelImpl extends AbstractService implements Channel {

  private static final Logger log = Logger.getLogger(ChannelImpl.class.getName());

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
      newTransport.addListener(new TransportListener(newTransport, notifyWhenRunning),
          MoreExecutors.directExecutor());
      newTransport.startAsync();
      return newTransport;
    }
    return activeTransport;
  }

  private synchronized void transportFailedOrStopped(ClientTransport transport, Throwable t) {
    if (transport.state() == State.FAILED) {
      log.log(Level.SEVERE, "client transport failed " + transport.getClass().getName(),
          transport.failureCause());
    }
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
    private final boolean notifyWhenRunning;

    public TransportListener(ClientTransport transport, boolean notifyWhenRunning) {
      this.transport = transport;
      this.notifyWhenRunning = notifyWhenRunning;
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

    @Override
    public void running() {
      if (notifyWhenRunning) {
        notifyStarted();
      }
    }
  }

  private class CallImpl<ReqT, RespT> extends Call<ReqT, RespT> {
    private final MethodDescriptor<ReqT, RespT> method;
    private final SerializingExecutor callExecutor;
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
    public void sendPayload(ReqT payload) {
      Preconditions.checkState(stream != null, "Not started");
      boolean failed = true;
      try {
        InputStream payloadIs = method.streamRequest(payload);
        stream.writeMessage(payloadIs, available(payloadIs), null);
        failed = false;
      } finally {
        if (failed) {
          cancel();
        }
      }
      stream.flush();
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

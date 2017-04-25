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

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ConnectivityStateInfo;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.PickResult;
import io.grpc.LoadBalancer.PickSubchannelArgs;
import io.grpc.LoadBalancer.SubchannelPicker;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A ManagedChannel backed by a single {@link InternalSubchannel} and used for {@link LoadBalancer}
 * to its own RPC needs.
 */
@ThreadSafe
final class OobChannel extends ManagedChannel implements WithLogId {
  private static final Logger log = Logger.getLogger(OobChannel.class.getName());

  private InternalSubchannel subchannel;
  private SubchannelImpl subchannelImpl;
  private SubchannelPicker subchannelPicker;

  private final LogId logId = LogId.allocate(getClass().getName());
  private final String authority;
  private final DelayedClientTransport delayedTransport;
  private final ObjectPool<? extends Executor> executorPool;
  private final Executor executor;
  private final ScheduledExecutorService deadlineCancellationExecutor;
  private final CountDownLatch terminatedLatch = new CountDownLatch(1);
  private volatile boolean shutdown;

  private final ClientTransportProvider transportProvider = new ClientTransportProvider() {
    @Override
    public ClientTransport get(PickSubchannelArgs args) {
      // delayed transport's newStream() always acquires a lock, but concurrent performance doesn't
      // matter here because OOB communication should be sparse, and it's not on application RPC's
      // critical path.
      return delayedTransport;
    }
  };

  OobChannel(
      String authority, ObjectPool<? extends Executor> executorPool,
      ScheduledExecutorService deadlineCancellationExecutor, ChannelExecutor channelExecutor) {
    this.authority = checkNotNull(authority, "authority");
    this.executorPool = checkNotNull(executorPool, "executorPool");
    this.executor = checkNotNull(executorPool.getObject(), "executor");
    this.deadlineCancellationExecutor = checkNotNull(
        deadlineCancellationExecutor, "deadlineCancellationExecutor");
    this.delayedTransport = new DelayedClientTransport(executor, channelExecutor);
    this.delayedTransport.start(new ManagedClientTransport.Listener() {
        @Override
        public void transportShutdown(Status s) {
          // Don't care
        }

        @Override
        public void transportTerminated() {
          subchannelImpl.shutdown();
        }

        @Override
        public void transportReady() {
          // Don't care
        }

        @Override
        public void transportInUse(boolean inUse) {
          // Don't care
        }
      });
  }

  // Must be called only once, right after the OobChannel is created.
  void setSubchannel(final InternalSubchannel subchannel) {
    log.log(Level.FINE, "[{0}] Created with [{1}]", new Object[] {this, subchannel});
    this.subchannel = subchannel;
    subchannelImpl = new SubchannelImpl() {
        @Override
        public void shutdown() {
          subchannel.shutdown();
        }

        @Override
        ClientTransport obtainActiveTransport() {
          return subchannel.obtainActiveTransport();
        }

        @Override
        public void requestConnection() {
          subchannel.obtainActiveTransport();
        }

        @Override
        public EquivalentAddressGroup getAddresses() {
          return subchannel.getAddressGroup();
        }

        @Override
        public Attributes getAttributes() {
          return Attributes.EMPTY;
        }
      };

    subchannelPicker = new SubchannelPicker() {
        final PickResult result = PickResult.withSubchannel(subchannelImpl);

        @Override
        public PickResult pickSubchannel(PickSubchannelArgs args) {
          return result;
        }
      };
    delayedTransport.reprocess(subchannelPicker);
  }

  void updateAddresses(EquivalentAddressGroup eag) {
    subchannel.updateAddresses(eag);
  }

  @Override
  public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
      MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
    return new ClientCallImpl<RequestT, ResponseT>(methodDescriptor,
        callOptions.getExecutor() == null ? executor : callOptions.getExecutor(),
        callOptions, transportProvider, deadlineCancellationExecutor);
  }

  @Override
  public String authority() {
    return authority;
  }

  @Override
  public LogId getLogId() {
    return logId;
  }

  @Override
  public boolean isTerminated() {
    return terminatedLatch.getCount() == 0;
  }

  @Override
  public boolean awaitTermination(long time, TimeUnit unit) throws InterruptedException {
    return terminatedLatch.await(time, unit);
  }

  @Override
  public ManagedChannel shutdown() {
    shutdown = true;
    delayedTransport.shutdown();
    return this;
  }

  @Override
  public boolean isShutdown() {
    return shutdown;
  }

  @Override
  public ManagedChannel shutdownNow() {
    shutdown = true;
    delayedTransport.shutdownNow(
        Status.UNAVAILABLE.withDescription("OobChannel.shutdownNow() called"));
    return this;
  }

  void handleSubchannelStateChange(final ConnectivityStateInfo newState) {
    switch (newState.getState()) {
      case READY:
      case IDLE:
        delayedTransport.reprocess(subchannelPicker);
        break;
      case TRANSIENT_FAILURE:
        delayedTransport.reprocess(new SubchannelPicker() {
            final PickResult errorResult = PickResult.withError(newState.getStatus());

            @Override
            public PickResult pickSubchannel(PickSubchannelArgs args) {
              return errorResult;
            }
          });
        break;
      default:
        // Do nothing
    }
  }

  void handleSubchannelTerminated() {
    // When delayedTransport is terminated, it shuts down subchannel.  Therefore, at this point
    // both delayedTransport and subchannel have terminated.
    executorPool.returnObject(executor);
    terminatedLatch.countDown();
  }
}

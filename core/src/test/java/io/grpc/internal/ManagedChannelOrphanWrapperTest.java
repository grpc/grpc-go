/*
 * Copyright 2018 The gRPC Authors
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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;

import com.google.common.testing.GcFinalization;
import com.google.common.testing.GcFinalization.FinalizationPredicate;
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.internal.ManagedChannelOrphanWrapper.ManagedChannelReference;
import java.lang.ref.ReferenceQueue;
import java.lang.ref.WeakReference;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.logging.Filter;
import java.util.logging.Level;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class ManagedChannelOrphanWrapperTest {
  @Test
  public void orphanedChannelsAreLogged() {
    ManagedChannel mc = new TestManagedChannel();
    String channelString = mc.toString();
    final ReferenceQueue<ManagedChannelOrphanWrapper> refqueue =
        new ReferenceQueue<>();
    ConcurrentMap<ManagedChannelReference, ManagedChannelReference> refs =
        new ConcurrentHashMap<>();

    assertEquals(0, refs.size());
    ManagedChannelOrphanWrapper channel = new ManagedChannelOrphanWrapper(mc, refqueue, refs);
    assertEquals(1, refs.size());

    // Try to capture the log output but without causing terminal noise.  Adding the filter must
    // be done before clearing the ref or else it might be missed.
    final List<LogRecord> records = new ArrayList<>(1);
    Logger orphanLogger = Logger.getLogger(ManagedChannelOrphanWrapper.class.getName());
    Filter oldFilter = orphanLogger.getFilter();
    orphanLogger.setFilter(new Filter() {

      @Override
      public boolean isLoggable(LogRecord record) {
        synchronized (records) {
          records.add(record);
        }
        return false;
      }
    });

    try {
      channel = null;
      final AtomicInteger numOrphans = new AtomicInteger();
      GcFinalization.awaitDone(
          new FinalizationPredicate() {
            @Override
            public boolean isDone() {
              numOrphans.getAndAdd(ManagedChannelReference.cleanQueue(refqueue));
              return numOrphans.get() > 0;
            }
          });
      assertEquals("unexpected extra orphans", 1, numOrphans.get());

      LogRecord lr;
      synchronized (records) {
        assertEquals(1, records.size());
        lr = records.get(0);
      }
      assertThat(lr.getMessage()).contains("shutdown");
      assertThat(lr.getParameters()).asList().containsExactly(channelString).inOrder();
      assertEquals(Level.SEVERE, lr.getLevel());
      assertEquals(0, refs.size());
    } finally {
      orphanLogger.setFilter(oldFilter);
    }
  }

  @Test
  public void refCycleIsGCed() {
    ReferenceQueue<ManagedChannelOrphanWrapper> refqueue =
        new ReferenceQueue<>();
    ConcurrentMap<ManagedChannelReference, ManagedChannelReference> refs =
        new ConcurrentHashMap<>();
    ApplicationWithChannelRef app = new ApplicationWithChannelRef();
    ChannelWithApplicationRef channelImpl = new ChannelWithApplicationRef();
    ManagedChannelOrphanWrapper channel =
        new ManagedChannelOrphanWrapper(channelImpl, refqueue, refs);
    app.channel = channel;
    channelImpl.application = app;
    WeakReference<ApplicationWithChannelRef> appWeakRef =
        new WeakReference<>(app);

    // Simulate the application and channel going out of scope. A ref cycle between app and
    // channel remains, so ensure that our tracking of orphaned channels does not prevent this
    // reference cycle from being GCed.
    channel = null;
    app = null;
    channelImpl = null;

    GcFinalization.awaitClear(appWeakRef);
  }

  private static class TestManagedChannel extends ManagedChannel {
    @Override
    public ManagedChannel shutdown() {
      return null;
    }

    @Override
    public boolean isShutdown() {
      return false;
    }

    @Override
    public boolean isTerminated() {
      return false;
    }

    @Override
    public ManagedChannel shutdownNow() {
      return null;
    }

    @Override
    public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
      return false;
    }

    @Override
    public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
        MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
      return null;
    }

    @Override
    public String authority() {
      return null;
    }
  }

  private static final class ApplicationWithChannelRef {
    private ManagedChannel channel;
  }

  private static final class ChannelWithApplicationRef extends TestManagedChannel {
    private ApplicationWithChannelRef application;
  }
}

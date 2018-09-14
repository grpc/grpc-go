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
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.internal.ManagedChannelOrphanWrapper.ManagedChannelReference;
import java.lang.ref.ReferenceQueue;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.concurrent.TimeUnit;
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
  public void orphanedChannelsAreLogged() throws Exception {
    ManagedChannel mc = mock(ManagedChannel.class);
    String channelString = mc.toString();
    ReferenceQueue<ManagedChannelOrphanWrapper> refqueue =
        new ReferenceQueue<ManagedChannelOrphanWrapper>();
    ConcurrentMap<ManagedChannelReference, ManagedChannelReference> refs =
        new ConcurrentHashMap<ManagedChannelReference, ManagedChannelReference>();

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

    // TODO(carl-mastrangelo): consider using com.google.common.testing.GcFinalization instead.
    try {
      channel = null;
      boolean success = false;
      for (int retry = 0; retry < 3; retry++) {
        System.gc();
        System.runFinalization();
        int orphans = ManagedChannelReference.cleanQueue(refqueue);
        if (orphans == 1) {
          success = true;
          break;
        }
        assertEquals("unexpected extra orphans", 0, orphans);
        Thread.sleep(100L * (1L << retry));
      }
      assertTrue("Channel was not garbage collected", success);

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

  private static final class TestManagedChannel extends ManagedChannel {
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
}

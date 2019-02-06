/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.inprocess;

import com.google.common.truth.Truth;
import io.grpc.ServerStreamTracer;
import io.grpc.internal.FakeClock;
import io.grpc.internal.ObjectPool;
import io.grpc.internal.ServerListener;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;
import java.util.Collections;
import java.util.concurrent.ScheduledExecutorService;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class InProcessServerTest {
  private InProcessServerBuilder builder = InProcessServerBuilder.forName("name");

  @Test
  public void getPort_notStarted() throws Exception {
    InProcessServer s =
        new InProcessServer(builder, Collections.<ServerStreamTracer.Factory>emptyList());

    Truth.assertThat(s.getListenSocketAddress()).isEqualTo(new InProcessSocketAddress("name"));
  }

  @Test
  public void serverHoldsRefToScheduler() throws Exception {
    final ScheduledExecutorService ses = new FakeClock().getScheduledExecutorService();
    class RefCountingObjectPool implements ObjectPool<ScheduledExecutorService> {
      private int count;

      @Override
      public ScheduledExecutorService getObject() {
        count++;
        return ses;
      }

      @Override
      public ScheduledExecutorService returnObject(Object returned) {
        count--;
        return null;
      }
    }

    RefCountingObjectPool pool = new RefCountingObjectPool();
    builder.schedulerPool = pool;
    InProcessServer s =
        new InProcessServer(builder, Collections.<ServerStreamTracer.Factory>emptyList());
    Truth.assertThat(pool.count).isEqualTo(0);
    s.start(new ServerListener() {
      @Override public ServerTransportListener transportCreated(ServerTransport transport) {
        throw new UnsupportedOperationException();
      }

      @Override public void serverShutdown() {}
    });
    Truth.assertThat(pool.count).isEqualTo(1);
    s.shutdown();
    Truth.assertThat(pool.count).isEqualTo(0);
  }
}


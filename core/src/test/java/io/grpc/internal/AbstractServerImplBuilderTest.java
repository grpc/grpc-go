/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

import com.google.instrumentation.stats.StatsContext;
import com.google.instrumentation.stats.StatsContextFactory;
import io.grpc.Metadata;
import io.grpc.ServerStreamTracer;
import java.io.File;
import java.io.InputStream;
import java.util.List;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AbstractServerImplBuilder}. */
@RunWith(JUnit4.class)
public class AbstractServerImplBuilderTest {
  private static final StatsContextFactory DUMMY_STATS_FACTORY =
      new StatsContextFactory() {
        @Override
        public StatsContext deserialize(InputStream input) {
          throw new UnsupportedOperationException();
        }

        @Override
        public StatsContext getDefault() {
          throw new UnsupportedOperationException();
        }
      };

  private static final ServerStreamTracer.Factory DUMMY_USER_TRACER =
      new ServerStreamTracer.Factory() {
        @Override
        public ServerStreamTracer newServerStreamTracer(String fullMethodName, Metadata headers) {
          throw new UnsupportedOperationException();
        }
      };

  private Builder builder = new Builder();

  @Test
  public void getTracerFactories_default() {
    builder.addStreamTracerFactory(DUMMY_USER_TRACER);
    List<ServerStreamTracer.Factory> factories = builder.getTracerFactories();
    assertEquals(3, factories.size());
    assertThat(factories.get(0)).isInstanceOf(CensusStatsModule.ServerTracerFactory.class);
    assertThat(factories.get(1)).isInstanceOf(CensusTracingModule.ServerTracerFactory.class);
    assertThat(factories.get(2)).isSameAs(DUMMY_USER_TRACER);
  }

  @Test
  public void getTracerFactories_disableStats() {
    builder.addStreamTracerFactory(DUMMY_USER_TRACER);
    builder.setStatsEnabled(false);
    List<ServerStreamTracer.Factory> factories = builder.getTracerFactories();
    assertEquals(2, factories.size());
    assertThat(factories.get(0)).isInstanceOf(CensusTracingModule.ServerTracerFactory.class);
    assertThat(factories.get(1)).isSameAs(DUMMY_USER_TRACER);
  }

  @Test
  public void getTracerFactories_disableTracing() {
    builder.addStreamTracerFactory(DUMMY_USER_TRACER);
    builder.setTracingEnabled(false);
    List<ServerStreamTracer.Factory> factories = builder.getTracerFactories();
    assertEquals(2, factories.size());
    assertThat(factories.get(0)).isInstanceOf(CensusStatsModule.ServerTracerFactory.class);
    assertThat(factories.get(1)).isSameAs(DUMMY_USER_TRACER);
  }

  @Test
  public void getTracerFactories_disableBoth() {
    builder.addStreamTracerFactory(DUMMY_USER_TRACER);
    builder.setTracingEnabled(false);
    builder.setStatsEnabled(false);
    List<ServerStreamTracer.Factory> factories = builder.getTracerFactories();
    assertThat(factories).containsExactly(DUMMY_USER_TRACER);
  }

  static class Builder extends AbstractServerImplBuilder<Builder> {
    Builder() {
      statsContextFactory(DUMMY_STATS_FACTORY);
    }

    @Override
    protected io.grpc.internal.InternalServer buildTransportServer(
        List<ServerStreamTracer.Factory> streamTracerFactories) {
      throw new UnsupportedOperationException();
    }

    @Override
    public Builder useTransportSecurity(File certChain, File privateKey) {
      throw new UnsupportedOperationException();
    }
  }

}

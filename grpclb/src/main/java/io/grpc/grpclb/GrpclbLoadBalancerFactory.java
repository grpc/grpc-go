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

package io.grpc.grpclb;

import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer;
import io.grpc.PickFirstBalancerFactory;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourcePool;
import io.grpc.util.RoundRobinLoadBalancerFactory;

/**
 * A factory for {@link LoadBalancer}s that uses the GRPCLB protocol.
 *
 * <p><b>Experimental:</b>This only works with the GRPCLB load-balancer service, which is not
 * available yet. Right now it's only good for internal testing.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1782")
public class GrpclbLoadBalancerFactory extends LoadBalancer.Factory {

  private static final GrpclbLoadBalancerFactory INSTANCE = new GrpclbLoadBalancerFactory();
  private static final TimeProvider TIME_PROVIDER = new TimeProvider() {
      @Override
      public long currentTimeMillis() {
        return System.currentTimeMillis();
      }
    };

  private GrpclbLoadBalancerFactory() {
  }

  public static GrpclbLoadBalancerFactory getInstance() {
    return INSTANCE;
  }

  @Override
  public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
    return new GrpclbLoadBalancer(
        helper, PickFirstBalancerFactory.getInstance(),
        RoundRobinLoadBalancerFactory.getInstance(),
        // TODO(zhangkun83): balancer sends load reporting RPCs from it, which also involves
        // channelExecutor thus may also run other tasks queued in the channelExecutor.  If such
        // load should not be on the shared scheduled executor, we should use a combination of the
        // scheduled executor and the default app executor.
        SharedResourcePool.forResource(GrpcUtil.TIMER_SERVICE),
        TIME_PROVIDER);
  }
}

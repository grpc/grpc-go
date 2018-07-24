/*
 * Copyright 2017 The gRPC Authors
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
import io.grpc.internal.ExponentialBackoffPolicy;
import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourcePool;
import io.grpc.internal.TimeProvider;

/**
 * A factory for {@link LoadBalancer}s that uses the GRPCLB protocol.
 *
 * <p><b>Experimental:</b>This only works with the GRPCLB load-balancer service, which is not
 * available yet. Right now it's only good for internal testing.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1782")
public class GrpclbLoadBalancerFactory extends LoadBalancer.Factory {

  private static final GrpclbLoadBalancerFactory INSTANCE = new GrpclbLoadBalancerFactory();

  private GrpclbLoadBalancerFactory() {
  }

  public static GrpclbLoadBalancerFactory getInstance() {
    return INSTANCE;
  }

  @Override
  public LoadBalancer newLoadBalancer(LoadBalancer.Helper helper) {
    return new GrpclbLoadBalancer(
        helper, new CachedSubchannelPool(),
        // TODO(zhangkun83): balancer sends load reporting RPCs from it, which also involves
        // channelExecutor thus may also run other tasks queued in the channelExecutor.  If such
        // load should not be on the shared scheduled executor, we should use a combination of the
        // scheduled executor and the default app executor.
        SharedResourcePool.forResource(GrpcUtil.TIMER_SERVICE),
        TimeProvider.SYSTEM_TIME_PROVIDER,
        new ExponentialBackoffPolicy.Provider());
  }
}

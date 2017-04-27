/*
 * Copyright 2017, Google Inc. All rights reserved.
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

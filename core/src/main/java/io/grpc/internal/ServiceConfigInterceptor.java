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

import static com.google.common.base.Verify.verify;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.Deadline;
import io.grpc.MethodDescriptor;
import io.grpc.internal.ManagedChannelServiceConfig.MethodInfo;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import javax.annotation.CheckForNull;
import javax.annotation.Nullable;

/**
 * Modifies RPCs in conformance with a Service Config.
 */
final class ServiceConfigInterceptor implements ClientInterceptor {

  // Map from method name to MethodInfo
  @VisibleForTesting
  final AtomicReference<ManagedChannelServiceConfig> managedChannelServiceConfig
      = new AtomicReference<>();

  private final boolean retryEnabled;
  private final int maxRetryAttemptsLimit;
  private final int maxHedgedAttemptsLimit;

  // Setting this to true and observing this equal to true are run in different threads.
  private volatile boolean initComplete;

  ServiceConfigInterceptor(
      boolean retryEnabled, int maxRetryAttemptsLimit, int maxHedgedAttemptsLimit) {
    this.retryEnabled = retryEnabled;
    this.maxRetryAttemptsLimit = maxRetryAttemptsLimit;
    this.maxHedgedAttemptsLimit = maxHedgedAttemptsLimit;
  }

  void handleUpdate(@Nullable Map<String, ?> serviceConfig) {
    ManagedChannelServiceConfig conf;
    if (serviceConfig == null) {
      conf = new ManagedChannelServiceConfig(
          new HashMap<String, MethodInfo>(), new HashMap<String, MethodInfo>());
    } else {
      conf = ManagedChannelServiceConfig.fromServiceConfig(
          serviceConfig, retryEnabled, maxRetryAttemptsLimit, maxHedgedAttemptsLimit);
    }
    managedChannelServiceConfig.set(conf);
    initComplete = true;
  }

  static final CallOptions.Key<RetryPolicy.Provider> RETRY_POLICY_KEY =
      CallOptions.Key.create("internal-retry-policy");
  static final CallOptions.Key<HedgingPolicy.Provider> HEDGING_POLICY_KEY =
      CallOptions.Key.create("internal-hedging-policy");

  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      final MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
    if (retryEnabled) {
      if (initComplete) {
        final RetryPolicy retryPolicy = getRetryPolicyFromConfig(method);
        final class ImmediateRetryPolicyProvider implements RetryPolicy.Provider {
          @Override
          public RetryPolicy get() {
            return retryPolicy;
          }
        }

        final HedgingPolicy hedgingPolicy = getHedgingPolicyFromConfig(method);
        final class ImmediateHedgingPolicyProvider implements HedgingPolicy.Provider {
          @Override
          public HedgingPolicy get() {
            return hedgingPolicy;
          }
        }

        verify(
            retryPolicy.equals(RetryPolicy.DEFAULT) || hedgingPolicy.equals(HedgingPolicy.DEFAULT),
            "Can not apply both retry and hedging policy for the method '%s'", method);

        callOptions = callOptions
            .withOption(RETRY_POLICY_KEY, new ImmediateRetryPolicyProvider())
            .withOption(HEDGING_POLICY_KEY, new ImmediateHedgingPolicyProvider());
      } else {
        final class DelayedRetryPolicyProvider implements RetryPolicy.Provider {
          /**
           * Returns RetryPolicy.DEFAULT if name resolving is not complete at the moment the method
           * is invoked, otherwise returns the RetryPolicy computed from service config.
           *
           * <p>Note that this method is used no more than once for each call.
           */
          @Override
          public RetryPolicy get() {
            if (!initComplete) {
              return RetryPolicy.DEFAULT;
            }
            return getRetryPolicyFromConfig(method);
          }
        }

        final class DelayedHedgingPolicyProvider implements HedgingPolicy.Provider {
          /**
           * Returns HedgingPolicy.DEFAULT if name resolving is not complete at the moment the
           * method is invoked, otherwise returns the HedgingPolicy computed from service config.
           *
           * <p>Note that this method is used no more than once for each call.
           */
          @Override
          public HedgingPolicy get() {
            if (!initComplete) {
              return HedgingPolicy.DEFAULT;
            }
            HedgingPolicy hedgingPolicy = getHedgingPolicyFromConfig(method);
            verify(
                hedgingPolicy.equals(HedgingPolicy.DEFAULT)
                    || getRetryPolicyFromConfig(method).equals(RetryPolicy.DEFAULT),
                "Can not apply both retry and hedging policy for the method '%s'", method);
            return hedgingPolicy;
          }
        }

        callOptions = callOptions
            .withOption(RETRY_POLICY_KEY, new DelayedRetryPolicyProvider())
            .withOption(HEDGING_POLICY_KEY, new DelayedHedgingPolicyProvider());
      }
    }

    MethodInfo info = getMethodInfo(method);
    if (info == null) {
      return next.newCall(method, callOptions);
    }

    if (info.timeoutNanos != null) {
      Deadline newDeadline = Deadline.after(info.timeoutNanos, TimeUnit.NANOSECONDS);
      Deadline existingDeadline = callOptions.getDeadline();
      // If the new deadline is sooner than the existing deadline, swap them.
      if (existingDeadline == null || newDeadline.compareTo(existingDeadline) < 0) {
        callOptions = callOptions.withDeadline(newDeadline);
      }
    }
    if (info.waitForReady != null) {
      callOptions =
          info.waitForReady ? callOptions.withWaitForReady() : callOptions.withoutWaitForReady();
    }
    if (info.maxInboundMessageSize != null) {
      Integer existingLimit = callOptions.getMaxInboundMessageSize();
      if (existingLimit != null) {
        callOptions = callOptions.withMaxInboundMessageSize(
            Math.min(existingLimit, info.maxInboundMessageSize));
      } else {
        callOptions = callOptions.withMaxInboundMessageSize(info.maxInboundMessageSize);
      }
    }
    if (info.maxOutboundMessageSize != null) {
      Integer existingLimit = callOptions.getMaxOutboundMessageSize();
      if (existingLimit != null) {
        callOptions = callOptions.withMaxOutboundMessageSize(
            Math.min(existingLimit, info.maxOutboundMessageSize));
      } else {
        callOptions = callOptions.withMaxOutboundMessageSize(info.maxOutboundMessageSize);
      }
    }

    return next.newCall(method, callOptions);
  }

  @CheckForNull
  private MethodInfo getMethodInfo(MethodDescriptor<?, ?> method) {
    ManagedChannelServiceConfig mcsc = managedChannelServiceConfig.get();
    MethodInfo info = null;
    if (mcsc != null) {
      info = mcsc.getServiceMethodMap().get(method.getFullMethodName());
    }
    if (info == null && mcsc != null) {
      String serviceName = MethodDescriptor.extractFullServiceName(method.getFullMethodName());
      info = mcsc.getServiceMap().get(serviceName);
    }
    return info;
  }

  @VisibleForTesting
  RetryPolicy getRetryPolicyFromConfig(MethodDescriptor<?, ?> method) {
    MethodInfo info = getMethodInfo(method);
    return info == null ? RetryPolicy.DEFAULT : info.retryPolicy;
  }

  @VisibleForTesting
  HedgingPolicy getHedgingPolicyFromConfig(MethodDescriptor<?, ?> method) {
    MethodInfo info = getMethodInfo(method);
    return info == null ? HedgingPolicy.DEFAULT : info.hedgingPolicy;
  }
}

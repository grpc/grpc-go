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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Verify.verify;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import com.google.common.base.Strings;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.Deadline;
import io.grpc.MethodDescriptor;
import io.grpc.Status.Code;
import java.util.Collections;
import java.util.EnumSet;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.CheckForNull;
import javax.annotation.Nonnull;

/**
 * Modifies RPCs in in conformance with a Service Config.
 */
final class ServiceConfigInterceptor implements ClientInterceptor {

  private static final Logger logger = Logger.getLogger(ServiceConfigInterceptor.class.getName());

  // Map from method name to MethodInfo
  @VisibleForTesting
  final AtomicReference<Map<String, MethodInfo>> serviceMethodMap
      = new AtomicReference<Map<String, MethodInfo>>();
  @VisibleForTesting
  final AtomicReference<Map<String, MethodInfo>> serviceMap
      = new AtomicReference<Map<String, MethodInfo>>();

  private final boolean retryEnabled;
  private final int maxRetryAttemptsLimit;
  private final int maxHedgedAttemptsLimit;

  // Setting this to true and observing this equal to true are run in different threads.
  private volatile boolean nameResolveComplete;

  ServiceConfigInterceptor(
      boolean retryEnabled, int maxRetryAttemptsLimit, int maxHedgedAttemptsLimit) {
    this.retryEnabled = retryEnabled;
    this.maxRetryAttemptsLimit = maxRetryAttemptsLimit;
    this.maxHedgedAttemptsLimit = maxHedgedAttemptsLimit;
  }

  void handleUpdate(@Nonnull Map<String, Object> serviceConfig) {
    Map<String, MethodInfo> newServiceMethodConfigs = new HashMap<String, MethodInfo>();
    Map<String, MethodInfo> newServiceConfigs = new HashMap<String, MethodInfo>();

    // Try and do as much validation here before we swap out the existing configuration.  In case
    // the input is invalid, we don't want to lose the existing configuration.

    List<Map<String, Object>> methodConfigs =
        ServiceConfigUtil.getMethodConfigFromServiceConfig(serviceConfig);
    if (methodConfigs == null) {
      logger.log(Level.FINE, "No method configs found, skipping");
      nameResolveComplete = true;
      return;
    }

    for (Map<String, Object> methodConfig : methodConfigs) {
      MethodInfo info = new MethodInfo(
          methodConfig, retryEnabled, maxRetryAttemptsLimit, maxHedgedAttemptsLimit);

      List<Map<String, Object>> nameList =
          ServiceConfigUtil.getNameListFromMethodConfig(methodConfig);

      checkArgument(
          nameList != null && !nameList.isEmpty(), "no names in method config %s", methodConfig);
      for (Map<String, Object> name : nameList) {
        String serviceName = ServiceConfigUtil.getServiceFromName(name);
        checkArgument(!Strings.isNullOrEmpty(serviceName), "missing service name");
        String methodName = ServiceConfigUtil.getMethodFromName(name);
        if (Strings.isNullOrEmpty(methodName)) {
          // Service scoped config
          checkArgument(
              !newServiceConfigs.containsKey(serviceName), "Duplicate service %s", serviceName);
          newServiceConfigs.put(serviceName, info);
        } else {
          // Method scoped config
          String fullMethodName = MethodDescriptor.generateFullMethodName(serviceName, methodName);
          checkArgument(
              !newServiceMethodConfigs.containsKey(fullMethodName),
              "Duplicate method name %s",
              fullMethodName);
          newServiceMethodConfigs.put(fullMethodName, info);
        }
      }
    }

    // Okay, service config is good, swap it.
    serviceMethodMap.set(Collections.unmodifiableMap(newServiceMethodConfigs));
    serviceMap.set(Collections.unmodifiableMap(newServiceConfigs));
    nameResolveComplete = true;
  }

  /**
   * Equivalent of MethodConfig from a ServiceConfig with restrictions from Channel setting.
   */
  static final class MethodInfo {
    final Long timeoutNanos;
    final Boolean waitForReady;
    final Integer maxInboundMessageSize;
    final Integer maxOutboundMessageSize;
    final RetryPolicy retryPolicy;
    final HedgingPolicy hedgingPolicy;

    /**
     * Constructor.
     *
     * @param retryEnabled when false, the argument maxRetryAttemptsLimit will have no effect.
     */
    MethodInfo(
        Map<String, Object> methodConfig, boolean retryEnabled, int maxRetryAttemptsLimit,
        int maxHedgedAttemptsLimit) {
      timeoutNanos = ServiceConfigUtil.getTimeoutFromMethodConfig(methodConfig);
      waitForReady = ServiceConfigUtil.getWaitForReadyFromMethodConfig(methodConfig);
      maxInboundMessageSize =
          ServiceConfigUtil.getMaxResponseMessageBytesFromMethodConfig(methodConfig);
      if (maxInboundMessageSize != null) {
        checkArgument(
            maxInboundMessageSize >= 0,
            "maxInboundMessageSize %s exceeds bounds", maxInboundMessageSize);
      }
      maxOutboundMessageSize =
          ServiceConfigUtil.getMaxRequestMessageBytesFromMethodConfig(methodConfig);
      if (maxOutboundMessageSize != null) {
        checkArgument(
            maxOutboundMessageSize >= 0,
            "maxOutboundMessageSize %s exceeds bounds", maxOutboundMessageSize);
      }

      Map<String, Object> retryPolicyMap =
          retryEnabled ? ServiceConfigUtil.getRetryPolicyFromMethodConfig(methodConfig) : null;
      retryPolicy = retryPolicyMap == null
          ? RetryPolicy.DEFAULT : retryPolicy(retryPolicyMap, maxRetryAttemptsLimit);

      Map<String, Object> hedgingPolicyMap =
          retryEnabled ? ServiceConfigUtil.getHedgingPolicyFromMethodConfig(methodConfig) : null;
      hedgingPolicy = hedgingPolicyMap == null
          ? HedgingPolicy.DEFAULT : hedgingPolicy(hedgingPolicyMap, maxHedgedAttemptsLimit);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(
          timeoutNanos, waitForReady, maxInboundMessageSize, maxOutboundMessageSize, retryPolicy);
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof MethodInfo)) {
        return false;
      }
      MethodInfo that = (MethodInfo) other;
      return Objects.equal(this.timeoutNanos, that.timeoutNanos)
          && Objects.equal(this.waitForReady, that.waitForReady)
          && Objects.equal(this.maxInboundMessageSize, that.maxInboundMessageSize)
          && Objects.equal(this.maxOutboundMessageSize, that.maxOutboundMessageSize)
          && Objects.equal(this.retryPolicy, that.retryPolicy);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("timeoutNanos", timeoutNanos)
          .add("waitForReady", waitForReady)
          .add("maxInboundMessageSize", maxInboundMessageSize)
          .add("maxOutboundMessageSize", maxOutboundMessageSize)
          .add("retryPolicy", retryPolicy)
          .toString();
    }

    @SuppressWarnings("BetaApi") // Verify is stabilized since Guava v24.0
    private static RetryPolicy retryPolicy(Map<String, Object> retryPolicy, int maxAttemptsLimit) {
      int maxAttempts = checkNotNull(
          ServiceConfigUtil.getMaxAttemptsFromRetryPolicy(retryPolicy),
          "maxAttempts cannot be empty");
      checkArgument(maxAttempts >= 2, "maxAttempts must be greater than 1: %s", maxAttempts);
      maxAttempts = Math.min(maxAttempts, maxAttemptsLimit);

      long initialBackoffNanos = checkNotNull(
          ServiceConfigUtil.getInitialBackoffNanosFromRetryPolicy(retryPolicy),
          "initialBackoff cannot be empty");
      checkArgument(
          initialBackoffNanos > 0,
          "initialBackoffNanos must be greater than 0: %s",
          initialBackoffNanos);

      long maxBackoffNanos = checkNotNull(
          ServiceConfigUtil.getMaxBackoffNanosFromRetryPolicy(retryPolicy),
          "maxBackoff cannot be empty");
      checkArgument(
          maxBackoffNanos > 0, "maxBackoff must be greater than 0: %s", maxBackoffNanos);

      double backoffMultiplier = checkNotNull(
          ServiceConfigUtil.getBackoffMultiplierFromRetryPolicy(retryPolicy),
          "backoffMultiplier cannot be empty");
      checkArgument(
          backoffMultiplier > 0,
          "backoffMultiplier must be greater than 0: %s",
          backoffMultiplier);

      List<String> rawCodes =
          ServiceConfigUtil.getRetryableStatusCodesFromRetryPolicy(retryPolicy);
      checkNotNull(rawCodes, "rawCodes must be present");
      checkArgument(!rawCodes.isEmpty(), "rawCodes can't be empty");
      EnumSet<Code> codes = EnumSet.noneOf(Code.class);
      // service config doesn't say if duplicates are allowed, so just accept them.
      for (String rawCode : rawCodes) {
        verify(!"OK".equals(rawCode), "rawCode can not be \"OK\"");
        codes.add(Code.valueOf(rawCode));
      }
      Set<Code> retryableStatusCodes = Collections.unmodifiableSet(codes);

      return new RetryPolicy(
          maxAttempts, initialBackoffNanos, maxBackoffNanos, backoffMultiplier,
          retryableStatusCodes);
    }
  }

  @SuppressWarnings("BetaApi") // Verify is stabilized since Guava v24.0
  private static HedgingPolicy hedgingPolicy(
      Map<String, Object> hedgingPolicy, int maxAttemptsLimit) {
    int maxAttempts = checkNotNull(
        ServiceConfigUtil.getMaxAttemptsFromHedgingPolicy(hedgingPolicy),
        "maxAttempts cannot be empty");
    checkArgument(maxAttempts >= 2, "maxAttempts must be greater than 1: %s", maxAttempts);
    maxAttempts = Math.min(maxAttempts, maxAttemptsLimit);

    long hedgingDelayNanos = checkNotNull(
        ServiceConfigUtil.getHedgingDelayNanosFromHedgingPolicy(hedgingPolicy),
        "hedgingDelay cannot be empty");
    checkArgument(
        hedgingDelayNanos >= 0, "hedgingDelay must not be negative: %s", hedgingDelayNanos);

    List<String> rawCodes =
        ServiceConfigUtil.getNonFatalStatusCodesFromHedgingPolicy(hedgingPolicy);
    checkNotNull(rawCodes, "rawCodes must be present");
    checkArgument(!rawCodes.isEmpty(), "rawCodes can't be empty");
    EnumSet<Code> codes = EnumSet.noneOf(Code.class);
    // service config doesn't say if duplicates are allowed, so just accept them.
    for (String rawCode : rawCodes) {
      verify(!"OK".equals(rawCode), "rawCode can not be \"OK\"");
      codes.add(Code.valueOf(rawCode));
    }
    Set<Code> nonFatalStatusCodes = Collections.unmodifiableSet(codes);

    return new HedgingPolicy(maxAttempts, hedgingDelayNanos, nonFatalStatusCodes);
  }

  static final CallOptions.Key<RetryPolicy.Provider> RETRY_POLICY_KEY =
      CallOptions.Key.create("internal-retry-policy");
  static final CallOptions.Key<HedgingPolicy.Provider> HEDGING_POLICY_KEY =
      CallOptions.Key.create("internal-hedging-policy");

  @SuppressWarnings("BetaApi") // Verify is stabilized since Guava v24.0
  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      final MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
    if (retryEnabled) {
      if (nameResolveComplete) {
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
            if (!nameResolveComplete) {
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
            if (!nameResolveComplete) {
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
    Map<String, MethodInfo> localServiceMethodMap = serviceMethodMap.get();
    MethodInfo info = null;
    if (localServiceMethodMap != null) {
      info = localServiceMethodMap.get(method.getFullMethodName());
    }
    if (info == null) {
      Map<String, MethodInfo> localServiceMap = serviceMap.get();
      if (localServiceMap != null) {
        info = localServiceMap.get(
            MethodDescriptor.extractFullServiceName(method.getFullMethodName()));
      }
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

/*
 * Copyright 2019 The gRPC Authors
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

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import com.google.common.base.Strings;
import io.grpc.MethodDescriptor;
import io.grpc.Status.Code;
import java.util.Collections;
import java.util.EnumSet;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * {@link ManagedChannelServiceConfig} is a fully parsed and validated representation of service
 * configuration data.
 */
final class ManagedChannelServiceConfig {

  private final Map<String, MethodInfo> serviceMethodMap;
  private final Map<String, MethodInfo> serviceMap;

  ManagedChannelServiceConfig(
      Map<String, MethodInfo> serviceMethodMap,
      Map<String, MethodInfo> serviceMap) {
    this.serviceMethodMap = Collections.unmodifiableMap(new HashMap<>(serviceMethodMap));
    this.serviceMap = Collections.unmodifiableMap(new HashMap<>(serviceMap));
  }

  static ManagedChannelServiceConfig fromServiceConfig(
      Map<String, ?> serviceConfig,
      boolean retryEnabled,
      int maxRetryAttemptsLimit,
      int maxHedgedAttemptsLimit) {
    Map<String, MethodInfo> serviceMethodMap = new HashMap<>();
    Map<String, MethodInfo> serviceMap = new HashMap<>();

    // Try and do as much validation here before we swap out the existing configuration.  In case
    // the input is invalid, we don't want to lose the existing configuration.

    List<Map<String, ?>> methodConfigs =
        ServiceConfigUtil.getMethodConfigFromServiceConfig(serviceConfig);

    if (methodConfigs == null) {
      // this is surprising, but possible.
      return new ManagedChannelServiceConfig(serviceMethodMap, serviceMap);
    }

    for (Map<String, ?> methodConfig : methodConfigs) {
      MethodInfo info = new MethodInfo(
          methodConfig, retryEnabled, maxRetryAttemptsLimit, maxHedgedAttemptsLimit);

      List<Map<String, ?>> nameList =
          ServiceConfigUtil.getNameListFromMethodConfig(methodConfig);

      checkArgument(
          nameList != null && !nameList.isEmpty(), "no names in method config %s", methodConfig);
      for (Map<String, ?> name : nameList) {
        String serviceName = ServiceConfigUtil.getServiceFromName(name);
        checkArgument(!Strings.isNullOrEmpty(serviceName), "missing service name");
        String methodName = ServiceConfigUtil.getMethodFromName(name);
        if (Strings.isNullOrEmpty(methodName)) {
          // Service scoped config
          checkArgument(
              !serviceMap.containsKey(serviceName), "Duplicate service %s", serviceName);
          serviceMap.put(serviceName, info);
        } else {
          // Method scoped config
          String fullMethodName = MethodDescriptor.generateFullMethodName(serviceName, methodName);
          checkArgument(
              !serviceMethodMap.containsKey(fullMethodName),
              "Duplicate method name %s",
              fullMethodName);
          serviceMethodMap.put(fullMethodName, info);
        }
      }
    }

    return new ManagedChannelServiceConfig(serviceMethodMap, serviceMap);
  }

  /**
   * Returns the per-service configuration for the channel.
   */
  Map<String, MethodInfo> getServiceMap() {
    return serviceMap;
  }

  /**
   * Returns the per-method configuration for the channel.
   */
  Map<String, MethodInfo> getServiceMethodMap() {
    return serviceMethodMap;
  }

  /**
   * Equivalent of MethodConfig from a ServiceConfig with restrictions from Channel setting.
   */
  static final class MethodInfo {
    // TODO(carl-mastrangelo): add getters for these fields and make them private.
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
        Map<String, ?> methodConfig, boolean retryEnabled, int maxRetryAttemptsLimit,
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

      Map<String, ?> retryPolicyMap =
          retryEnabled ? ServiceConfigUtil.getRetryPolicyFromMethodConfig(methodConfig) : null;
      retryPolicy = retryPolicyMap == null
          ? RetryPolicy.DEFAULT : retryPolicy(retryPolicyMap, maxRetryAttemptsLimit);

      Map<String, ?> hedgingPolicyMap =
          retryEnabled ? ServiceConfigUtil.getHedgingPolicyFromMethodConfig(methodConfig) : null;
      hedgingPolicy = hedgingPolicyMap == null
          ? HedgingPolicy.DEFAULT : hedgingPolicy(hedgingPolicyMap, maxHedgedAttemptsLimit);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(
          timeoutNanos,
          waitForReady,
          maxInboundMessageSize,
          maxOutboundMessageSize,
          retryPolicy,
          hedgingPolicy);
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
          && Objects.equal(this.retryPolicy, that.retryPolicy)
          && Objects.equal(this.hedgingPolicy, that.hedgingPolicy);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("timeoutNanos", timeoutNanos)
          .add("waitForReady", waitForReady)
          .add("maxInboundMessageSize", maxInboundMessageSize)
          .add("maxOutboundMessageSize", maxOutboundMessageSize)
          .add("retryPolicy", retryPolicy)
          .add("hedgingPolicy", hedgingPolicy)
          .toString();
    }

    private static RetryPolicy retryPolicy(Map<String, ?> retryPolicy, int maxAttemptsLimit) {
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

    private static HedgingPolicy hedgingPolicy(
        Map<String, ?> hedgingPolicy, int maxAttemptsLimit) {
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
  }
}

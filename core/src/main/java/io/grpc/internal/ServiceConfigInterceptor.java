/*
 * Copyright 2018, gRPC Authors All rights reserved.
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

  ServiceConfigInterceptor() {}

  void handleUpdate(Map<String, Object> serviceConfig) {
    Map<String, MethodInfo> newServiceMethodConfigs = new HashMap<String, MethodInfo>();
    Map<String, MethodInfo> newServiceConfigs = new HashMap<String, MethodInfo>();

    // Try and do as much validation here before we swap out the existing configuration.  In case
    // the input is invalid, we don't want to lose the existing configuration.

    List<Map<String, Object>> methodConfigs =
        ServiceConfigUtil.getMethodConfigFromServiceConfig(serviceConfig);
    if (methodConfigs == null) {
      logger.log(Level.FINE, "No method configs found, skipping");
      return;
    }

    for (Map<String, Object> methodConfig : methodConfigs) {
      MethodInfo info = new MethodInfo(methodConfig);

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
  }

  /**
   * Equivalent of MethodConfig from a ServiceConfig.
   */
  static final class MethodInfo {
    final Long timeoutNanos;
    final Boolean waitForReady;
    final Integer maxInboundMessageSize;
    final Integer maxOutboundMessageSize;
    final RetryPolicy retryPolicy;

    MethodInfo(Map<String, Object> methodConfig) {
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

      Map<String, Object> policy = ServiceConfigUtil.getRetryPolicyFromMethodConfig(methodConfig);
      retryPolicy = policy == null ? null : new RetryPolicy(policy);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(
          timeoutNanos, waitForReady, maxInboundMessageSize, maxOutboundMessageSize);
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
          && Objects.equal(this.maxOutboundMessageSize, that.maxOutboundMessageSize);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("timeoutNanos", timeoutNanos)
          .add("waitForReady", waitForReady)
          .add("maxInboundMessageSize", maxInboundMessageSize)
          .add("maxOutboundMessageSize", maxOutboundMessageSize)
          .toString();
    }

    static final class RetryPolicy {
      final int maxAttempts;
      final long initialBackoffNanos;
      final long maxBackoffNanos;
      final double backoffMultiplier;
      final Set<Code> retryableStatusCodes;

      RetryPolicy(Map<String, Object> retryPolicy) {
        maxAttempts = checkNotNull(
            ServiceConfigUtil.getMaxAttemptsFromRetryPolicy(retryPolicy),
            "maxAttempts cannot be empty");
        checkArgument(maxAttempts >= 2, "maxAttempts must be greater than 1: %s", maxAttempts);

        initialBackoffNanos = checkNotNull(
            ServiceConfigUtil.getInitialBackoffNanosFromRetryPolicy(retryPolicy),
            "initialBackoff cannot be empty");
        checkArgument(
            initialBackoffNanos > 0,
            "initialBackoffNanos must be greater than 0: %s",
            initialBackoffNanos);

        maxBackoffNanos = checkNotNull(
            ServiceConfigUtil.getMaxBackoffNanosFromRetryPolicy(retryPolicy),
            "maxBackoff cannot be empty");
        checkArgument(
            maxBackoffNanos > 0, "maxBackoff must be greater than 0: %s", maxBackoffNanos);

        backoffMultiplier = checkNotNull(
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
          codes.add(Code.valueOf(rawCode));
        }
        retryableStatusCodes = Collections.unmodifiableSet(codes);
      }

      @VisibleForTesting
      RetryPolicy(
          int maxAttempts,
          long initialBackoffNanos,
          long maxBackoffNanos,
          double backoffMultiplier,
          Set<Code> retryableStatusCodes) {
        this.maxAttempts = maxAttempts;
        this.initialBackoffNanos = initialBackoffNanos;
        this.maxBackoffNanos = maxBackoffNanos;
        this.backoffMultiplier = backoffMultiplier;
        this.retryableStatusCodes = retryableStatusCodes;
      }

      @Override
      public int hashCode() {
        return Objects.hashCode(
            maxAttempts,
            initialBackoffNanos,
            maxBackoffNanos,
            backoffMultiplier,
            retryableStatusCodes);
      }

      @Override
      public boolean equals(Object other) {
        if (!(other instanceof RetryPolicy)) {
          return false;
        }
        RetryPolicy that = (RetryPolicy) other;
        return Objects.equal(this.maxAttempts, that.maxAttempts)
            && Objects.equal(this.initialBackoffNanos, that.initialBackoffNanos)
            && Objects.equal(this.maxBackoffNanos, that.maxBackoffNanos)
            && Objects.equal(this.backoffMultiplier, that.backoffMultiplier)
            && Objects.equal(this.retryableStatusCodes, that.retryableStatusCodes);
      }

      @Override
      public String toString() {
        return MoreObjects.toStringHelper(this)
            .add("maxAttempts", maxAttempts)
            .add("initialBackoffNanos", initialBackoffNanos)
            .add("maxBackoffNanos", maxBackoffNanos)
            .add("backoffMultiplier", backoffMultiplier)
            .add("retryableStatusCodes", retryableStatusCodes)
            .toString();
      }

    }
  }

  static final CallOptions.Key<MethodInfo.RetryPolicy> RETRY_POLICY_KEY =
      CallOptions.Key.of("internal-retry-policy", null);

  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next) {
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
    if (info.retryPolicy != null) {
      callOptions = callOptions.withOption(RETRY_POLICY_KEY, info.retryPolicy);
    }

    return next.newCall(method, callOptions);
  }
}

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

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import java.util.List;
import javax.annotation.Nullable;

/**
 * Represents a ServiceConfig as described at
 * https://github.com/grpc/grpc/blob/master/doc/service_config.md
 */
public final class ServiceConfiguration {

  private ServiceConfiguration() {}

  /**
   * Represents a {@link Name}.
   */
  public static final class Name {
    private String service;
    private String method;

    /**
     * Constructs a {@link Name}.
     *
     * @param service required
     * @param method required
     *
     */
    public Name(@Nullable String service, @Nullable String method) {
      this.service = service;
      this.method = method;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      Name name = (Name) o;
      return Objects.equal(service, name.service)
          && Objects.equal(method, name.method);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(service, method);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("service", service)
          .add("method", method)
          .toString();
    }
  }

  /**
   * Represents a {@link RetryPolicy}.
   */
  public static final class RetryPolicy {
    private int maxRetryAttempts;
    private String initialBackoff;
    private String maxBackoff;
    private float backoffMultiplier;
    private List<String> retryableStatusCodes;

    /**
     * Constructs a {@link RetryPolicy}.
     *
     * @param retryableStatusCodes cannot be empty
     */
    public RetryPolicy(
        int maxRetryAttempts,
        @Nullable String initialBackoff,
        @Nullable String maxBackoff,
        float backoffMultiplier,
        @Nullable List<String> retryableStatusCodes) {
      this.maxRetryAttempts = maxRetryAttempts;
      this.initialBackoff = initialBackoff;
      this.maxBackoff = maxBackoff;
      this.backoffMultiplier = backoffMultiplier;
      this.retryableStatusCodes = retryableStatusCodes;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      RetryPolicy that = (RetryPolicy) o;
      return maxRetryAttempts == that.maxRetryAttempts
          && Float.compare(that.backoffMultiplier, backoffMultiplier) == 0
          && Objects.equal(initialBackoff, that.initialBackoff)
          && Objects.equal(maxBackoff, that.maxBackoff)
          && Objects.equal(retryableStatusCodes, that.retryableStatusCodes);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(maxRetryAttempts, initialBackoff, maxBackoff, backoffMultiplier,
          retryableStatusCodes);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("maxRetryAttempts", maxRetryAttempts)
          .add("initialBackoff", initialBackoff)
          .add("maxBackoff", maxBackoff)
          .add("backoffMultiplier", backoffMultiplier)
          .add("retryableStatusCodes", retryableStatusCodes)
          .toString();
    }
  }

  /**
   * Represents a {@link HedgingPolicy}.
   */
  public static final class HedgingPolicy {
    private int maxRequests;
    private String hedgingDelay;
    private List<String> nonFatalStatusCodes;

    /**
     * Constructs a {@link RetryPolicy}.
     *
     * @param nonFatalStatusCodes must not be empty
     */
    public HedgingPolicy(
        int maxRequests,
        @Nullable String hedgingDelay,
        @Nullable List<String> nonFatalStatusCodes) {
      this.maxRequests = maxRequests;
      this.hedgingDelay = hedgingDelay;
      this.nonFatalStatusCodes = nonFatalStatusCodes;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      HedgingPolicy that = (HedgingPolicy) o;
      return maxRequests == that.maxRequests
          && Objects.equal(hedgingDelay, that.hedgingDelay)
          && Objects.equal(nonFatalStatusCodes, that.nonFatalStatusCodes);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(maxRequests, hedgingDelay, nonFatalStatusCodes);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("maxRequests", maxRequests)
          .add("hedgingDelay", hedgingDelay)
          .add("nonFatalStatusCodes", nonFatalStatusCodes)
          .toString();
    }
  }

  /**
   * Represents a {@link MethodConfig}.
   */
  public static final class MethodConfig {
    private List<Name> name;
    private Boolean waitForReady;
    private String timeout;
    private Integer maxRequestMessageBytes;
    private Integer maxResponseMessageBytes;
    private RetryPolicy retryPolicy;
    private HedgingPolicy hedgingPolicy;

    /**
     * Constructs a {@link MethodConfig}.
     */
    public MethodConfig(
        @Nullable List<Name> name,
        @Nullable Boolean waitForReady,
        @Nullable String timeout,
        @Nullable Integer maxRequestMessageBytes,
        @Nullable Integer maxResponseMessageBytes,
        @Nullable RetryPolicy retryPolicy,
        @Nullable HedgingPolicy hedgingPolicy) {
      this.name = name;
      this.waitForReady = waitForReady;
      this.timeout = timeout;
      this.maxRequestMessageBytes = maxRequestMessageBytes;
      this.maxResponseMessageBytes = maxResponseMessageBytes;
      this.retryPolicy = retryPolicy;
      this.hedgingPolicy = hedgingPolicy;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      MethodConfig that = (MethodConfig) o;
      return Objects.equal(name, that.name)
          && Objects.equal(waitForReady, that.waitForReady)
          && Objects.equal(timeout, that.timeout)
          && Objects.equal(maxRequestMessageBytes, that.maxRequestMessageBytes)
          && Objects.equal(maxResponseMessageBytes, that.maxResponseMessageBytes)
          && Objects.equal(retryPolicy, that.retryPolicy)
          && Objects.equal(hedgingPolicy, that.hedgingPolicy);
    }

    @Override
    public int hashCode() {
      return Objects
          .hashCode(name, waitForReady, timeout, maxRequestMessageBytes, maxResponseMessageBytes,
              retryPolicy, hedgingPolicy);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("name", name)
          .add("waitForReady", waitForReady)
          .add("timeout", timeout)
          .add("maxRequestMessageBytes", maxRequestMessageBytes)
          .add("maxResponseMessageBytes", maxResponseMessageBytes)
          .add("retryPolicy", retryPolicy)
          .add("hedgingPolicy", hedgingPolicy)
          .toString();
    }
  }

  /**
   * Represents a {@link RetryThrottlingPolicy}.
   */
  public static final class RetryThrottlingPolicy {
    private int maxTokens;
    private float tokenRatio;

    /**
     * Constructs a {@link RetryThrottlingPolicy}.
     */
    public RetryThrottlingPolicy(int maxTokens, float tokenRatio) {
      this.maxTokens = maxTokens;
      this.tokenRatio = tokenRatio;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      RetryThrottlingPolicy that = (RetryThrottlingPolicy) o;
      return maxTokens == that.maxTokens
          && Float.compare(that.tokenRatio, tokenRatio) == 0;
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(maxTokens, tokenRatio);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("maxTokens", maxTokens)
          .add("tokenRatio", tokenRatio)
          .toString();
    }
  }

  /**
   * Represents a {@link ServiceConfig}.
   */
  public static final class ServiceConfig {
    private String loadBalancingPolicy;
    private List<MethodConfig> methodConfig;
    private RetryThrottlingPolicy retryThrottling;

    /**
     * Constructs a {@link ServiceConfig}.
     */
    public ServiceConfig(
        @Nullable String loadBalancingPolicy,
        @Nullable List<MethodConfig> methodConfig,
        @Nullable RetryThrottlingPolicy retryThrottling) {
      this.loadBalancingPolicy = loadBalancingPolicy;
      this.methodConfig = methodConfig;
      this.retryThrottling = retryThrottling;
    }

    @Override
    public boolean equals(Object o) {
      if (this == o) {
        return true;
      }
      if (o == null || getClass() != o.getClass()) {
        return false;
      }
      ServiceConfig that = (ServiceConfig) o;
      return Objects.equal(loadBalancingPolicy, that.loadBalancingPolicy)
          && Objects.equal(methodConfig, that.methodConfig)
          && Objects.equal(retryThrottling, that.retryThrottling);
    }

    @Override
    public int hashCode() {
      return Objects.hashCode(loadBalancingPolicy, methodConfig, retryThrottling);
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("loadBalancingPolicy", loadBalancingPolicy)
          .add("methodConfig", methodConfig)
          .add("retryThrottling", retryThrottling)
          .toString();
    }
  }
}

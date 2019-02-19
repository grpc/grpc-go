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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;
import static com.google.common.math.LongMath.checkedAdd;

import com.google.common.annotations.VisibleForTesting;
import io.grpc.internal.RetriableStream.Throttle;
import java.text.ParseException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

/**
 * Helper utility to work with service configs.
 */
public final class ServiceConfigUtil {

  private static final String SERVICE_CONFIG_METHOD_CONFIG_KEY = "methodConfig";
  private static final String SERVICE_CONFIG_LOAD_BALANCING_POLICY_KEY = "loadBalancingPolicy";
  private static final String SERVICE_CONFIG_LOAD_BALANCING_CONFIG_KEY = "loadBalancingConfig";
  private static final String XDS_CONFIG_BALANCER_NAME_KEY = "balancerName";
  private static final String XDS_CONFIG_CHILD_POLICY_KEY = "childPolicy";
  private static final String XDS_CONFIG_FALLBACK_POLICY_KEY = "fallbackPolicy";
  private static final String SERVICE_CONFIG_STICKINESS_METADATA_KEY = "stickinessMetadataKey";
  private static final String METHOD_CONFIG_NAME_KEY = "name";
  private static final String METHOD_CONFIG_TIMEOUT_KEY = "timeout";
  private static final String METHOD_CONFIG_WAIT_FOR_READY_KEY = "waitForReady";
  private static final String METHOD_CONFIG_MAX_REQUEST_MESSAGE_BYTES_KEY =
      "maxRequestMessageBytes";
  private static final String METHOD_CONFIG_MAX_RESPONSE_MESSAGE_BYTES_KEY =
      "maxResponseMessageBytes";
  private static final String METHOD_CONFIG_RETRY_POLICY_KEY = "retryPolicy";
  private static final String METHOD_CONFIG_HEDGING_POLICY_KEY = "hedgingPolicy";
  private static final String NAME_SERVICE_KEY = "service";
  private static final String NAME_METHOD_KEY = "method";
  private static final String RETRY_POLICY_MAX_ATTEMPTS_KEY = "maxAttempts";
  private static final String RETRY_POLICY_INITIAL_BACKOFF_KEY = "initialBackoff";
  private static final String RETRY_POLICY_MAX_BACKOFF_KEY = "maxBackoff";
  private static final String RETRY_POLICY_BACKOFF_MULTIPLIER_KEY = "backoffMultiplier";
  private static final String RETRY_POLICY_RETRYABLE_STATUS_CODES_KEY = "retryableStatusCodes";
  private static final String HEDGING_POLICY_MAX_ATTEMPTS_KEY = "maxAttempts";
  private static final String HEDGING_POLICY_HEDGING_DELAY_KEY = "hedgingDelay";
  private static final String HEDGING_POLICY_NON_FATAL_STATUS_CODES_KEY = "nonFatalStatusCodes";

  private static final long DURATION_SECONDS_MIN = -315576000000L;
  private static final long DURATION_SECONDS_MAX = 315576000000L;

  private ServiceConfigUtil() {}

  /**
   * Fetch the health-checked service name from service config. {@code null} if can't find one.
   */
  @Nullable
  public static String getHealthCheckedServiceName(@Nullable Map<String, Object> serviceConfig) {
    String healthCheckKey = "healthCheckConfig";
    String serviceNameKey = "serviceName";
    if (serviceConfig == null || !serviceConfig.containsKey(healthCheckKey)) {
      return null;
    }

    /* schema as follows
    {
      "healthCheckConfig": {
        // Service name to use in the health-checking request.
        "serviceName": string
      }
    }
    */
    Map<String, Object> healthCheck = getObject(serviceConfig, healthCheckKey);
    if (!healthCheck.containsKey(serviceNameKey)) {
      return null;
    }
    return getString(healthCheck, "serviceName");
  }

  @Nullable
  static Throttle getThrottlePolicy(@Nullable Map<String, Object> serviceConfig) {
    String retryThrottlingKey = "retryThrottling";
    if (serviceConfig == null || !serviceConfig.containsKey(retryThrottlingKey)) {
      return null;
    }

    /* schema as follows
    {
      "retryThrottling": {
        // The number of tokens starts at maxTokens. The token_count will always be
        // between 0 and maxTokens.
        //
        // This field is required and must be greater than zero.
        "maxTokens": number,

        // The amount of tokens to add on each successful RPC. Typically this will
        // be some number between 0 and 1, e.g., 0.1.
        //
        // This field is required and must be greater than zero. Up to 3 decimal
        // places are supported.
        "tokenRatio": number
      }
    }
    */

    Map<String, Object> throttling = getObject(serviceConfig, retryThrottlingKey);

    float maxTokens = getDouble(throttling, "maxTokens").floatValue();
    float tokenRatio = getDouble(throttling, "tokenRatio").floatValue();
    checkState(maxTokens > 0f, "maxToken should be greater than zero");
    checkState(tokenRatio > 0f, "tokenRatio should be greater than zero");
    return new Throttle(maxTokens, tokenRatio);
  }

  @Nullable
  static Integer getMaxAttemptsFromRetryPolicy(Map<String, Object> retryPolicy) {
    if (!retryPolicy.containsKey(RETRY_POLICY_MAX_ATTEMPTS_KEY)) {
      return null;
    }
    return getDouble(retryPolicy, RETRY_POLICY_MAX_ATTEMPTS_KEY).intValue();
  }

  @Nullable
  static Long getInitialBackoffNanosFromRetryPolicy(Map<String, Object> retryPolicy) {
    if (!retryPolicy.containsKey(RETRY_POLICY_INITIAL_BACKOFF_KEY)) {
      return null;
    }
    String rawInitialBackoff = getString(retryPolicy, RETRY_POLICY_INITIAL_BACKOFF_KEY);
    try {
      return parseDuration(rawInitialBackoff);
    } catch (ParseException e) {
      throw new RuntimeException(e);
    }
  }

  @Nullable
  static Long getMaxBackoffNanosFromRetryPolicy(Map<String, Object> retryPolicy) {
    if (!retryPolicy.containsKey(RETRY_POLICY_MAX_BACKOFF_KEY)) {
      return null;
    }
    String rawMaxBackoff = getString(retryPolicy, RETRY_POLICY_MAX_BACKOFF_KEY);
    try {
      return parseDuration(rawMaxBackoff);
    } catch (ParseException e) {
      throw new RuntimeException(e);
    }
  }

  @Nullable
  static Double getBackoffMultiplierFromRetryPolicy(Map<String, Object> retryPolicy) {
    if (!retryPolicy.containsKey(RETRY_POLICY_BACKOFF_MULTIPLIER_KEY)) {
      return null;
    }
    return getDouble(retryPolicy, RETRY_POLICY_BACKOFF_MULTIPLIER_KEY);
  }

  @Nullable
  static List<String> getRetryableStatusCodesFromRetryPolicy(Map<String, Object> retryPolicy) {
    if (!retryPolicy.containsKey(RETRY_POLICY_RETRYABLE_STATUS_CODES_KEY)) {
      return null;
    }
    return checkStringList(getList(retryPolicy, RETRY_POLICY_RETRYABLE_STATUS_CODES_KEY));
  }

  @Nullable
  static Integer getMaxAttemptsFromHedgingPolicy(Map<String, Object> hedgingPolicy) {
    if (!hedgingPolicy.containsKey(HEDGING_POLICY_MAX_ATTEMPTS_KEY)) {
      return null;
    }
    return getDouble(hedgingPolicy, HEDGING_POLICY_MAX_ATTEMPTS_KEY).intValue();
  }

  @Nullable
  static Long getHedgingDelayNanosFromHedgingPolicy(Map<String, Object> hedgingPolicy) {
    if (!hedgingPolicy.containsKey(HEDGING_POLICY_HEDGING_DELAY_KEY)) {
      return null;
    }
    String rawHedgingDelay = getString(hedgingPolicy, HEDGING_POLICY_HEDGING_DELAY_KEY);
    try {
      return parseDuration(rawHedgingDelay);
    } catch (ParseException e) {
      throw new RuntimeException(e);
    }
  }

  @Nullable
  static List<String> getNonFatalStatusCodesFromHedgingPolicy(Map<String, Object> hedgingPolicy) {
    if (!hedgingPolicy.containsKey(HEDGING_POLICY_NON_FATAL_STATUS_CODES_KEY)) {
      return null;
    }
    return checkStringList(getList(hedgingPolicy, HEDGING_POLICY_NON_FATAL_STATUS_CODES_KEY));
  }

  @Nullable
  static String getServiceFromName(Map<String, Object> name) {
    if (!name.containsKey(NAME_SERVICE_KEY)) {
      return null;
    }
    return getString(name, NAME_SERVICE_KEY);
  }

  @Nullable
  static String getMethodFromName(Map<String, Object> name) {
    if (!name.containsKey(NAME_METHOD_KEY)) {
      return null;
    }
    return getString(name, NAME_METHOD_KEY);
  }

  @Nullable
  static Map<String, Object> getRetryPolicyFromMethodConfig(Map<String, Object> methodConfig) {
    if (!methodConfig.containsKey(METHOD_CONFIG_RETRY_POLICY_KEY)) {
      return null;
    }
    return getObject(methodConfig, METHOD_CONFIG_RETRY_POLICY_KEY);
  }

  @Nullable
  static Map<String, Object> getHedgingPolicyFromMethodConfig(Map<String, Object> methodConfig) {
    if (!methodConfig.containsKey(METHOD_CONFIG_HEDGING_POLICY_KEY)) {
      return null;
    }
    return getObject(methodConfig, METHOD_CONFIG_HEDGING_POLICY_KEY);
  }

  @Nullable
  static List<Map<String, Object>> getNameListFromMethodConfig(Map<String, Object> methodConfig) {
    if (!methodConfig.containsKey(METHOD_CONFIG_NAME_KEY)) {
      return null;
    }
    return checkObjectList(getList(methodConfig, METHOD_CONFIG_NAME_KEY));
  }

  /**
   * Returns the number of nanoseconds of timeout for the given method config.
   *
   * @return duration nanoseconds, or {@code null} if it isn't present.
   */
  @Nullable
  static Long getTimeoutFromMethodConfig(Map<String, Object> methodConfig) {
    if (!methodConfig.containsKey(METHOD_CONFIG_TIMEOUT_KEY)) {
      return null;
    }
    String rawTimeout = getString(methodConfig, METHOD_CONFIG_TIMEOUT_KEY);
    try {
      return parseDuration(rawTimeout);
    } catch (ParseException e) {
      throw new RuntimeException(e);
    }
  }

  @Nullable
  static Boolean getWaitForReadyFromMethodConfig(Map<String, Object> methodConfig) {
    if (!methodConfig.containsKey(METHOD_CONFIG_WAIT_FOR_READY_KEY)) {
      return null;
    }
    return getBoolean(methodConfig, METHOD_CONFIG_WAIT_FOR_READY_KEY);
  }

  @Nullable
  static Integer getMaxRequestMessageBytesFromMethodConfig(Map<String, Object> methodConfig) {
    if (!methodConfig.containsKey(METHOD_CONFIG_MAX_REQUEST_MESSAGE_BYTES_KEY)) {
      return null;
    }
    return getDouble(methodConfig, METHOD_CONFIG_MAX_REQUEST_MESSAGE_BYTES_KEY).intValue();
  }

  @Nullable
  static Integer getMaxResponseMessageBytesFromMethodConfig(Map<String, Object> methodConfig) {
    if (!methodConfig.containsKey(METHOD_CONFIG_MAX_RESPONSE_MESSAGE_BYTES_KEY)) {
      return null;
    }
    return getDouble(methodConfig, METHOD_CONFIG_MAX_RESPONSE_MESSAGE_BYTES_KEY).intValue();
  }

  @Nullable
  static List<Map<String, Object>> getMethodConfigFromServiceConfig(
      Map<String, Object> serviceConfig) {
    if (!serviceConfig.containsKey(SERVICE_CONFIG_METHOD_CONFIG_KEY)) {
      return null;
    }
    return checkObjectList(getList(serviceConfig, SERVICE_CONFIG_METHOD_CONFIG_KEY));
  }

  /**
   * Extracts load balancing configs from a service config.
   */
  @SuppressWarnings("unchecked")
  @VisibleForTesting
  public static List<Map<String, Object>> getLoadBalancingConfigsFromServiceConfig(
      Map<String, Object> serviceConfig) {
    /* schema as follows
    {
      "loadBalancingConfig": {
        [
          {"xds" :
            {
              "balancerName": "balancer1",
              "childPolicy": [...],
              "fallbackPolicy": [...],
            }
          },
          {"round_robin": {}}
        ]
      },
      "loadBalancingPolicy": "ROUND_ROBIN"  // The deprecated policy key
    }
    */
    List<Map<String, Object>> lbConfigs = new ArrayList<>();
    if (serviceConfig.containsKey(SERVICE_CONFIG_LOAD_BALANCING_CONFIG_KEY)) {
      List<Object> configs = getList(serviceConfig, SERVICE_CONFIG_LOAD_BALANCING_CONFIG_KEY);
      for (Object config : configs) {
        lbConfigs.add((Map<String, Object>) config);
      }
    }
    if (lbConfigs.isEmpty()) {
      // No LoadBalancingConfig found.  Fall back to the deprecated LoadBalancingPolicy
      if (serviceConfig.containsKey(SERVICE_CONFIG_LOAD_BALANCING_POLICY_KEY)) {
        String policy = getString(serviceConfig, SERVICE_CONFIG_LOAD_BALANCING_POLICY_KEY);
        // Convert the policy to a config, so that the caller can handle them in the same way.
        policy = policy.toLowerCase(Locale.ROOT);
        Map<String, Object> fakeConfig =
            Collections.singletonMap(policy, (Object) Collections.emptyMap());
        lbConfigs.add(fakeConfig);
      }
    }
    return Collections.unmodifiableList(lbConfigs);
  }

  /**
   * Extracts the loadbalancing policy name from loadbalancer config.
   */
  public static String getBalancerPolicyNameFromLoadBalancingConfig(Map<String, Object> lbConfig) {
    return lbConfig.entrySet().iterator().next().getKey();
  }

  /**
   * Extracts the loadbalancer name from xds loadbalancer config.
   */
  @SuppressWarnings("unchecked")
  public static String getBalancerNameFromXdsConfig(
      Map<String, Object> xdsConfig) {
    Object entry = xdsConfig.entrySet().iterator().next().getValue();
    return getString((Map<String, Object>) entry, XDS_CONFIG_BALANCER_NAME_KEY);
  }

  /**
   * Extracts list of child policies from xds loadbalancer config.
   */
  @SuppressWarnings("unchecked")
  @Nullable
  public static List<Map<String, Object>> getChildPolicyFromXdsConfig(
      Map<String, Object> xdsConfig) {
    Object rawEntry = xdsConfig.entrySet().iterator().next().getValue();
    if (rawEntry instanceof Map) {
      Map<String, Object> entry = (Map<String, Object>) rawEntry;
      if (entry.containsKey(XDS_CONFIG_CHILD_POLICY_KEY)) {
        return (List<Map<String, Object>>) (List<?>) getList(entry, XDS_CONFIG_CHILD_POLICY_KEY);
      }
    }
    return null;
  }

  /**
   * Extracts list of fallback policies from xds loadbalancer config.
   */
  @SuppressWarnings("unchecked")
  @Nullable
  public static List<Map<String, Object>> getFallbackPolicyFromXdsConfig(
      Map<String, Object> lbConfig) {
    Object rawEntry = lbConfig.entrySet().iterator().next().getValue();
    if (rawEntry instanceof Map) {
      Map<String, Object> entry = (Map<String, Object>) rawEntry;
      if (entry.containsKey(XDS_CONFIG_FALLBACK_POLICY_KEY)) {
        return (List<Map<String, Object>>) (List<?>) getList(entry, XDS_CONFIG_FALLBACK_POLICY_KEY);
      }
    }
    return null;
  }

  /**
   * Extracts the stickiness metadata key from a service config, or {@code null}.
   */
  @Nullable
  public static String getStickinessMetadataKeyFromServiceConfig(
      Map<String, Object> serviceConfig) {
    if (!serviceConfig.containsKey(SERVICE_CONFIG_STICKINESS_METADATA_KEY)) {
      return null;
    }
    return getString(serviceConfig, SERVICE_CONFIG_STICKINESS_METADATA_KEY);
  }

  /**
   * Gets a list from an object for the given key.
   */
  @SuppressWarnings("unchecked")
  static List<Object> getList(Map<String, Object> obj, String key) {
    assert obj.containsKey(key);
    Object value = checkNotNull(obj.get(key), "no such key %s", key);
    if (value instanceof List) {
      return (List<Object>) value;
    }
    throw new ClassCastException(
        String.format("value %s for key %s in %s is not List", value, key, obj));
  }

  /**
   * Gets an object from an object for the given key.
   */
  @SuppressWarnings("unchecked")
  static Map<String, Object> getObject(Map<String, Object> obj, String key) {
    assert obj.containsKey(key);
    Object value = checkNotNull(obj.get(key), "no such key %s", key);
    if (value instanceof Map) {
      return (Map<String, Object>) value;
    }
    throw new ClassCastException(
        String.format("value %s for key %s in %s is not object", value, key, obj));
  }

  /**
   * Gets a double from an object for the given key.
   */
  @SuppressWarnings("unchecked")
  static Double getDouble(Map<String, Object> obj, String key) {
    assert obj.containsKey(key);
    Object value = checkNotNull(obj.get(key), "no such key %s", key);
    if (value instanceof Double) {
      return (Double) value;
    }
    throw new ClassCastException(
        String.format("value %s for key %s in %s is not Double", value, key, obj));
  }

  /**
   * Gets a string from an object for the given key.
   */
  @SuppressWarnings("unchecked")
  static String getString(Map<String, Object> obj, String key) {
    assert obj.containsKey(key);
    Object value = checkNotNull(obj.get(key), "no such key %s", key);
    if (value instanceof String) {
      return (String) value;
    }
    throw new ClassCastException(
        String.format("value %s for key %s in %s is not String", value, key, obj));
  }

  /**
   * Gets a string from an object for the given index.
   */
  @SuppressWarnings("unchecked")
  static String getString(List<Object> list, int i) {
    assert i >= 0 && i < list.size();
    Object value = checkNotNull(list.get(i), "idx %s in %s is null", i, list);
    if (value instanceof String) {
      return (String) value;
    }
    throw new ClassCastException(
        String.format("value %s for idx %d in %s is not String", value, i, list));
  }

  /**
   * Gets a boolean from an object for the given key.
   */
  static Boolean getBoolean(Map<String, Object> obj, String key) {
    assert obj.containsKey(key);
    Object value = checkNotNull(obj.get(key), "no such key %s", key);
    if (value instanceof Boolean) {
      return (Boolean) value;
    }
    throw new ClassCastException(
        String.format("value %s for key %s in %s is not Boolean", value, key, obj));
  }

  @SuppressWarnings("unchecked")
  private static List<Map<String, Object>> checkObjectList(List<Object> rawList) {
    for (int i = 0; i < rawList.size(); i++) {
      if (!(rawList.get(i) instanceof Map)) {
        throw new ClassCastException(
            String.format("value %s for idx %d in %s is not object", rawList.get(i), i, rawList));
      }
    }
    return (List<Map<String, Object>>) (List<?>) rawList;
  }

  @SuppressWarnings("unchecked")
  static List<String> checkStringList(List<Object> rawList) {
    for (int i = 0; i < rawList.size(); i++) {
      if (!(rawList.get(i) instanceof String)) {
        throw new ClassCastException(
            String.format("value %s for idx %d in %s is not string", rawList.get(i), i, rawList));
      }
    }
    return (List<String>) (List<?>) rawList;
  }

  /**
   * Parse from a string to produce a duration.  Copy of
   * {@link com.google.protobuf.util.Durations#parse}.
   *
   * @return A Duration parsed from the string.
   * @throws ParseException if parsing fails.
   */
  private static long parseDuration(String value) throws ParseException {
    // Must ended with "s".
    if (value.isEmpty() || value.charAt(value.length() - 1) != 's') {
      throw new ParseException("Invalid duration string: " + value, 0);
    }
    boolean negative = false;
    if (value.charAt(0) == '-') {
      negative = true;
      value = value.substring(1);
    }
    String secondValue = value.substring(0, value.length() - 1);
    String nanoValue = "";
    int pointPosition = secondValue.indexOf('.');
    if (pointPosition != -1) {
      nanoValue = secondValue.substring(pointPosition + 1);
      secondValue = secondValue.substring(0, pointPosition);
    }
    long seconds = Long.parseLong(secondValue);
    int nanos = nanoValue.isEmpty() ? 0 : parseNanos(nanoValue);
    if (seconds < 0) {
      throw new ParseException("Invalid duration string: " + value, 0);
    }
    if (negative) {
      seconds = -seconds;
      nanos = -nanos;
    }
    try {
      return normalizedDuration(seconds, nanos);
    } catch (IllegalArgumentException e) {
      throw new ParseException("Duration value is out of range.", 0);
    }
  }

  /**
   * Copy of {@link com.google.protobuf.util.Timestamps#parseNanos}.
   */
  private static int parseNanos(String value) throws ParseException {
    int result = 0;
    for (int i = 0; i < 9; ++i) {
      result = result * 10;
      if (i < value.length()) {
        if (value.charAt(i) < '0' || value.charAt(i) > '9') {
          throw new ParseException("Invalid nanoseconds.", 0);
        }
        result += value.charAt(i) - '0';
      }
    }
    return result;
  }

  private static final long NANOS_PER_SECOND = TimeUnit.SECONDS.toNanos(1);

  /**
   * Copy of {@link com.google.protobuf.util.Durations#normalizedDuration}.
   */
  @SuppressWarnings("NarrowingCompoundAssignment")
  private static long normalizedDuration(long seconds, int nanos) {
    if (nanos <= -NANOS_PER_SECOND || nanos >= NANOS_PER_SECOND) {
      seconds = checkedAdd(seconds, nanos / NANOS_PER_SECOND);
      nanos %= NANOS_PER_SECOND;
    }
    if (seconds > 0 && nanos < 0) {
      nanos += NANOS_PER_SECOND; // no overflow since nanos is negative (and we're adding)
      seconds--; // no overflow since seconds is positive (and we're decrementing)
    }
    if (seconds < 0 && nanos > 0) {
      nanos -= NANOS_PER_SECOND; // no overflow since nanos is positive (and we're subtracting)
      seconds++; // no overflow since seconds is negative (and we're incrementing)
    }
    if (!durationIsValid(seconds, nanos)) {
      throw new IllegalArgumentException(String.format(
          "Duration is not valid. See proto definition for valid values. "
              + "Seconds (%s) must be in range [-315,576,000,000, +315,576,000,000]. "
              + "Nanos (%s) must be in range [-999,999,999, +999,999,999]. "
              + "Nanos must have the same sign as seconds", seconds, nanos));
    }
    return saturatedAdd(TimeUnit.SECONDS.toNanos(seconds), nanos);
  }

  /**
   * Returns true if the given number of seconds and nanos is a valid {@code Duration}. The {@code
   * seconds} value must be in the range [-315,576,000,000, +315,576,000,000]. The {@code nanos}
   * value must be in the range [-999,999,999, +999,999,999].
   *
   * <p><b>Note:</b> Durations less than one second are represented with a 0 {@code seconds} field
   * and a positive or negative {@code nanos} field. For durations of one second or more, a non-zero
   * value for the {@code nanos} field must be of the same sign as the {@code seconds} field.
   *
   * <p>Copy of {@link com.google.protobuf.util.Duration#isValid}.</p>
   */
  private static boolean durationIsValid(long seconds, int nanos) {
    if (seconds < DURATION_SECONDS_MIN || seconds > DURATION_SECONDS_MAX) {
      return false;
    }
    if (nanos < -999999999L || nanos >= NANOS_PER_SECOND) {
      return false;
    }
    if (seconds < 0 || nanos < 0) {
      if (seconds > 0 || nanos > 0) {
        return false;
      }
    }
    return true;
  }

  /**
   * Returns the sum of {@code a} and {@code b} unless it would overflow or underflow in which case
   * {@code Long.MAX_VALUE} or {@code Long.MIN_VALUE} is returned, respectively.
   *
   * <p>Copy of {@link com.google.common.math.LongMath#saturatedAdd}.</p>
   *
   */
  @SuppressWarnings("ShortCircuitBoolean")
  private static long saturatedAdd(long a, long b) {
    long naiveSum = a + b;
    if ((a ^ b) < 0 | (a ^ naiveSum) >= 0) {
      // If a and b have different signs or a has the same sign as the result then there was no
      // overflow, return.
      return naiveSum;
    }
    // we did over/under flow, if the sign is negative we should return MAX otherwise MIN
    return Long.MAX_VALUE + ((naiveSum >>> (Long.SIZE - 1)) ^ 1);
  }
}

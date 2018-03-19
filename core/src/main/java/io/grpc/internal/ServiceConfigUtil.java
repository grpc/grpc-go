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

import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import com.google.common.base.Verify;
import io.grpc.MethodDescriptor;
import io.grpc.Status.Code;
import io.grpc.internal.RetriableStream.RetryPolicies;
import io.grpc.internal.RetriableStream.RetryPolicy;
import io.grpc.internal.RetriableStream.Throttle;
import java.io.IOException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Map.Entry;
import java.util.Random;
import java.util.Set;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Helper utility to work with service configs.
 */
final class ServiceConfigUtil {

  private static final Logger logger = Logger.getLogger(ServiceConfigUtil.class.getName());

  // From https://github.com/grpc/proposal/blob/master/A2-service-configs-in-dns.md
  static final String SERVICE_CONFIG_PREFIX = "_grpc_config=";
  private static final Set<String> SERVICE_CONFIG_CHOICE_KEYS =
      Collections.unmodifiableSet(
          new HashSet<String>(
              Arrays.asList("clientLanguage", "percentage", "clientHostname", "serviceConfig")));

  private ServiceConfigUtil() {}

  @Nullable
  static String getLoadBalancingPolicy(Map<String, Object> serviceConfig) {
    String key = "loadBalancingPolicy";
    if (serviceConfig.containsKey(key)) {
      return getString(serviceConfig, key);
    }
    return null;
  }

  /**
   * Gets retry policies from the service config.
   *
   * @throw ClassCastException if the service config doesn't parse properly
   */
  static RetryPolicies getRetryPolicies(
      @Nullable Map<String, Object> serviceConfig, int maxAttemptsLimit) {
    final Map<String, RetryPolicy> fullMethodNameMap = new HashMap<String, RetryPolicy>();
    final Map<String, RetryPolicy> serviceNameMap = new HashMap<String, RetryPolicy>();

    if (serviceConfig != null) {

      /* schema as follows
      {
        "methodConfig": [
          {
            "name": [
              {
                "service": string,
                "method": string,         // Optional
              }
            ],
            "retryPolicy": {
              "maxAttempts": number,
              "initialBackoff": string,   // Long decimal with "s" appended
              "maxBackoff": string,       // Long decimal with "s" appended
              "backoffMultiplier": number
              "retryableStatusCodes": []
            }
          }
        ]
      }
      */

      if (serviceConfig.containsKey("methodConfig")) {
        List<Object> methodConfigs = getList(serviceConfig, "methodConfig");
        for (int i = 0; i < methodConfigs.size(); i++) {
          Map<String, Object> methodConfig = getObject(methodConfigs, i);
          if (methodConfig.containsKey("retryPolicy")) {
            Map<String, Object> retryPolicy = getObject(methodConfig, "retryPolicy");

            int maxAttempts = getDouble(retryPolicy, "maxAttempts").intValue();
            maxAttempts = Math.min(maxAttempts, maxAttemptsLimit);

            String initialBackoffStr = getString(retryPolicy, "initialBackoff");
            checkState(
                initialBackoffStr.charAt(initialBackoffStr.length() - 1) == 's',
                "invalid value of initialBackoff");
            double initialBackoff =
                Double.parseDouble(initialBackoffStr.substring(0, initialBackoffStr.length() - 1));

            String maxBackoffStr = getString(retryPolicy, "maxBackoff");
            checkState(
                maxBackoffStr.charAt(maxBackoffStr.length() - 1) == 's',
                "invalid value of maxBackoff");
            double maxBackoff =
                Double.parseDouble(maxBackoffStr.substring(0, maxBackoffStr.length() - 1));

            double backoffMultiplier = getDouble(retryPolicy, "backoffMultiplier");

            List<Object> retryableStatusCodes = getList(retryPolicy, "retryableStatusCodes");
            Set<Code> codeSet = new HashSet<Code>(retryableStatusCodes.size());
            for (int j = 0; j < retryableStatusCodes.size(); j++) {
              String code = getString(retryableStatusCodes, j);
              codeSet.add(Code.valueOf(code));
            }

            RetryPolicy pojoPolicy = new RetryPolicy(
                maxAttempts, initialBackoff, maxBackoff, backoffMultiplier, codeSet);

            List<Object> names = getList(methodConfig, "name");
            for (int j = 0; j < names.size(); j++) {
              Map<String, Object> name = getObject(names, j);
              String service = getString(name, "service");
              if (name.containsKey("method")) {
                String method = getString(name, "method");
                fullMethodNameMap.put(
                    MethodDescriptor.generateFullMethodName(service, method), pojoPolicy);
              } else {
                serviceNameMap.put(service, pojoPolicy);
              }
            }
          }
        }
      }
    }

    return new RetryPolicies() {
      @Override
      public RetryPolicy get(MethodDescriptor<?, ?> method) {
        RetryPolicy retryPolicy = fullMethodNameMap.get(method.getFullMethodName());
        if (retryPolicy == null) {
          retryPolicy = serviceNameMap
              .get(MethodDescriptor.extractFullServiceName(method.getFullMethodName()));
        }
        if (retryPolicy == null) {
          retryPolicy = RetryPolicy.DEFAULT;
        }
        return retryPolicy;
      }
    };
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

  /**
   * Determines if a given Service Config choice applies, and if so, returns it.
   *
   * @see <a href="https://github.com/grpc/proposal/blob/master/A2-service-configs-in-dns.md">
   *   Service Config in DNS</a>
   * @param choice The service config choice.
   * @return The service config object or {@code null} if this choice does not apply.
   */
  @Nullable
  @SuppressWarnings("BetaApi") // Verify isn't all that beta
  static Map<String, Object> maybeChooseServiceConfig(
      Map<String, Object> choice, Random random, String hostname) {
    for (Entry<String, ?> entry : choice.entrySet()) {
      Verify.verify(SERVICE_CONFIG_CHOICE_KEYS.contains(entry.getKey()), "Bad key: %s", entry);
    }
    if (choice.containsKey("clientLanguage")) {
      List<Object> clientLanguages = getList(choice, "clientLanguage");
      if (clientLanguages.size() != 0) {
        boolean javaPresent = false;
        for (int i = 0; i < clientLanguages.size(); i++) {
          String lang = getString(clientLanguages, i).toLowerCase(Locale.ROOT);
          if ("java".equals(lang)) {
            javaPresent = true;
            break;
          }
        }
        if (!javaPresent) {
          return null;
        }
      }
    }
    if (choice.containsKey("percentage")) {
      int pct = getDouble(choice, "percentage").intValue();
      Verify.verify(pct >= 0 && pct <= 100, "Bad percentage", choice.get("percentage"));
      if (random.nextInt(100) >= pct) {
        return null;
      }
    }
    if (choice.containsKey("clientHostname")) {
      List<Object> clientHostnames = getList(choice, "clientHostname");
      if (clientHostnames.size() != 0) {
        boolean hostnamePresent = false;
        for (int i = 0; i < clientHostnames.size(); i++) {
          if (getString(clientHostnames, i).equals(hostname)) {
            hostnamePresent = true;
            break;
          }
        }
        if (!hostnamePresent) {
          return null;
        }
      }
    }
    return getObject(choice, "serviceConfig");
  }

  @SuppressWarnings("unchecked")
  static List<Map<String, Object>> parseTxtResults(List<String> txtRecords) {
    List<Map<String, Object>> serviceConfigs = new ArrayList<Map<String, Object>>();

    for (String txtRecord : txtRecords) {
      if (txtRecord.startsWith(SERVICE_CONFIG_PREFIX)) {
        List<Map<String, Object>> choices;
        try {
          Object rawChoices = JsonParser.parse(txtRecord.substring(SERVICE_CONFIG_PREFIX.length()));
          if (!(rawChoices instanceof List)) {
            throw new IOException("wrong type" + rawChoices);
          }
          List<Object> listChoices = (List<Object>) rawChoices;
          for (Object obj : listChoices) {
            if (!(obj instanceof Map)) {
              throw new IOException("wrong element type" + rawChoices);
            }
          }
          choices = (List<Map<String, Object>>) (List) listChoices;
        } catch (IOException e) {
          logger.log(Level.WARNING, "Bad service config: " + txtRecord, e);
          continue;
        }
        serviceConfigs.addAll(choices);
      } else {
        logger.log(Level.FINE, "Ignoring non service config {0}", new Object[]{txtRecord});
      }
    }
    return serviceConfigs;
  }

  /**
   * Gets a list from an object for the given key.
   */
  @SuppressWarnings("unchecked")
  private static List<Object> getList(Map<String, Object> obj, String key) {
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
  private static Map<String, Object> getObject(Map<String, Object> obj, String key) {
    assert obj.containsKey(key);
    Object value = checkNotNull(obj.get(key), "no such key %s", key);
    if (value instanceof Map) {
      return (Map<String, Object>) value;
    }
    throw new ClassCastException(
        String.format("value %s for key %s in %s is not object", value, key, obj));
  }

  /**
   * Gets an object from a list of objects for the given index.
   */
  @SuppressWarnings("unchecked")
  private static Map<String, Object> getObject(List<Object> list, int i) {
    assert i >= 0 && i < list.size();
    Object value = checkNotNull(list.get(i), "idx %s in %s is null", i, list);
    if (value instanceof Map) {
      return (Map<String, Object>) value;
    }
    throw new ClassCastException(
        String.format("value %s for idx %d in %s is not a map", value, i, list));
  }

  /**
   * Gets a double from an object for the given key.
   */
  @SuppressWarnings("unchecked")
  private static Double getDouble(Map<String, Object> obj, String key) {
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
  private static String getString(Map<String, Object> obj, String key) {
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
  private static String getString(List<Object> list, int i) {
    assert i >= 0 && i < list.size();
    Object value = checkNotNull(list.get(i), "idx %s in %s is null", i, list);
    if (value instanceof String) {
      return (String) value;
    }
    throw new ClassCastException(
        String.format("value %s for idx %d in %s is not String", value, i, list));
  }
}

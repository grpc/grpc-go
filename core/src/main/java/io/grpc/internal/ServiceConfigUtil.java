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

import com.google.common.base.Verify;
import java.io.IOException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
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

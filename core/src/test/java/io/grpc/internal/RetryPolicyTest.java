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

import static com.google.common.base.Preconditions.checkState;
import static io.grpc.internal.RetriableStream.RetryPolicy.DEFAULT;
import static java.lang.Double.parseDouble;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.google.gson.JsonPrimitive;
import io.grpc.MethodDescriptor;
import io.grpc.Status.Code;
import io.grpc.internal.ManagedChannelImpl.RetryPolicies;
import io.grpc.internal.RetriableStream.RetryPolicy;
import io.grpc.testing.TestMethodDescriptors;
import java.io.InputStreamReader;
import java.io.Reader;
import java.util.Arrays;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for RetryPolicy. */
@RunWith(JUnit4.class)
public class RetryPolicyTest {
  // TODO(zdapeng): move and refactor it to an appropriate place in src when implementing
  // ManagedChannelImpl.getRetryPolicies()
  static RetryPolicies getRetryPolicies(JsonObject serviceConfig) {
    final Map<String, RetryPolicy> fullMethodNameMap = new HashMap<String, RetryPolicy>();
    final Map<String, RetryPolicy> serviceNameMap = new HashMap<String, RetryPolicy>();

    if (serviceConfig != null) {
      JsonArray methodConfigs = serviceConfig.getAsJsonArray("methodConfig");

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

      if (methodConfigs != null) {
        for (JsonElement methodConfig : methodConfigs) {
          JsonArray names = methodConfig.getAsJsonObject().getAsJsonArray("name");
          JsonObject retryPolicy = methodConfig.getAsJsonObject().getAsJsonObject("retryPolicy");
          if (retryPolicy != null) {
            int maxAttempts = retryPolicy.getAsJsonPrimitive("maxAttempts").getAsInt();
            String initialBackoffStr =
                retryPolicy.getAsJsonPrimitive("initialBackoff").getAsString();
            checkState(
                initialBackoffStr.charAt(initialBackoffStr.length() - 1) == 's',
                "invalid value of initialBackoff");
            double initialBackoff =
                Double.parseDouble(initialBackoffStr.substring(0, initialBackoffStr.length() - 1));
            String maxBackoffStr =
                retryPolicy.getAsJsonPrimitive("maxBackoff").getAsString();
            checkState(
                maxBackoffStr.charAt(maxBackoffStr.length() - 1) == 's',
                "invalid value of maxBackoff");
            double maxBackoff =
                Double.parseDouble(maxBackoffStr.substring(0, maxBackoffStr.length() - 1));
            double backoffMultiplier =
                retryPolicy.getAsJsonPrimitive("backoffMultiplier").getAsDouble();
            JsonArray retryableStatusCodes = retryPolicy.getAsJsonArray("retryableStatusCodes");
            Set<Code> codeSet = new HashSet<Code>(retryableStatusCodes.size());
            for (JsonElement retryableStatusCode : retryableStatusCodes) {
              codeSet.add(Code.valueOf(retryableStatusCode.getAsString()));
            }
            RetryPolicy pojoPolicy = new RetryPolicy(
                maxAttempts, initialBackoff, maxBackoff, backoffMultiplier, codeSet);

            for (JsonElement name : names) {
              String service = name.getAsJsonObject().getAsJsonPrimitive("service").getAsString();
              JsonPrimitive method = name.getAsJsonObject().getAsJsonPrimitive("method");
              if (method != null && !method.getAsString().isEmpty()) {
                fullMethodNameMap.put(
                    MethodDescriptor.generateFullMethodName(service, method.getAsString()),
                    pojoPolicy);
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
          retryPolicy = DEFAULT;
        }
        return retryPolicy;
      }
    };
  }

  @Test
  public void getRetryPolicies() throws Exception {
    Reader reader = null;
    try {
      reader = new InputStreamReader(
          RetryPolicyTest.class.getResourceAsStream(
              "/io/grpc/internal/test_retry_service_config.json"),
          "UTF-8");
      JsonObject serviceConfig = new JsonParser().parse(reader).getAsJsonObject();
      RetryPolicies retryPolicies = getRetryPolicies(serviceConfig);
      assertNotNull(retryPolicies);

      MethodDescriptor.Builder<Void, Void> builder = TestMethodDescriptors.voidMethod().toBuilder();

      assertEquals(
          RetryPolicy.DEFAULT,
          retryPolicies.get(builder.setFullMethodName("not/exist").build()));
      assertEquals(
          RetryPolicy.DEFAULT,
          retryPolicies.get(builder.setFullMethodName("not_exist/Foo1").build()));

      assertEquals(
          new RetryPolicy(
              3, parseDouble("2.1"), parseDouble("2.2"), parseDouble("3"),
              Arrays.asList(Code.UNAVAILABLE, Code.RESOURCE_EXHAUSTED)),
          retryPolicies.get(builder.setFullMethodName("SimpleService1/not_exist").build()));
      assertEquals(
          new RetryPolicy(
              4, parseDouble(".1"), parseDouble("1"), parseDouble("2"),
              Arrays.asList(Code.UNAVAILABLE)),
          retryPolicies.get(builder.setFullMethodName("SimpleService1/Foo1").build()));

      assertEquals(
          RetryPolicy.DEFAULT,
          retryPolicies.get(builder.setFullMethodName("SimpleService2/not_exist").build()));
      assertEquals(
          new RetryPolicy(
              4, parseDouble(".1"), parseDouble("1"), parseDouble("2"),
              Arrays.asList(Code.UNAVAILABLE)),
          retryPolicies.get(builder.setFullMethodName("SimpleService2/Foo2").build()));
    } finally {
      if (reader != null) {
        reader.close();
      }
    }
  }
}

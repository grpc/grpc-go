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

import static java.lang.Double.parseDouble;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

import io.grpc.MethodDescriptor;
import io.grpc.Status.Code;
import io.grpc.internal.RetriableStream.RetryPolicies;
import io.grpc.internal.RetriableStream.RetryPolicy;
import io.grpc.internal.RetriableStream.Throttle;
import io.grpc.testing.TestMethodDescriptors;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.util.Arrays;
import java.util.Map;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for RetryPolicy. */
@RunWith(JUnit4.class)
public class RetryPolicyTest {

  @Test
  public void getRetryPolicies() throws Exception {
    BufferedReader reader = null;
    try {
      reader = new BufferedReader(new InputStreamReader(RetryPolicyTest.class.getResourceAsStream(
          "/io/grpc/internal/test_retry_service_config.json"), "UTF-8"));
      StringBuilder sb = new StringBuilder();
      String line;
      while ((line = reader.readLine()) != null) {
        sb.append(line).append('\n');
      }
      Object serviceConfigObj = JsonParser.parse(sb.toString());
      assertTrue(serviceConfigObj instanceof Map);

      @SuppressWarnings("unchecked")
      Map<String, Object> serviceConfig = (Map<String, Object>) serviceConfigObj;
      RetryPolicies retryPolicies = ServiceConfigUtil.getRetryPolicies(serviceConfig, 4);
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

  @Test
  public void getThrottle() throws Exception {
    BufferedReader reader = null;
    try {
      reader = new BufferedReader(new InputStreamReader(RetryPolicyTest.class.getResourceAsStream(
          "/io/grpc/internal/test_retry_service_config.json"), "UTF-8"));
      StringBuilder sb = new StringBuilder();
      String line;
      while ((line = reader.readLine()) != null) {
        sb.append(line).append('\n');
      }
      Object serviceConfigObj = JsonParser.parse(sb.toString());
      assertTrue(serviceConfigObj instanceof Map);

      @SuppressWarnings("unchecked")
      Map<String, Object> serviceConfig = (Map<String, Object>) serviceConfigObj;
      Throttle throttle = ServiceConfigUtil.getThrottlePolicy(serviceConfig);

      assertEquals(new Throttle(10f, 0.1f), throttle);
    } finally {
      if (reader != null) {
        reader.close();
      }
    }
  }
}

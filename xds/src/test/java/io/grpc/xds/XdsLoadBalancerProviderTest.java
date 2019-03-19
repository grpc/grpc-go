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

package io.grpc.xds;

import static org.junit.Assert.assertEquals;

import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.internal.JsonParser;
import io.grpc.internal.ServiceConfigUtil;
import io.grpc.internal.ServiceConfigUtil.LbConfig;
import java.util.List;
import java.util.Map;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.junit.MockitoJUnit;
import org.mockito.junit.MockitoRule;

/**
 * Tests for {@link XdsLoadBalancerProvider}.
 */
@RunWith(JUnit4.class)
public class XdsLoadBalancerProviderTest {

  @Rule
  public final MockitoRule mocks = MockitoJUnit.rule();

  @Mock
  private LoadBalancer fakeBalancer1;

  private final LoadBalancerRegistry lbRegistry = new LoadBalancerRegistry();

  private final LoadBalancerProvider lbProvider1 = new LoadBalancerProvider() {
    @Override
    public boolean isAvailable() {
      return true;
    }

    @Override
    public int getPriority() {
      return 5;
    }

    @Override
    public String getPolicyName() {
      return "supported_1";
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return fakeBalancer1;
    }
  };

  private final LoadBalancerProvider roundRobin = new LoadBalancerProvider() {
    @Override
    public boolean isAvailable() {
      return true;
    }

    @Override
    public int getPriority() {
      return 5;
    }

    @Override
    public String getPolicyName() {
      return "round_robin";
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return null;
    }
  };

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    lbRegistry.register(lbProvider1);
    lbRegistry.register(roundRobin);
  }

  @Test
  public void selectChildPolicy() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported_1\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}},"
        + "{\"supported_2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    LbConfig expectedChildPolicy =
        ServiceConfigUtil.unwrapLoadBalancingConfig(
            checkObject(JsonParser.parse("{\"supported_1\" : {\"key\" : \"val\"}}")));

    LbConfig childPolicy =
        XdsLoadBalancerProvider.selectChildPolicy(
            ServiceConfigUtil.unwrapLoadBalancingConfig(checkObject(JsonParser.parse(lbConfigRaw))),
            lbRegistry);

    assertEquals(expectedChildPolicy, childPolicy);
  }

  @Test
  public void selectFallBackPolicy() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}},"
        + "{\"supported_2\" : {\"key\" : \"val\"}}]"
        + "}}";
    LbConfig expectedFallbackPolicy = ServiceConfigUtil.unwrapLoadBalancingConfig(
        checkObject(JsonParser.parse("{\"supported_1\" : {\"key\" : \"val\"}}")));

    LbConfig fallbackPolicy = XdsLoadBalancerProvider.selectFallbackPolicy(
        ServiceConfigUtil.unwrapLoadBalancingConfig(checkObject(JsonParser.parse(lbConfigRaw))),
        lbRegistry);

    assertEquals(expectedFallbackPolicy, fallbackPolicy);
  }

  @Test
  public void selectFallBackPolicy_roundRobinIsDefault() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    LbConfig expectedFallbackPolicy = ServiceConfigUtil.unwrapLoadBalancingConfig(
        checkObject(JsonParser.parse("{\"round_robin\" : {}}")));

    LbConfig fallbackPolicy = XdsLoadBalancerProvider.selectFallbackPolicy(
        ServiceConfigUtil.unwrapLoadBalancingConfig(
            checkObject(JsonParser.parse(lbConfigRaw))),
        lbRegistry);

    assertEquals(expectedFallbackPolicy, fallbackPolicy);
  }

  @SuppressWarnings("unchecked")
  private static List<Map<String, ?>> checkObjectList(Object o) {
    return (List<Map<String, ?>>) o;
  }

  @SuppressWarnings("unchecked")
  private static Map<String, ?> checkObject(Object o) {
    return (Map<String, ?>) o;
  }
}

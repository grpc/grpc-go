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

import static com.google.common.truth.Truth.assertThat;
import static io.grpc.LoadBalancer.ATTR_LOAD_BALANCING_CONFIG;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.LoadBalancer;
import io.grpc.LoadBalancer.Helper;
import io.grpc.LoadBalancerProvider;
import io.grpc.LoadBalancerRegistry;
import io.grpc.internal.JsonParser;
import java.util.Collections;
import java.util.Map;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link XdsLoadBalancer}.
 */
@RunWith(JUnit4.class)
public class XdsLoadBalancerTest {
  @Mock
  private Helper helper;
  private XdsLoadBalancer lb;

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
      return null;
    }
  };

  private final LoadBalancerProvider lbProvider2 = new LoadBalancerProvider() {
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
      return "supported_2";
    }

    @Override
    public LoadBalancer newLoadBalancer(Helper helper) {
      return null;
    }
  };



  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    LoadBalancerRegistry.getDefaultRegistry().register(lbProvider1);
    LoadBalancerRegistry.getDefaultRegistry().register(lbProvider2);
    lb = new XdsLoadBalancer(helper);
  }

  @After
  public void tearDown() {
    LoadBalancerRegistry.getDefaultRegistry().deregister(lbProvider1);
    LoadBalancerRegistry.getDefaultRegistry().deregister(lbProvider2);
  }

  @Test
  @SuppressWarnings("unchecked")
  public void selectChildPolicy() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported_1\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}},"
        + "{\"supported_2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    Map<String, Object> expectedChildPolicy = (Map<String, Object>) JsonParser.parse(
        "{\"supported_1\" : {\"key\" : \"val\"}}");

    Map<String, Object> childPolicy = XdsLoadBalancer
        .selectChildPolicy((Map<String, Object>) JsonParser.parse(lbConfigRaw));

    assertEquals(expectedChildPolicy, childPolicy);
  }

  @Test
  @SuppressWarnings("unchecked")
  public void selectFallBackPolicy() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}},"
        + "{\"supported_2\" : {\"key\" : \"val\"}}]"
        + "}}";
    Map<String, Object> expectedFallbackPolicy = (Map<String, Object>) JsonParser.parse(
        "{\"supported_1\" : {\"key\" : \"val\"}}");

    Map<String, Object> fallbackPolicy = XdsLoadBalancer
        .selectFallbackPolicy((Map<String, Object>) JsonParser.parse(lbConfigRaw));

    assertEquals(expectedFallbackPolicy, fallbackPolicy);
  }

  @Test
  public void canHandleEmptyAddressListFromNameResolution() {
    assertTrue(lb.canHandleEmptyAddressListFromNameResolution());
  }

  @Test
  @SuppressWarnings("unchecked")
  public void resolverEvent_standardModeToStandardMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbState().childPolicy).isNull();

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbState().childPolicy).isNull();

    // TODO(zdapeng): test adsStream is unchanged.
  }

  @Test
  @SuppressWarnings("unchecked")
  public void resolverEvent_standardModeToCustomMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_1\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbState().childPolicy).isNotNull();

    // TODO(zdapeng): test adsStream is reset, channel is unchanged.
  }

  @Test
  @SuppressWarnings("unchecked")
  public void resolverEvent_customModeToStandardMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_1\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbState().childPolicy).isNotNull();

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"unsupported\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbState().childPolicy).isNull();

    // TODO(zdapeng): test adsStream is unchanged.
  }

  @Test
  @SuppressWarnings("unchecked")
  public void resolverEvent_customModeToCustomMode() throws Exception {
    String lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_1\" : {\"key\" : \"val\"}}, {\"unsupported_2\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    Map<String, Object> lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    Attributes attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbState().childPolicy).isNotNull();

    lbConfigRaw = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"supported_2\" : {\"key\" : \"val\"}}, {\"unsupported_1\" : {}}],"
        + "\"fallbackPolicy\" : [{\"unsupported\" : {}}, {\"supported_1\" : {\"key\" : \"val\"}}]"
        + "}}";
    lbConfig = (Map<String, Object>) JsonParser.parse(lbConfigRaw);
    attrs = Attributes.newBuilder().set(ATTR_LOAD_BALANCING_CONFIG, lbConfig).build();

    lb.handleResolvedAddressGroups(Collections.<EquivalentAddressGroup>emptyList(), attrs);

    assertThat(lb.getXdsLbState().childPolicy).isNotNull();

    // TODO(zdapeng): test adsStream is reset, channel is unchanged.
  }

  // TODO(zdapeng): test balancer name change
}

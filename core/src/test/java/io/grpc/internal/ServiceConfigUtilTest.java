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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;

import java.util.List;
import java.util.Map;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit test for {@link ServiceConfigUtil}.
 */
@RunWith(JUnit4.class)
public class ServiceConfigUtilTest {
  @SuppressWarnings("unchecked")
  @Test
  public void getBalancerPolicyNameFromLoadBalancingConfig() throws Exception {
    String lbConfig = "{\"lbPolicy1\" : { \"key\" : \"val\" }}";
    assertEquals(
        "lbPolicy1",
        ServiceConfigUtil.getBalancerPolicyNameFromLoadBalancingConfig(
            (Map<String, Object>) JsonParser.parse(lbConfig)));
  }

  @SuppressWarnings("unchecked")
  @Test
  public void getBalancerNameFromXdsConfig() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    assertEquals(
        "dns:///balancer.example.com:8080",
        ServiceConfigUtil.getBalancerNameFromXdsConfig(
            (Map<String, Object>) JsonParser.parse(lbConfig)));
  }

  @SuppressWarnings("unchecked")
  @Test
  public void getChildPolicyFromXdsConfig() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    Map<String, Object> expectedChildPolicy1 = (Map<String, Object>) JsonParser.parse(
        "{\"round_robin\" : {}}");
    Map<String, Object> expectedChildPolicy2 = (Map<String, Object>) JsonParser.parse(
        "{\"lbPolicy2\" : {\"key\" : \"val\"}}");

    List<Map<String, Object>> childPolicies = ServiceConfigUtil.getChildPolicyFromXdsConfig(
        (Map<String, Object>) JsonParser.parse(lbConfig));

    assertThat(childPolicies).containsExactly(expectedChildPolicy1, expectedChildPolicy2);
  }

  @SuppressWarnings("unchecked")
  @Test
  public void getChildPolicyFromXdsConfig_null() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";

    List<Map<String, Object>> childPolicies = ServiceConfigUtil.getChildPolicyFromXdsConfig(
        (Map<String, Object>) JsonParser.parse(lbConfig));

    assertThat(childPolicies).isNull();
  }

  @SuppressWarnings("unchecked")
  @Test
  public void getFallbackPolicyFromXdsConfig() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    Map<String, Object> expectedFallbackPolicy1 = (Map<String, Object>) JsonParser.parse(
        "{\"lbPolicy3\" : {\"key\" : \"val\"}}");
    Map<String, Object> expectedFallbackPolicy2 = (Map<String, Object>) JsonParser.parse(
        "{\"lbPolicy4\" : {}}");

    List<Map<String, Object>> childPolicies = ServiceConfigUtil.getFallbackPolicyFromXdsConfig(
        (Map<String, Object>) JsonParser.parse(lbConfig));

    assertThat(childPolicies).containsExactly(expectedFallbackPolicy1, expectedFallbackPolicy2);
  }

  @SuppressWarnings("unchecked")
  @Test
  public void getFallbackPolicyFromXdsConfig_null() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}]"
        + "}}";

    List<Map<String, Object>> fallbackPolicies = ServiceConfigUtil.getFallbackPolicyFromXdsConfig(
        (Map<String, Object>) JsonParser.parse(lbConfig));

    assertThat(fallbackPolicies).isNull();
  }
}

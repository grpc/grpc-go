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
import static org.junit.Assert.fail;

import io.grpc.internal.ServiceConfigUtil.LbConfig;
import java.util.List;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit test for {@link ServiceConfigUtil}.
 */
@RunWith(JUnit4.class)
public class ServiceConfigUtilTest {
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
            ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig))));
  }

  @Test
  public void getChildPolicyFromXdsConfig() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    LbConfig expectedChildPolicy1 = ServiceConfigUtil.unwrapLoadBalancingConfig(
        JsonParser.parse("{\"round_robin\" : {}}"));
    LbConfig expectedChildPolicy2 = ServiceConfigUtil.unwrapLoadBalancingConfig(
        JsonParser.parse("{\"lbPolicy2\" : {\"key\" : \"val\"}}"));

    List<LbConfig> childPolicies = ServiceConfigUtil.getChildPolicyFromXdsConfig(
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig)));

    assertThat(childPolicies).containsExactly(expectedChildPolicy1, expectedChildPolicy2);
  }

  @Test
  public void getChildPolicyFromXdsConfig_null() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";

    List<LbConfig> childPolicies = ServiceConfigUtil.getChildPolicyFromXdsConfig(
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig)));

    assertThat(childPolicies).isNull();
  }

  @Test
  public void getFallbackPolicyFromXdsConfig() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}],"
        + "\"fallbackPolicy\" : [{\"lbPolicy3\" : {\"key\" : \"val\"}}, {\"lbPolicy4\" : {}}]"
        + "}}";
    LbConfig expectedFallbackPolicy1 = ServiceConfigUtil.unwrapLoadBalancingConfig(
        JsonParser.parse("{\"lbPolicy3\" : {\"key\" : \"val\"}}"));
    LbConfig expectedFallbackPolicy2 = ServiceConfigUtil.unwrapLoadBalancingConfig(
        JsonParser.parse("{\"lbPolicy4\" : {}}"));

    List<LbConfig> childPolicies = ServiceConfigUtil.getFallbackPolicyFromXdsConfig(
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig)));

    assertThat(childPolicies).containsExactly(expectedFallbackPolicy1, expectedFallbackPolicy2);
  }

  @Test
  public void getFallbackPolicyFromXdsConfig_null() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}]"
        + "}}";

    List<LbConfig> fallbackPolicies = ServiceConfigUtil.getFallbackPolicyFromXdsConfig(
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig)));

    assertThat(fallbackPolicies).isNull();
  }

  @Test
  public void unwrapLoadBalancingConfig() throws Exception {
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}]"
        + "}}";

    LbConfig config = ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig));
    assertThat(config.getPolicyName()).isEqualTo("xds_experimental");
    assertThat(config.getRawConfigValue()).isEqualTo(JsonParser.parse(
            "{\"balancerName\" : \"dns:///balancer.example.com:8080\","
            + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}]"
            + "}"));
  }

  @Test
  public void unwrapLoadBalancingConfig_failOnTooManyFields() throws Exception {
    // A LoadBalancingConfig should not have more than one field.
    String lbConfig = "{\"xds_experimental\" : { "
        + "\"balancerName\" : \"dns:///balancer.example.com:8080\","
        + "\"childPolicy\" : [{\"round_robin\" : {}}, {\"lbPolicy2\" : {\"key\" : \"val\"}}]"
        + "},"
        + "\"grpclb\" : {} }";
    try {
      ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig));
      fail("Should throw");
    } catch (Exception e) {
      assertThat(e).hasMessageThat().contains("There are 2 fields");
    }
  }

  @Test
  public void unwrapLoadBalancingConfig_failOnEmptyObject() throws Exception {
    // A LoadBalancingConfig should not exactly one field.
    String lbConfig = "{}";
    try {
      ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig));
      fail("Should throw");
    } catch (Exception e) {
      assertThat(e).hasMessageThat().contains("There are 0 fields");
    }
  }

  @Test
  public void unwrapLoadBalancingConfig_failOnList() throws Exception {
    // A LoadBalancingConfig must be a JSON dictionary (map)
    String lbConfig = "[ { \"xds\" : {} } ]";
    try {
      ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig));
      fail("Should throw");
    } catch (Exception e) {
      assertThat(e).hasMessageThat().contains("Invalid type");
    }
  }

  @Test
  public void unwrapLoadBalancingConfig_failOnString() throws Exception {
    // A LoadBalancingConfig must be a JSON dictionary (map)
    String lbConfig = "\"xds\"";
    try {
      ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig));
      fail("Should throw");
    } catch (Exception e) {
      assertThat(e).hasMessageThat().contains("Invalid type");
    }
  }

  @Test
  public void unwrapLoadBalancingConfig_failWhenConfigIsString() throws Exception {
    // The value of the config should be a JSON dictionary (map)
    String lbConfig = "{ \"xds\" : \"I thought I was a config.\" }";
    try {
      ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(lbConfig));
      fail("Should throw");
    } catch (Exception e) {
      assertThat(e).hasMessageThat().contains("Invalid value type");
    }
  }

  @Test
  public void unwrapLoadBalancingConfigList() throws Exception {
    String lbConfig = "[ "
        + "{\"xds_experimental\" : {\"balancerName\" : \"dns:///balancer.example.com:8080\"} },"
        + "{\"grpclb\" : {} } ]";
    List<LbConfig> configs =
        ServiceConfigUtil.unwrapLoadBalancingConfigList(JsonParser.parse(lbConfig));
    assertThat(configs).containsExactly(
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(
                "{\"xds_experimental\" : "
                + "{\"balancerName\" : \"dns:///balancer.example.com:8080\"} }")),
        ServiceConfigUtil.unwrapLoadBalancingConfig(JsonParser.parse(
                "{\"grpclb\" : {} }"))).inOrder();
  }

  @Test
  public void unwrapLoadBalancingConfigList_failOnObject() throws Exception {
    String notAList = "{}";
    try {
      ServiceConfigUtil.unwrapLoadBalancingConfigList(JsonParser.parse(notAList));
      fail("Should throw");
    } catch (Exception e) {
      assertThat(e).hasMessageThat().contains("List expected");
    }
  }

  @Test
  public void unwrapLoadBalancingConfigList_failOnMalformedConfig() throws Exception {
    String lbConfig = "[ "
        + "{\"xds_experimental\" : \"I thought I was a config\" },"
        + "{\"grpclb\" : {} } ]";
    try {
      ServiceConfigUtil.unwrapLoadBalancingConfigList(JsonParser.parse(lbConfig));
      fail("Should throw");
    } catch (Exception e) {
      assertThat(e).hasMessageThat().contains("Invalid value type");
    }
  }
}

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

import static org.junit.Assert.assertEquals;

import com.google.gson.Gson;
import io.grpc.internal.ServiceConfiguration.MethodConfig;
import io.grpc.internal.ServiceConfiguration.Name;
import io.grpc.internal.ServiceConfiguration.ServiceConfig;
import java.util.Arrays;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link ServiceConfiguration}.
 */
@RunWith(JUnit4.class)
public class ServiceConfigurationTest {

  @Test
  public void parses() {
    String json = "{\n"
        + "    \"loadBalancingPolicy\": \"round_robin\", \n"
        + "    \"methodConfig\": [\n"
        + "        {\n"
        + "            \"name\": [\n"
        + "                {\n"
        + "                    \"method\": \"Foo\", \n"
        + "                    \"service\": \"SimpleService\"\n"
        + "                }\n"
        + "            ], \n"
        + "            \"waitForReady\": true\n"
        + "        }, \n"
        + "        {\n"
        + "            \"name\": [\n"
        + "                {\n"
        + "                    \"method\": \"FooTwo\", \n"
        + "                    \"service\": \"SimpleService\"\n"
        + "                }\n"
        + "            ] \n"
        + "        }, \n"
        + "        {\n"
        + "            \"name\": [\n"
        + "                {\n"
        + "                    \"method\": \"FooTwelve\", \n"
        + "                    \"service\": \"SimpleService\"\n"
        + "                }\n"
        + "            ], \n"
        + "            \"waitForReady\": false\n"
        + "        }\n"
        + "    ]\n"
        + "}";
    ServiceConfig actual = new Gson().fromJson(json, ServiceConfig.class);

    ServiceConfig expected =
        new ServiceConfig(
            "round_robin",
            Arrays.asList(
                new MethodConfig(
                    Arrays.asList(new Name("SimpleService", "Foo")),
                    /*waitForReady=*/ true,
                    /*timeout=*/ null,
                    /*maxRequestMessageBytes=*/ null,
                    /*maxResponseMessageBytes=*/ null,
                    /*retryPolicy=*/ null,
                    /*hedgingPolicy=*/ null),
                new MethodConfig(
                    Arrays.asList(new Name("SimpleService", "FooTwo")),
                    /*waitForReady=*/ null,
                    /*timeout=*/ null,
                    /*maxRequestMessageBytes=*/ null,
                    /*maxResponseMessageBytes=*/ null,
                    /*retryPolicy=*/ null,
                    /*hedgingPolicy=*/ null),
                new MethodConfig(
                    Arrays.asList(new Name("SimpleService", "FooTwelve")),
                    /*waitForReady=*/ false,
                    /*timeout=*/ null,
                    /*maxRequestMessageBytes=*/ null,
                    /*maxResponseMessageBytes=*/ null,
                    /*retryPolicy=*/ null,
                    /*hedgingPolicy=*/ null)),
            /*retryThrottlingPolicy=*/null);

    assertEquals(actual, expected);
  }
}

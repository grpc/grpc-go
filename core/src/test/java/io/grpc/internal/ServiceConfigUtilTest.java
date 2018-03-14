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

import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Random;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link ServiceConfigUtil}.
 */
@RunWith(JUnit4.class)
public class ServiceConfigUtilTest {

  @Rule
  public final ExpectedException thrown = ExpectedException.none();

  private final Map<String, Object> serviceConfig = new LinkedHashMap<String, Object>();

  @Test
  public void maybeChooseServiceConfig_failsOnMisspelling() {
    Map<String, Object> bad = new LinkedHashMap<String, Object>();
    bad.put("parcentage", 1.0);
    thrown.expectMessage("Bad key");

    ServiceConfigUtil.maybeChooseServiceConfig(bad, new Random(), "host");
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageMatchesJava() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<Object>();
    langs.add("java");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageDoesntMatchGo() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<Object>();
    langs.add("go");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageCaseInsensitive() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<Object>();
    langs.add("JAVA");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageMatchesEmtpy() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<Object>();
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageMatchesMulti() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> langs = new ArrayList<Object>();
    langs.add("go");
    langs.add("java");
    choice.put("clientLanguage", langs);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageZeroAlwaysFails() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 0D);
    choice.put("serviceConfig", serviceConfig);

    assertNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageHundredAlwaysSucceeds() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 100D);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAboveMatches50() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 50D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 49;
      }
    };

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAtFails50() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 50D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 50;
      }
    };

    assertNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAboveMatches99() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 99D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 98;
      }
    };

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAtFails99() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 99D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 99;
      }
    };

    assertNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAboveMatches1() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 1D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 0;
      }
    };

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_percentageAtFails1() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    choice.put("percentage", 1D);
    choice.put("serviceConfig", serviceConfig);

    Random r = new Random() {
      @Override
      public int nextInt(int bound) {
        return 1;
      }
    };

    assertNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, r, "host"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameMatches() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<Object>();
    hosts.add("localhost");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameDoesntMatch() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<Object>();
    hosts.add("localhorse");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }

  @Test
  public void maybeChooseServiceConfig_clientLanguageCaseSensitive() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<Object>();
    hosts.add("LOCALHOST");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameMatchesEmtpy() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<Object>();
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "host"));
  }

  @Test
  public void maybeChooseServiceConfig_hostnameMatchesMulti() {
    Map<String, Object> choice = new LinkedHashMap<String, Object>();
    List<Object> hosts = new ArrayList<Object>();
    hosts.add("localhorse");
    hosts.add("localhost");
    choice.put("clientHostname", hosts);
    choice.put("serviceConfig", serviceConfig);

    assertNotNull(ServiceConfigUtil.maybeChooseServiceConfig(choice, new Random(), "localhost"));
  }
}

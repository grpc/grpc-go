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

package io.grpc.services;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import io.grpc.services.BinaryLog.FactoryImpl;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Tests for {@link BinaryLog}. */
@RunWith(JUnit4.class)
public final class BinaryLogTest {
  private static final BinaryLog HEADER_FULL = new Builder().header(Integer.MAX_VALUE).build();
  private static final BinaryLog HEADER_256 = new Builder().header(256).build();
  private static final BinaryLog MSG_FULL = new Builder().msg(Integer.MAX_VALUE).build();
  private static final BinaryLog MSG_256 = new Builder().msg(256).build();
  private static final BinaryLog BOTH_256 = new Builder().header(256).msg(256).build();
  private static final BinaryLog BOTH_FULL =
      new Builder().header(Integer.MAX_VALUE).msg(Integer.MAX_VALUE).build();
  

  @Test
  public void configBinLog_global() throws Exception {
    assertEquals(BOTH_FULL, new FactoryImpl("*").getLog("p.s/m"));
    assertEquals(BOTH_FULL, new FactoryImpl("*{h;m}").getLog("p.s/m"));
    assertEquals(HEADER_FULL, new FactoryImpl("*{h}").getLog("p.s/m"));
    assertEquals(MSG_FULL, new FactoryImpl("*{m}").getLog("p.s/m"));
    assertEquals(HEADER_256, new FactoryImpl("*{h:256}").getLog("p.s/m"));
    assertEquals(MSG_256, new FactoryImpl("*{m:256}").getLog("p.s/m"));
    assertEquals(BOTH_256, new FactoryImpl("*{h:256;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        new FactoryImpl("*{h;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        new FactoryImpl("*{h:256;m}").getLog("p.s/m"));
  }

  @Test
  public void configBinLog_method() throws Exception {
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/m").getLog("p.s/m"));
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/m{h;m}").getLog("p.s/m"));
    assertEquals(HEADER_FULL, new FactoryImpl("p.s/m{h}").getLog("p.s/m"));
    assertEquals(MSG_FULL, new FactoryImpl("p.s/m{m}").getLog("p.s/m"));
    assertEquals(HEADER_256, new FactoryImpl("p.s/m{h:256}").getLog("p.s/m"));
    assertEquals(MSG_256, new FactoryImpl("p.s/m{m:256}").getLog("p.s/m"));
    assertEquals(BOTH_256, new FactoryImpl("p.s/m{h:256;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        new FactoryImpl("p.s/m{h;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        new FactoryImpl("p.s/m{h:256;m}").getLog("p.s/m"));
  }

  @Test
  public void configBinLog_method_absent() throws Exception {
    assertNull(new FactoryImpl("p.s/m").getLog("p.s/absent"));
  }

  @Test
  public void configBinLog_service() throws Exception {
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/*").getLog("p.s/m"));
    assertEquals(BOTH_FULL, new FactoryImpl("p.s/*{h;m}").getLog("p.s/m"));
    assertEquals(HEADER_FULL, new FactoryImpl("p.s/*{h}").getLog("p.s/m"));
    assertEquals(MSG_FULL, new FactoryImpl("p.s/*{m}").getLog("p.s/m"));
    assertEquals(HEADER_256, new FactoryImpl("p.s/*{h:256}").getLog("p.s/m"));
    assertEquals(MSG_256, new FactoryImpl("p.s/*{m:256}").getLog("p.s/m"));
    assertEquals(BOTH_256, new FactoryImpl("p.s/*{h:256;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        new FactoryImpl("p.s/*{h;m:256}").getLog("p.s/m"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        new FactoryImpl("p.s/*{h:256;m}").getLog("p.s/m"));
  }

  @Test
  public void configBinLog_service_absent() throws Exception {
    assertNull(new FactoryImpl("p.s/*").getLog("p.other/m"));
  }

  @Test
  public void createLogFromOptionString() throws Exception {
    assertEquals(BOTH_FULL, FactoryImpl.createBinaryLog(/*logConfig=*/ null));
    assertEquals(HEADER_FULL, FactoryImpl.createBinaryLog("{h}"));
    assertEquals(MSG_FULL, FactoryImpl.createBinaryLog("{m}"));
    assertEquals(HEADER_256, FactoryImpl.createBinaryLog("{h:256}"));
    assertEquals(MSG_256, FactoryImpl.createBinaryLog("{m:256}"));
    assertEquals(BOTH_256, FactoryImpl.createBinaryLog("{h:256;m:256}"));
    assertEquals(
        new Builder().header(Integer.MAX_VALUE).msg(256).build(),
        FactoryImpl.createBinaryLog("{h;m:256}"));
    assertEquals(
        new Builder().header(256).msg(Integer.MAX_VALUE).build(),
        FactoryImpl.createBinaryLog("{h:256;m}"));
  }

  @Test
  public void createLogFromOptionString_malformed() throws Exception {
    assertNull(FactoryImpl.createBinaryLog("bad"));
    assertNull(FactoryImpl.createBinaryLog("{bad}"));
    assertNull(FactoryImpl.createBinaryLog("{x;y}"));
    assertNull(FactoryImpl.createBinaryLog("{h:abc}"));
    assertNull(FactoryImpl.createBinaryLog("{2}"));
    assertNull(FactoryImpl.createBinaryLog("{2;2}"));
    // The grammar specifies that if both h and m are present, h comes before m
    assertNull(FactoryImpl.createBinaryLog("{m:123;h:123}"));
    // NumberFormatException
    assertNull(FactoryImpl.createBinaryLog("{h:99999999999999}"));
  }

  @Test
  public void configBinLog_multiConfig_withGlobal() throws Exception {
    FactoryImpl factory = new FactoryImpl(
        "*{h},"
        + "package.both256/*{h:256;m:256},"
        + "package.service1/both128{h:128;m:128},"
        + "package.service2/method_messageOnly{m}");
    assertEquals(HEADER_FULL, factory.getLog("otherpackage.service/method"));

    assertEquals(BOTH_256, factory.getLog("package.both256/method1"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method2"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method3"));

    assertEquals(
        new Builder().header(128).msg(128).build(), factory.getLog("package.service1/both128"));
    // the global config is in effect
    assertEquals(HEADER_FULL, factory.getLog("package.service1/absent"));

    assertEquals(MSG_FULL, factory.getLog("package.service2/method_messageOnly"));
    // the global config is in effect
    assertEquals(HEADER_FULL, factory.getLog("package.service2/absent"));
  }

  @Test
  public void configBinLog_multiConfig_noGlobal() throws Exception {
    FactoryImpl factory = new FactoryImpl(
        "package.both256/*{h:256;m:256},"
        + "package.service1/both128{h:128;m:128},"
        + "package.service2/method_messageOnly{m}");
    assertNull(factory.getLog("otherpackage.service/method"));

    assertEquals(BOTH_256, factory.getLog("package.both256/method1"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method2"));
    assertEquals(BOTH_256, factory.getLog("package.both256/method3"));

    assertEquals(
        new Builder().header(128).msg(128).build(), factory.getLog("package.service1/both128"));
    // no global config in effect
    assertNull(factory.getLog("package.service1/absent"));

    assertEquals(MSG_FULL, factory.getLog("package.service2/method_messageOnly"));
    // no global config in effect
    assertNull(factory.getLog("package.service2/absent"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_global() throws Exception {
    FactoryImpl factory = new FactoryImpl("*{h},p.s/m,*{h:256}");
    // The duplicate
    assertEquals(HEADER_FULL, factory.getLog("p.other1/m"));
    assertEquals(HEADER_FULL, factory.getLog("p.other2/m"));
    // Other
    assertEquals(BOTH_FULL, factory.getLog("p.s/m"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_service() throws Exception {
    FactoryImpl factory = new FactoryImpl("p.s/*,*{h:256},p.s/*{h}");
    // The duplicate
    assertEquals(BOTH_FULL, factory.getLog("p.s/m1"));
    assertEquals(BOTH_FULL, factory.getLog("p.s/m2"));
    // Other
    assertEquals(HEADER_256, factory.getLog("p.other1/m"));
    assertEquals(HEADER_256, factory.getLog("p.other2/m"));
  }

  @Test
  public void configBinLog_ignoreDuplicates_method() throws Exception {
    FactoryImpl factory = new FactoryImpl("p.s/m,*{h:256},p.s/m{h}");
    // The duplicate
    assertEquals(BOTH_FULL, factory.getLog("p.s/m"));
    // Other
    assertEquals(HEADER_256, factory.getLog("p.other1/m"));
    assertEquals(HEADER_256, factory.getLog("p.other2/m"));
  }

  /** A builder class to make unit test code more readable. */
  private static final class Builder {
    int maxHeaderBytes = 0;
    int maxMessageBytes = 0;

    Builder header(int bytes) {
      maxHeaderBytes = bytes;
      return this;
    }

    Builder msg(int bytes) {
      maxMessageBytes = bytes;
      return this;
    }

    BinaryLog build() {
      return new BinaryLog(maxHeaderBytes, maxMessageBytes);
    }
  }
}

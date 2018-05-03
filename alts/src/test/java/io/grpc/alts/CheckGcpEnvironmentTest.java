/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.alts;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import java.io.BufferedReader;
import java.io.StringReader;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class CheckGcpEnvironmentTest {

  @Test
  public void checkGcpLinuxPlatformData() throws Exception {
    BufferedReader reader;
    reader = new BufferedReader(new StringReader("HP Z440 Workstation"));
    assertFalse(CheckGcpEnvironment.checkProductNameOnLinux(reader));
    reader = new BufferedReader(new StringReader("Google"));
    assertTrue(CheckGcpEnvironment.checkProductNameOnLinux(reader));
    reader = new BufferedReader(new StringReader("Google Compute Engine"));
    assertTrue(CheckGcpEnvironment.checkProductNameOnLinux(reader));
    reader = new BufferedReader(new StringReader("Google Compute Engine    "));
    assertTrue(CheckGcpEnvironment.checkProductNameOnLinux(reader));
  }

  @Test
  public void checkGcpWindowsPlatformData() throws Exception {
    BufferedReader reader;
    reader = new BufferedReader(new StringReader("Product : Google"));
    assertFalse(CheckGcpEnvironment.checkBiosDataOnWindows(reader));
    reader = new BufferedReader(new StringReader("Manufacturer : LENOVO"));
    assertFalse(CheckGcpEnvironment.checkBiosDataOnWindows(reader));
    reader = new BufferedReader(new StringReader("Manufacturer : Google Compute Engine"));
    assertFalse(CheckGcpEnvironment.checkBiosDataOnWindows(reader));
    reader = new BufferedReader(new StringReader("Manufacturer : Google"));
    assertTrue(CheckGcpEnvironment.checkBiosDataOnWindows(reader));
    reader = new BufferedReader(new StringReader("Manufacturer:Google"));
    assertTrue(CheckGcpEnvironment.checkBiosDataOnWindows(reader));
    reader = new BufferedReader(new StringReader("Manufacturer :   Google    "));
    assertTrue(CheckGcpEnvironment.checkBiosDataOnWindows(reader));
    reader = new BufferedReader(new StringReader("BIOSVersion : 1.0\nManufacturer : Google\n"));
    assertTrue(CheckGcpEnvironment.checkBiosDataOnWindows(reader));
  }
}

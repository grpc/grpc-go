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

package io.grpc.services;

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.primitives.Ints;
import java.net.Inet4Address;
import java.net.Inet6Address;
import java.net.InetAddress;
import java.util.Arrays;

// This is copied from guava 20.0 because it is a @Beta api
final class InetAddressUtil {
  private static final int IPV6_PART_COUNT = 8;

  public static String toAddrString(InetAddress ip) {
    checkNotNull(ip);
    if (ip instanceof Inet4Address) {
      // For IPv4, Java's formatting is good enough.
      return ip.getHostAddress();
    }
    checkArgument(ip instanceof Inet6Address);
    byte[] bytes = ip.getAddress();
    int[] hextets = new int[IPV6_PART_COUNT];
    for (int i = 0; i < hextets.length; i++) {
      hextets[i] = Ints.fromBytes((byte) 0, (byte) 0, bytes[2 * i], bytes[2 * i + 1]);
    }
    compressLongestRunOfZeroes(hextets);
    return hextetsToIPv6String(hextets);
  }

  private static void compressLongestRunOfZeroes(int[] hextets) {
    int bestRunStart = -1;
    int bestRunLength = -1;
    int runStart = -1;
    for (int i = 0; i < hextets.length + 1; i++) {
      if (i < hextets.length && hextets[i] == 0) {
        if (runStart < 0) {
          runStart = i;
        }
      } else if (runStart >= 0) {
        int runLength = i - runStart;
        if (runLength > bestRunLength) {
          bestRunStart = runStart;
          bestRunLength = runLength;
        }
        runStart = -1;
      }
    }
    if (bestRunLength >= 2) {
      Arrays.fill(hextets, bestRunStart, bestRunStart + bestRunLength, -1);
    }
  }

  private static String hextetsToIPv6String(int[] hextets) {
    // While scanning the array, handle these state transitions:
    //   start->num => "num"     start->gap => "::"
    //   num->num   => ":num"    num->gap   => "::"
    //   gap->num   => "num"     gap->gap   => ""
    StringBuilder buf = new StringBuilder(39);
    boolean lastWasNumber = false;
    for (int i = 0; i < hextets.length; i++) {
      boolean thisIsNumber = hextets[i] >= 0;
      if (thisIsNumber) {
        if (lastWasNumber) {
          buf.append(':');
        }
        buf.append(Integer.toHexString(hextets[i]));
      } else {
        if (i == 0 || lastWasNumber) {
          buf.append("::");
        }
      }
      lastWasNumber = thisIsNumber;
    }
    return buf.toString();
  }
}
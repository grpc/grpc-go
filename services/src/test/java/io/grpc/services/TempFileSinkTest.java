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

package io.grpc.services;

import static org.junit.Assert.assertEquals;

import io.grpc.binarylog.v1.GrpcLogEntry;
import java.io.DataInputStream;
import java.io.FileInputStream;
import java.io.IOException;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link io.grpc.services.TempFileSink}.
 */
@RunWith(JUnit4.class)
public class TempFileSinkTest {
  @Test
  public void readMyWrite() throws Exception {
    TempFileSink sink = new TempFileSink();
    GrpcLogEntry e1 = GrpcLogEntry.newBuilder()
        .setCallId(1234)
        .build();
    GrpcLogEntry e2 = GrpcLogEntry.newBuilder()
        .setCallId(5678)
        .build();
    sink.write(e1);
    sink.write(e2);
    sink.close();

    DataInputStream input = new DataInputStream(new FileInputStream(sink.getPath()));
    try {
      GrpcLogEntry read1 = GrpcLogEntry.parseDelimitedFrom(input);
      GrpcLogEntry read2 = GrpcLogEntry.parseDelimitedFrom(input);

      assertEquals(e1, read1);
      assertEquals(e2, read2);
      assertEquals(-1, input.read());
    } finally {
      input.close();
    }
  }

  @Test
  public void writeAfterCloseIsSilent() throws IOException {
    TempFileSink sink = new TempFileSink();
    sink.close();
    sink.write(GrpcLogEntry.newBuilder()
        .setCallId(1234)
        .build());
  }
}

/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.protobuf;

import static org.junit.Assert.assertEquals;

import com.google.common.io.ByteStreams;
import com.google.protobuf.MessageLite;
import com.google.protobuf.Type;

import io.grpc.MethodDescriptor.Marshaller;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.io.ByteArrayInputStream;
import java.io.InputStream;

/** Unit tests for {@link ProtoUtils}. */
@RunWith(JUnit4.class)
public class ProtoUtilsTest {
  private Type proto = Type.newBuilder().setName("name").build();

  @Test
  public void testRoundtrip() throws Exception {
    Marshaller<Type> marshaller = ProtoUtils.marshaller(Type.getDefaultInstance());
    InputStream is = marshaller.stream(proto);
    is = new ByteArrayInputStream(ByteStreams.toByteArray(is));
    assertEquals(proto, marshaller.parse(is));
  }

  @Test
  @Deprecated
  public void testRoundtripLite() throws Exception {
    Marshaller<MessageLite> marshaller
        = ProtoUtils.marshaller((MessageLite) Type.getDefaultInstance());
    InputStream is = marshaller.stream(proto);
    is = new ByteArrayInputStream(ByteStreams.toByteArray(is));
    assertEquals(proto, marshaller.parse(is));
  }

  @Test
  public void keyForProto() {
    assertEquals("google.protobuf.Type-bin",
        ProtoUtils.keyForProto(Type.getDefaultInstance()).originalName());
  }
}

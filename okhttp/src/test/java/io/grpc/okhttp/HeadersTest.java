/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.okhttp;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;

import io.grpc.Metadata;
import io.grpc.internal.GrpcUtil;
import io.grpc.okhttp.internal.framed.Header;
import java.util.List;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class HeadersTest {
  @Test
  public void createRequestHeaders_sanitizes() {
    Metadata metaData = new Metadata();

    // Intentionally being explicit here rather than relying on any pre-defined lists of headers,
    // since the goal of this test is to validate the correctness of such lists in the first place.
    metaData.put(GrpcUtil.CONTENT_TYPE_KEY, "to-be-removed");
    metaData.put(GrpcUtil.USER_AGENT_KEY, "to-be-removed");
    metaData.put(GrpcUtil.TE_HEADER, "to-be-removed");


    Metadata.Key<String> userKey = Metadata.Key.of("user-key", Metadata.ASCII_STRING_MARSHALLER);
    String userValue = "user-value";
    metaData.put(userKey, userValue);

    String path = "//testServerice/test";
    String authority = "localhost";
    String userAgent = "useragent";

    List<Header> headers = Headers.createRequestHeaders(
        metaData,
        path,
        authority,
        userAgent,
        false);

    // 7 reserved headers, 1 user header
    assertEquals(7 + 1, headers.size());
    // Check the 3 reserved headers that are non pseudo
    // Users can not create pseudo headers keys so no need to check for them here
    assertThat(headers).contains(Headers.CONTENT_TYPE_HEADER);
    assertThat(headers).contains(new Header(GrpcUtil.USER_AGENT_KEY.name(), userAgent));
    assertThat(headers).contains(new Header(GrpcUtil.TE_HEADER.name(), GrpcUtil.TE_TRAILERS));
    // Check the user header is in tact
    assertThat(headers).contains(new Header(userKey.name(), userValue));
  }
}

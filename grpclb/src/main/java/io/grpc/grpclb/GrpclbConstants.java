/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.grpclb;

import io.grpc.Attributes;
import io.grpc.ExperimentalApi;
import io.grpc.Metadata;

/**
 * Constants for the GRPCLB load-balancer.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1782")
public final class GrpclbConstants {
  /**
   * The load-balancing policy designated by the naming system.
   */
  public enum LbPolicy {
    PICK_FIRST,
    ROUND_ROBIN,
    GRPCLB
  }

  /**
   * An attribute of a name resolution result, designating the LB policy.
   */
  public static final Attributes.Key<LbPolicy> ATTR_LB_POLICY =
      Attributes.Key.of("io.grpc.grpclb.lbPolicy");

  /**
   * The naming authority of an LB server address.  It is an address-group-level attribute, present
   * when the address group is a LoadBalancer.
   */
  public static final Attributes.Key<String> ATTR_LB_ADDR_AUTHORITY =
      Attributes.Key.of("io.grpc.grpclb.lbAddrAuthority");

  /**
   * The opaque token given by the remote balancer for each returned server address.  The client
   * will send this token with any requests sent to the associated server.
   */
  public static final Metadata.Key<String> TOKEN_METADATA_KEY =
      Metadata.Key.of("lb-token", Metadata.ASCII_STRING_MARSHALLER);

  private GrpclbConstants() { }
}

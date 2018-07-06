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

package io.grpc.internal;

import io.grpc.Attributes;
import java.util.Map;

/**
 * Special attributes that are only useful to gRPC.
 */
public final class GrpcAttributes {
  /**
   * Attribute key for service config.
   */
  public static final Attributes.Key<Map<String, Object>> NAME_RESOLVER_SERVICE_CONFIG =
      Attributes.Key.create("service-config");

  /**
   * The naming authority of a gRPC LB server address.  It is an address-group-level attribute,
   * present when the address group is a LoadBalancer.
   */
  public static final Attributes.Key<String> ATTR_LB_ADDR_AUTHORITY =
      Attributes.Key.create("io.grpc.grpclb.lbAddrAuthority");

  /**
   * Whether this EquivalentAddressGroup was provided by a GRPCLB server. It would be rare for this
   * value to be {@code false}; generally it would be better to not have the key present at all.
   */
  public static final Attributes.Key<Boolean> ATTR_LB_PROVIDED_BACKEND =
      Attributes.Key.create("io.grpc.grpclb.lbProvidedBackend");

  private GrpcAttributes() {}
}

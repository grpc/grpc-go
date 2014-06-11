package com.google.net.stubby.stub;

import com.google.common.collect.ImmutableList;
import com.google.net.stubby.MethodDescriptor;

import java.util.Map;

/**
 * Base class for all stub configurations.
 */
public abstract class AbstractServiceDescriptor<T extends AbstractServiceDescriptor> {

  /**
   * Returns the list of operations defined in the stub configuration.
   */
  public abstract ImmutableList<MethodDescriptor> methods();

  /**
   * Returns a new stub configuration for the provided method configurations.
   */
  protected abstract T build(Map<String, MethodDescriptor> methodMap);
}

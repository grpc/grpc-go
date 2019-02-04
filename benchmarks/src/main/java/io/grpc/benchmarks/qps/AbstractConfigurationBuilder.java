/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc.benchmarks.qps;

import static java.lang.Math.max;
import static java.lang.String.CASE_INSENSITIVE_ORDER;

import com.google.common.base.Strings;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;

/**
 * Abstract base class for all {@link Configuration.Builder}s.
 */
public abstract class AbstractConfigurationBuilder<T extends Configuration>
    implements Configuration.Builder<T> {

  private static final Param HELP = new Param() {
    @Override
    public String getName() {
      return "help";
    }

    @Override
    public String getType() {
      return "";
    }

    @Override
    public String getDescription() {
      return "Print this text.";
    }

    @Override
    public boolean isRequired() {
      return false;
    }

    @Override
    public String getDefaultValue() {
      return null;
    }

    @Override
    public void setValue(Configuration config, String value) {
      throw new UnsupportedOperationException();
    }
  };

  /**
   * A single application parameter supported by this builder.
   */
  protected interface Param {
    /**
     * The name of the parameter as it would appear on the command-line.
     */
    String getName();

    /**
     * A string representation of the parameter type. If not applicable, just returns an empty
     * string.
     */
    String getType();

    /**
     * A description of this parameter used when printing usage.
     */
    String getDescription();

    /**
     * The default value used when not set explicitly. Ignored if {@link #isRequired()} is {@code
     * true}.
     */
    String getDefaultValue();

    /**
     * Indicates whether or not this parameter is required and must therefore be set before the
     * configuration can be successfully built.
     */
    boolean isRequired();

    /**
     * Sets this parameter on the given configuration instance.
     */
    void setValue(Configuration config, String value);
  }

  @Override
  public final T build(String[] args) {
    T config = newConfiguration();
    Map<String, Param> paramMap = getParamMap();
    Set<String> appliedParams = new TreeSet<>(CASE_INSENSITIVE_ORDER);

    for (String arg : args) {
      if (!arg.startsWith("--")) {
        throw new IllegalArgumentException("All arguments must start with '--': " + arg);
      }
      String[] pair = arg.substring(2).split("=", 2);
      String key = pair[0];
      String value = "";
      if (pair.length == 2) {
        value = pair[1];
      }

      // If help was requested, just throw now to print out the usage.
      if (HELP.getName().equalsIgnoreCase(key)) {
        throw new IllegalArgumentException("Help requested");
      }

      Param param = paramMap.get(key);
      if (param == null) {
        throw new IllegalArgumentException("Unsupported argument: " + key);
      }
      param.setValue(config, value);
      appliedParams.add(key);
    }

    // Ensure that all required options have been provided.
    for (Param param : getParams()) {
      if (param.isRequired() && !appliedParams.contains(param.getName())) {
        throw new IllegalArgumentException("Missing required option '--"
            + param.getName() + "'.");
      }
    }

    return build0(config);
  }

  @Override
  public final void printUsage() {
    System.out.println("Usage: [ARGS...]");
    int column1Width = 0;
    List<Param> params = new ArrayList<>();
    params.add(HELP);
    params.addAll(getParams());

    for (Param param : params) {
      column1Width = max(commandLineFlag(param).length(), column1Width);
    }
    int column1Start = 2;
    int column2Start = column1Start + column1Width + 2;
    for (Param param : params) {
      StringBuilder sb = new StringBuilder();
      sb.append(Strings.repeat(" ", column1Start));
      sb.append(commandLineFlag(param));
      sb.append(Strings.repeat(" ", column2Start - sb.length()));
      String message = param.getDescription();
      sb.append(wordWrap(message, column2Start, 80));
      if (param.isRequired()) {
        sb.append(Strings.repeat(" ", column2Start));
        sb.append("[Required]\n");
      } else if (param.getDefaultValue() != null && !param.getDefaultValue().isEmpty()) {
        sb.append(Strings.repeat(" ", column2Start));
        sb.append("[Default=" + param.getDefaultValue() + "]\n");
      }
      System.out.println(sb);
    }
    System.out.println();
  }

  /**
   * Creates a new configuration instance which will be used as the target for command-line
   * arguments.
   */
  protected abstract T newConfiguration();

  /**
   * Returns the valid parameters supported by the configuration.
   */
  protected abstract Collection<Param> getParams();

  /**
   * Called by {@link #build(String[])} after verifying that all required options have been set.
   * Performs any final validation and modifications to the configuration. If successful, returns
   * the fully built configuration.
   */
  protected abstract T build0(T config);

  private Map<String, Param> getParamMap() {
    Map<String, Param> map = new TreeMap<>(CASE_INSENSITIVE_ORDER);
    for (Param param : getParams()) {
      map.put(param.getName(), param);
    }
    return map;
  }

  private static String commandLineFlag(Param param) {
    String name = param.getName().toLowerCase(Locale.ROOT);
    String type = (!param.getType().isEmpty() ? '=' + param.getType() : "");
    return "--" + name + type;
  }

  private static String wordWrap(String text, int startPos, int maxPos) {
    StringBuilder builder = new StringBuilder();
    int pos = startPos;
    String[] parts = text.split("\\n", -1);
    boolean isBulleted = parts.length > 1;
    for (String part : parts) {
      int lineStart = startPos;
      while (!part.isEmpty()) {
        if (pos < lineStart) {
          builder.append(Strings.repeat(" ", lineStart - pos));
          pos = lineStart;
        }
        int maxLength = maxPos - pos;
        int length = part.length();
        if (length > maxLength) {
          length = part.lastIndexOf(' ', maxPos - pos) + 1;
          if (length == 0) {
            length = part.length();
          }
        }
        builder.append(part.substring(0, length));
        part = part.substring(length);

        // Wrap to the next line.
        builder.append("\n");
        pos = 0;
        lineStart = isBulleted ? startPos + 2 : startPos;
      }
    }
    return builder.toString();
  }
}

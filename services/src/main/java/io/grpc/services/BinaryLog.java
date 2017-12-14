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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import io.grpc.MethodDescriptor;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import javax.annotation.Nullable;

/**
 * A binary log class that is configured for a specific {@link MethodDescriptor}.
 */
final class BinaryLog {
  private static final Logger logger = Logger.getLogger(BinaryLog.class.getName());
  private static final BinaryLog NOOP_LOG =
      new BinaryLog(/*maxHeaderBytes=*/ 0, /*maxMessageBytes=*/ 0);
  private final int maxHeaderBytes;
  private final int maxMessageBytes;

  @VisibleForTesting
  BinaryLog(int maxHeaderBytes, int maxMessageBytes) {
    this.maxHeaderBytes = maxHeaderBytes;
    this.maxMessageBytes = maxMessageBytes;
  }

  @Override
  public boolean equals(Object o) {
    if (!(o instanceof BinaryLog)) {
      return false;
    }
    BinaryLog that = (BinaryLog) o;
    return this.maxHeaderBytes == that.maxHeaderBytes
        && this.maxMessageBytes == that.maxMessageBytes;
  }

  @Override
  public int hashCode() {
    return Objects.hashCode(maxHeaderBytes, maxMessageBytes);
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + '['
        + "maxHeaderBytes=" + maxHeaderBytes + ", "
        + "maxMessageBytes=" + maxMessageBytes + "]";
  }

  private static final Factory DEFAULT_FACTORY;
  private static final Factory NOOP_FACTORY = new Factory() {
    @Override
    public BinaryLog getLog(String fullMethodName) {
      return NOOP_LOG;
    }
  };

  static {
    Factory defaultFactory = NOOP_FACTORY;
    try {
      String configStr = System.getenv("GRPC_BINARY_LOG_CONFIG");
      if (configStr != null && configStr.length() > 0) {
        defaultFactory = new FactoryImpl(configStr);
      }
    } catch (Throwable t) {
      logger.log(Level.SEVERE, "Failed to initialize binary log. Disabling binary log.", t);
      defaultFactory = NOOP_FACTORY;
    }
    DEFAULT_FACTORY = defaultFactory;
  }

  /**
   * Accepts the fullMethodName and returns the binary log that should be used. The log may be
   * a log that does nothing.
   */
  static BinaryLog getLog(String fullMethodName) {
    return DEFAULT_FACTORY.getLog(fullMethodName);
  }

  interface Factory {
    BinaryLog getLog(String fullMethodName);
  }

  static final class FactoryImpl implements Factory {
    // '*' for global, 'service/*' for service glob, or 'service/method' for fully qualified.
    private static final Pattern logPatternRe = Pattern.compile("[^{]+");
    // A curly brace wrapped expression. Will be further matched with the more specified REs below.
    private static final Pattern logOptionsRe = Pattern.compile("\\{[^}]+}");
    private static final Pattern configRe = Pattern.compile(
        String.format("^(%s)(%s)?$", logPatternRe.pattern(), logOptionsRe.pattern()));
    // Regexes to extract per-binlog options
    // The form: {m:256}
    private static final Pattern msgRe = Pattern.compile("\\{m(?::(\\d+))?}");
    // The form: {h:256}
    private static final Pattern headerRe = Pattern.compile("\\{h(?::(\\d+))?}");
    // The form: {h:256,m:256}
    private static final Pattern bothRe = Pattern.compile("\\{h(?::(\\d+))?;m(?::(\\d+))?}");

    private final BinaryLog globalLog;
    private final Map<String, BinaryLog> perServiceLogs;
    private final Map<String, BinaryLog> perMethodLogs;

    /**
     * Accepts a string in the format specified by the binary log spec.
     */
    @VisibleForTesting
    FactoryImpl(String configurationString) {
      Preconditions.checkState(configurationString != null && configurationString.length() > 0);
      BinaryLog globalLog = null;
      Map<String, BinaryLog> perServiceLogs = new HashMap<String, BinaryLog>();
      Map<String, BinaryLog> perMethodLogs = new HashMap<String, BinaryLog>();

      String[] configurations = configurationString.split(",");
      for (String configuration : configurations) {
        Matcher configMatcher = configRe.matcher(configuration);
        if (!configMatcher.matches()) {
          throw new IllegalArgumentException("Bad input: " + configuration);
        }
        String methodOrSvc = configMatcher.group(1);
        String binlogOptionStr = configMatcher.group(2);
        BinaryLog binLog = createBinaryLog(binlogOptionStr);
        if (binLog == null) {
          continue;
        }
        if (methodOrSvc.equals("*")) {
          if (globalLog != null) {
            logger.log(Level.SEVERE, "Ignoring duplicate entry: " + configuration);
            continue;
          }
          globalLog = binLog;
          logger.info("Global binlog: " + globalLog);
        } else if (isServiceGlob(methodOrSvc)) {
          String service = MethodDescriptor.extractFullServiceName(methodOrSvc);
          if (perServiceLogs.containsKey(service)) {
            logger.log(Level.SEVERE, "Ignoring duplicate entry: " + configuration);
            continue;
          }
          perServiceLogs.put(service, binLog);
          logger.info(String.format("Service binlog: service=%s log=%s", service, binLog));
        } else {
          // assume fully qualified method name
          if (perMethodLogs.containsKey(methodOrSvc)) {
            logger.log(Level.SEVERE, "Ignoring duplicate entry: " + configuration);
            continue;
          }
          perMethodLogs.put(methodOrSvc, binLog);
          logger.info(String.format("Method binlog: method=%s log=%s", methodOrSvc, binLog));
        }
      }
      this.globalLog = globalLog == null ? NOOP_LOG : globalLog;
      this.perServiceLogs = Collections.unmodifiableMap(perServiceLogs);
      this.perMethodLogs = Collections.unmodifiableMap(perMethodLogs);
    }

    /**
     * Accepts a full method name and returns the log that should be used.
     */
    @Override
    public BinaryLog getLog(String fullMethodName) {
      BinaryLog methodLog = perMethodLogs.get(fullMethodName);
      if (methodLog != null) {
        return methodLog;
      }
      BinaryLog serviceLog = perServiceLogs.get(
          MethodDescriptor.extractFullServiceName(fullMethodName));
      if (serviceLog != null) {
        return serviceLog;
      }
      return globalLog;
    }

    /**
     * Returns a binlog with the correct header and message limits or {@code null} if the input
     * is malformed. The input should be a string that is in one of these forms:
     *
     * <p>{@code {h(:\d+)?}, {m(:\d+)?}, {h(:\d+)?,m(:\d+)?}}
     *
     * <p>If the {@code logConfig} is null, the returned binlog will have a limit of
     * Integer.MAX_VALUE.
     */
    @VisibleForTesting
    @Nullable
    static BinaryLog createBinaryLog(@Nullable String logConfig) {
      if (logConfig == null) {
        return new BinaryLog(Integer.MAX_VALUE, Integer.MAX_VALUE);
      }
      try {
        Matcher headerMatcher;
        Matcher msgMatcher;
        Matcher bothMatcher;
        final int maxHeaderBytes;
        final int maxMsgBytes;
        if ((headerMatcher = headerRe.matcher(logConfig)).matches()) {
          String maxHeaderStr = headerMatcher.group(1);
          maxHeaderBytes =
              maxHeaderStr != null ? Integer.parseInt(maxHeaderStr) : Integer.MAX_VALUE;
          maxMsgBytes = 0;
        } else if ((msgMatcher = msgRe.matcher(logConfig)).matches()) {
          maxHeaderBytes = 0;
          String maxMsgStr = msgMatcher.group(1);
          maxMsgBytes = maxMsgStr != null ? Integer.parseInt(maxMsgStr) : Integer.MAX_VALUE;
        } else if ((bothMatcher = bothRe.matcher(logConfig)).matches()) {
          String maxHeaderStr = bothMatcher.group(1);
          String maxMsgStr = bothMatcher.group(2);
          maxHeaderBytes =
              maxHeaderStr != null ? Integer.parseInt(maxHeaderStr) : Integer.MAX_VALUE;
          maxMsgBytes = maxMsgStr != null ? Integer.parseInt(maxMsgStr) : Integer.MAX_VALUE;
        } else {
          logger.log(Level.SEVERE, "Illegal log config pattern: " + logConfig);
          return null;
        }
        return new BinaryLog(maxHeaderBytes, maxMsgBytes);
      } catch (NumberFormatException e) {
        logger.log(Level.SEVERE, "Illegal log config pattern: " + logConfig);
        return null;
      }
    }

    /**
     * Returns true if the input string is a glob of the form: {@code <package-service>/*}.
     */
    static boolean isServiceGlob(String input) {
      return input.endsWith("/*");
    }
  }
}

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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.base.Verify;
import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.NameResolver;
import io.grpc.Status;
import io.grpc.internal.SharedResourceHolder.Resource;
import java.io.IOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.net.URI;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Map.Entry;
import java.util.Random;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.regex.Pattern;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.naming.NamingEnumeration;
import javax.naming.NamingException;
import javax.naming.directory.Attribute;
import javax.naming.directory.InitialDirContext;

/**
 * A DNS-based {@link NameResolver}.
 *
 * <p>Each {@code A} or {@code AAAA} record emits an {@link EquivalentAddressGroup} in the list
 * passed to {@link NameResolver.Listener#onAddresses(List, Attributes)}
 *
 * @see DnsNameResolverProvider
 */
final class DnsNameResolver extends NameResolver {

  private static final Logger logger = Logger.getLogger(DnsNameResolver.class.getName());

  private static final boolean JNDI_AVAILABLE = jndiAvailable();

  private static final String SERVICE_CONFIG_CHOICE_CLIENT_LANGUAGE_KEY = "clientLanguage";
  private static final String SERVICE_CONFIG_CHOICE_PERCENTAGE_KEY = "percentage";
  private static final String SERVICE_CONFIG_CHOICE_CLIENT_HOSTNAME_KEY = "clientHostname";
  private static final String SERVICE_CONFIG_CHOICE_SERVICE_CONFIG_KEY = "serviceConfig";

  // From https://github.com/grpc/proposal/blob/master/A2-service-configs-in-dns.md
  static final String SERVICE_CONFIG_PREFIX = "_grpc_config=";
  private static final Set<String> SERVICE_CONFIG_CHOICE_KEYS =
      Collections.unmodifiableSet(
          new HashSet<String>(
              Arrays.asList(
                  SERVICE_CONFIG_CHOICE_CLIENT_LANGUAGE_KEY,
                  SERVICE_CONFIG_CHOICE_PERCENTAGE_KEY,
                  SERVICE_CONFIG_CHOICE_CLIENT_HOSTNAME_KEY,
                  SERVICE_CONFIG_CHOICE_SERVICE_CONFIG_KEY)));

  // From https://github.com/grpc/proposal/blob/master/A2-service-configs-in-dns.md
  private static final String SERVICE_CONFIG_NAME_PREFIX = "_grpc_config.";
  // From https://github.com/grpc/proposal/blob/master/A5-grpclb-in-dns.md
  private static final String GRPCLB_NAME_PREFIX = "_grpclb._tcp.";

  private static final String JNDI_PROPERTY =
      System.getProperty("io.grpc.internal.DnsNameResolverProvider.enable_jndi", "false");

  @VisibleForTesting
  static boolean enableJndi = Boolean.parseBoolean(JNDI_PROPERTY);


  @VisibleForTesting
  final ProxyDetector proxyDetector;

  /** Access through {@link #getLocalHostname}. */
  private static String localHostname;

  private final Random random = new Random();

  private DelegateResolver delegateResolver = pickDelegateResolver();

  private final String authority;
  private final String host;
  private final int port;
  private final Resource<ExecutorService> executorResource;
  @GuardedBy("this")
  private boolean shutdown;
  @GuardedBy("this")
  private ExecutorService executor;
  @GuardedBy("this")
  private boolean resolving;
  @GuardedBy("this")
  private Listener listener;

  DnsNameResolver(@Nullable String nsAuthority, String name, Attributes params,
      Resource<ExecutorService> executorResource,
      ProxyDetector proxyDetector) {
    // TODO: if a DNS server is provided as nsAuthority, use it.
    // https://www.captechconsulting.com/blogs/accessing-the-dusty-corners-of-dns-with-java
    this.executorResource = executorResource;
    // Must prepend a "//" to the name when constructing a URI, otherwise it will be treated as an
    // opaque URI, thus the authority and host of the resulted URI would be null.
    URI nameUri = URI.create("//" + name);
    authority = Preconditions.checkNotNull(nameUri.getAuthority(),
        "nameUri (%s) doesn't have an authority", nameUri);
    host = Preconditions.checkNotNull(nameUri.getHost(), "host");
    if (nameUri.getPort() == -1) {
      Integer defaultPort = params.get(NameResolver.Factory.PARAMS_DEFAULT_PORT);
      if (defaultPort != null) {
        port = defaultPort;
      } else {
        throw new IllegalArgumentException(
            "name '" + name + "' doesn't contain a port, and default port is not set in params");
      }
    } else {
      port = nameUri.getPort();
    }
    this.proxyDetector = proxyDetector;
  }

  @Override
  public final String getServiceAuthority() {
    return authority;
  }

  @Override
  public final synchronized void start(Listener listener) {
    Preconditions.checkState(this.listener == null, "already started");
    executor = SharedResourceHolder.get(executorResource);
    this.listener = Preconditions.checkNotNull(listener, "listener");
    resolve();
  }

  @Override
  public final synchronized void refresh() {
    Preconditions.checkState(listener != null, "not started");
    resolve();
  }

  private final Runnable resolutionRunnable = new Runnable() {
      @Override
      public void run() {
        Listener savedListener;
        synchronized (DnsNameResolver.this) {
          if (shutdown) {
            return;
          }
          savedListener = listener;
          resolving = true;
        }
        try {
          InetSocketAddress destination = InetSocketAddress.createUnresolved(host, port);
          ProxyParameters proxy;
          try {
            proxy = proxyDetector.proxyFor(destination);
          } catch (IOException e) {
            savedListener.onError(
                Status.UNAVAILABLE.withDescription("Unable to resolve host " + host).withCause(e));
            return;
          }
          if (proxy != null) {
            EquivalentAddressGroup server =
                new EquivalentAddressGroup(
                    new PairSocketAddress(
                        destination,
                        Attributes
                            .newBuilder()
                            .set(ProxyDetector.PROXY_PARAMS_KEY, proxy)
                            .build()));
            savedListener.onAddresses(Collections.singletonList(server), Attributes.EMPTY);
            return;
          }
          ResolutionResults resolvedInetAddrs;
          try {
            resolvedInetAddrs = delegateResolver.resolve(host);
          } catch (Exception e) {
            savedListener.onError(
                Status.UNAVAILABLE.withDescription("Unable to resolve host " + host).withCause(e));
            return;
          }
          // Each address forms an EAG
          List<EquivalentAddressGroup> servers = new ArrayList<EquivalentAddressGroup>();
          for (InetAddress inetAddr : resolvedInetAddrs.addresses) {
            servers.add(new EquivalentAddressGroup(new InetSocketAddress(inetAddr, port)));
          }
          servers.addAll(resolvedInetAddrs.balancerAddresses);

          Attributes.Builder attrs = Attributes.newBuilder();
          if (!resolvedInetAddrs.txtRecords.isEmpty()) {
            Map<String, Object> serviceConfig = null;
            try {
              for (Map<String, Object> possibleConfig :
                  parseTxtResults(resolvedInetAddrs.txtRecords)) {
                try {
                  serviceConfig =
                      maybeChooseServiceConfig(possibleConfig, random, getLocalHostname());
                } catch (RuntimeException e) {
                  logger.log(Level.WARNING, "Bad service config choice " + possibleConfig, e);
                }
                if (serviceConfig != null) {
                  break;
                }
              }
            } catch (RuntimeException e) {
              logger.log(Level.WARNING, "Can't parse service Configs", e);
            }
            if (serviceConfig != null) {
              attrs.set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, serviceConfig);
            }
          } else {
            logger.log(Level.FINE, "No TXT records found for {0}", new Object[]{host});
          }
          savedListener.onAddresses(servers, attrs.build());
        } finally {
          synchronized (DnsNameResolver.this) {
            resolving = false;
          }
        }
      }
    };

  @GuardedBy("this")
  private void resolve() {
    if (resolving || shutdown) {
      return;
    }
    executor.execute(resolutionRunnable);
  }

  @Override
  public final synchronized void shutdown() {
    if (shutdown) {
      return;
    }
    shutdown = true;
    if (executor != null) {
      executor = SharedResourceHolder.release(executorResource, executor);
    }
  }

  final int getPort() {
    return port;
  }

  private DelegateResolver pickDelegateResolver() {
    JdkResolver jdkResolver = new JdkResolver();
    if (JNDI_AVAILABLE && enableJndi) {
      return new CompositeResolver(jdkResolver, new JndiResolver());
    }
    return jdkResolver;
  }

  /**
   * Forces the resolver.  This should only be used by testing code.
   */
  @VisibleForTesting
  void setDelegateResolver(DelegateResolver delegateResolver) {
    this.delegateResolver = delegateResolver;
  }

  @SuppressWarnings("unchecked")
  @VisibleForTesting
  static List<Map<String, Object>> parseTxtResults(List<String> txtRecords) {
    List<Map<String, Object>> serviceConfigs = new ArrayList<Map<String, Object>>();
    for (String txtRecord : txtRecords) {
      if (txtRecord.startsWith(SERVICE_CONFIG_PREFIX)) {
        List<Map<String, Object>> choices;
        try {
          Object rawChoices = JsonParser.parse(txtRecord.substring(SERVICE_CONFIG_PREFIX.length()));
          if (!(rawChoices instanceof List)) {
            throw new IOException("wrong type " + rawChoices);
          }
          List<Object> listChoices = (List<Object>) rawChoices;
          for (Object obj : listChoices) {
            if (!(obj instanceof Map)) {
              throw new IOException("wrong element type " + rawChoices);
            }
          }
          choices = (List<Map<String, Object>>) (List<?>) listChoices;
        } catch (IOException e) {
          logger.log(Level.WARNING, "Bad service config: " + txtRecord, e);
          continue;
        }
        serviceConfigs.addAll(choices);
      } else {
        logger.log(Level.FINE, "Ignoring non service config {0}", new Object[]{txtRecord});
      }
    }
    return serviceConfigs;
  }

  @Nullable
  private static final Double getPercentageFromChoice(
      Map<String, Object> serviceConfigChoice) {
    if (!serviceConfigChoice.containsKey(SERVICE_CONFIG_CHOICE_PERCENTAGE_KEY)) {
      return null;
    }
    return ServiceConfigUtil.getDouble(serviceConfigChoice, SERVICE_CONFIG_CHOICE_PERCENTAGE_KEY);
  }

  @Nullable
  private static final List<String> getClientLanguagesFromChoice(
      Map<String, Object> serviceConfigChoice) {
    if (!serviceConfigChoice.containsKey(SERVICE_CONFIG_CHOICE_CLIENT_LANGUAGE_KEY)) {
      return null;
    }
    return ServiceConfigUtil.checkStringList(
        ServiceConfigUtil.getList(serviceConfigChoice, SERVICE_CONFIG_CHOICE_CLIENT_LANGUAGE_KEY));
  }

  @Nullable
  private static final List<String> getHostnamesFromChoice(
      Map<String, Object> serviceConfigChoice) {
    if (!serviceConfigChoice.containsKey(SERVICE_CONFIG_CHOICE_CLIENT_HOSTNAME_KEY)) {
      return null;
    }
    return ServiceConfigUtil.checkStringList(
        ServiceConfigUtil.getList(serviceConfigChoice, SERVICE_CONFIG_CHOICE_CLIENT_HOSTNAME_KEY));
  }

  /**
   * Determines if a given Service Config choice applies, and if so, returns it.
   *
   * @see <a href="https://github.com/grpc/proposal/blob/master/A2-service-configs-in-dns.md">
   *   Service Config in DNS</a>
   * @param choice The service config choice.
   * @return The service config object or {@code null} if this choice does not apply.
   */
  @Nullable
  @SuppressWarnings("BetaApi") // Verify isn't all that beta
  @VisibleForTesting
  static Map<String, Object> maybeChooseServiceConfig(
      Map<String, Object> choice, Random random, String hostname) {
    for (Entry<String, ?> entry : choice.entrySet()) {
      Verify.verify(SERVICE_CONFIG_CHOICE_KEYS.contains(entry.getKey()), "Bad key: %s", entry);
    }

    List<String> clientLanguages = getClientLanguagesFromChoice(choice);
    if (clientLanguages != null && !clientLanguages.isEmpty()) {
      boolean javaPresent = false;
      for (String lang : clientLanguages) {
        if ("java".equalsIgnoreCase(lang)) {
          javaPresent = true;
          break;
        }
      }
      if (!javaPresent) {
        return null;
      }
    }
    Double percentage = getPercentageFromChoice(choice);
    if (percentage != null) {
      int pct = percentage.intValue();
      Verify.verify(pct >= 0 && pct <= 100, "Bad percentage: %s", percentage);
      if (random.nextInt(100) >= pct) {
        return null;
      }
    }
    List<String> clientHostnames = getHostnamesFromChoice(choice);
    if (clientHostnames != null && !clientHostnames.isEmpty()) {
      boolean hostnamePresent = false;
      for (String clientHostname : clientHostnames) {
        if (clientHostname.equals(hostname)) {
          hostnamePresent = true;
          break;
        }
      }
      if (!hostnamePresent) {
        return null;
      }
    }
    return ServiceConfigUtil.getObject(choice, SERVICE_CONFIG_CHOICE_SERVICE_CONFIG_KEY);
  }

  /**
   * Returns whether the JNDI DNS resolver is available.  This is accomplished by looking up a
   * particular class.  It is believed to be the default (only?) DNS resolver that will actually be
   * used.  It is provided by the OpenJDK, but unlikely Android.  Actual resolution will be done by
   * using a service provider when a hostname query is present, so the {@code DnsContextFactory}
   * may not actually be used to perform the query.  This is believed to be "okay."
   */
  @VisibleForTesting
  @SuppressWarnings("LiteralClassName")
  static boolean jndiAvailable() {
    if (GrpcUtil.IS_RESTRICTED_APPENGINE) {
      return false;
    }
    try {
      Class.forName("javax.naming.directory.InitialDirContext");
      Class.forName("com.sun.jndi.dns.DnsContextFactory");
    } catch (ClassNotFoundException e) {
      logger.log(Level.FINE, "Unable to find JNDI DNS resolver, skipping", e);
      return false;
    }
    return true;
  }

  /**
   * Common interface between the delegate resolvers used by DnsNameResolver.
   */
  @VisibleForTesting
  abstract static class DelegateResolver {
    abstract ResolutionResults resolve(String host) throws Exception;
  }

  /**
   * Describes the results from a DNS query.
   */
  @VisibleForTesting
  static final class ResolutionResults {
    final List<InetAddress> addresses;
    final List<String> txtRecords;
    final List<EquivalentAddressGroup> balancerAddresses;

    ResolutionResults(
        List<InetAddress> addresses,
        List<String> txtRecords,
        List<EquivalentAddressGroup> balancerAddresses) {
      this.addresses = Collections.unmodifiableList(checkNotNull(addresses, "addresses"));
      this.txtRecords = Collections.unmodifiableList(checkNotNull(txtRecords, "txtRecords"));
      this.balancerAddresses =
          Collections.unmodifiableList(checkNotNull(balancerAddresses, "balancerAddresses"));
    }
  }

  /**
   * A composite DNS resolver that uses both the JDK and JNDI resolvers as delegate.  It is
   * expected that two DNS queries will be executed, with the second one being from JNDI.
   */
  @VisibleForTesting
  static final class CompositeResolver extends DelegateResolver {

    private final DelegateResolver jdkResovler;
    private final DelegateResolver jndiResovler;

    CompositeResolver(DelegateResolver jdkResovler, DelegateResolver jndiResovler) {
      this.jdkResovler = jdkResovler;
      this.jndiResovler = jndiResovler;
    }

    @Override
    ResolutionResults resolve(String host) throws Exception {
      ResolutionResults jdkResults = jdkResovler.resolve(host);
      List<InetAddress> addresses = jdkResults.addresses;
      List<String> txtRecords = Collections.emptyList();
      List<EquivalentAddressGroup> balancerAddresses = Collections.emptyList();
      try {
        ResolutionResults jdniResults = jndiResovler.resolve(host);
        txtRecords = jdniResults.txtRecords;
        balancerAddresses = jdniResults.balancerAddresses;
      } catch (Throwable e) {
        // JndiResolver.resolve may throw Error that could cause rpc to hang.
        // Catch and log Throwable and keep using jdkResolver's result to prevent it.
        logger.log(Level.SEVERE, "Failed to resolve TXT results", e);
      }

      return new ResolutionResults(addresses, txtRecords, balancerAddresses);
    }
  }

  /**
   * The default name resolver provided with the JDK.  This is unable to lookup TXT records, but
   * provides address ordering sorted according to RFC 3484.  This is true on OpenJDK, because it
   * in turn calls into libc which sorts addresses in order of reachability.
   */
  @VisibleForTesting
  static final class JdkResolver extends DelegateResolver {

    @Override
    ResolutionResults resolve(String host) throws Exception {
      return new ResolutionResults(
          Arrays.asList(InetAddress.getAllByName(host)),
          Collections.<String>emptyList(),
          Collections.<EquivalentAddressGroup>emptyList());
    }
  }

  /**
   * A resolver that uses JNDI.  This class is capable of looking up both addresses
   * and text records, but does not provide ordering guarantees.  It is currently not used for
   * address resolution.
   */
  @VisibleForTesting
  static final class JndiResolver extends DelegateResolver {

    private static final Pattern whitespace = Pattern.compile("\\s+");

    @SuppressWarnings("BetaApi") // Verify is stable in Guava 23.5
    @Override
    ResolutionResults resolve(String host) throws NamingException {
      List<String> serviceConfigTxtRecords = Collections.emptyList();
      String serviceConfigHostname = SERVICE_CONFIG_NAME_PREFIX + host;
      if (logger.isLoggable(Level.FINER)) {
        logger.log(
            Level.FINER, "About to query TXT records for {0}", new Object[]{serviceConfigHostname});
      }
      try {
        serviceConfigTxtRecords = getAllRecords("TXT", "dns:///" + serviceConfigHostname);
      } catch (NamingException e) {

        if (logger.isLoggable(Level.FINE)) {
          logger.log(Level.FINE, "Unable to look up " + serviceConfigHostname, e);
        }
      }

      String grpclbHostname = GRPCLB_NAME_PREFIX + host;
      if (logger.isLoggable(Level.FINER)) {
        logger.log(
            Level.FINER, "About to query SRV records for {0}", new Object[]{grpclbHostname});
      }
      List<EquivalentAddressGroup> balancerAddresses = Collections.emptyList();
      try {
        List<String> grpclbSrvRecords = getAllRecords("SRV", "dns:///" + grpclbHostname);
        balancerAddresses = new ArrayList<EquivalentAddressGroup>(grpclbSrvRecords.size());
        for (String srvRecord : grpclbSrvRecords) {
          try {
            String[] parts = whitespace.split(srvRecord);
            Verify.verify(parts.length == 4, "Bad SRV Record: %s, ", srvRecord);
            String srvHostname = parts[3];
            int port = Integer.parseInt(parts[2]);

            InetAddress[] addrs = InetAddress.getAllByName(srvHostname);
            List<SocketAddress> sockaddrs = new ArrayList<SocketAddress>(addrs.length);
            for (InetAddress addr : addrs) {
              sockaddrs.add(new InetSocketAddress(addr, port));
            }
            Attributes attrs = Attributes.newBuilder()
                .set(GrpcAttributes.ATTR_LB_ADDR_AUTHORITY, srvHostname)
                .build();
            balancerAddresses.add(
                new EquivalentAddressGroup(Collections.unmodifiableList(sockaddrs), attrs));
          } catch (UnknownHostException e) {
            logger.log(Level.WARNING, "Can't find address for SRV record" + srvRecord, e);
          } catch (RuntimeException e) {
            logger.log(Level.WARNING, "Failed to construct SRV record" + srvRecord, e);
          }
        }
      } catch (NamingException e) {
        if (logger.isLoggable(Level.FINE)) {
          logger.log(Level.FINE, "Unable to look up " + serviceConfigHostname, e);
        }
      }

      return new ResolutionResults(
          /*addresses=*/ Collections.<InetAddress>emptyList(),
          serviceConfigTxtRecords,
          Collections.unmodifiableList(balancerAddresses));
    }

    private List<String> getAllRecords(String recordType, String name) throws NamingException {
      InitialDirContext dirContext = new InitialDirContext();
      String[] rrType = new String[]{recordType};
      javax.naming.directory.Attributes attrs = dirContext.getAttributes(name, rrType);
      List<String> records = new ArrayList<String>();

      NamingEnumeration<? extends Attribute> rrGroups = attrs.getAll();
      try {
        while (rrGroups.hasMore()) {
          Attribute rrEntry = rrGroups.next();
          assert Arrays.asList(rrType).contains(rrEntry.getID());
          NamingEnumeration<?> rrValues = rrEntry.getAll();
          try {
            while (rrValues.hasMore()) {
              records.add(unquote(String.valueOf(rrValues.next())));
            }
          } finally {
            rrValues.close();
          }
        }
      } finally {
        rrGroups.close();
      }
      return records;
    }
  }

  /**
   * Undo the quoting done in {@link com.sun.jndi.dns.ResourceRecord#decodeTxt}.
   */
  @VisibleForTesting
  static String unquote(String txtRecord) {
    StringBuilder sb = new StringBuilder(txtRecord.length());
    boolean inquote = false;
    for (int i = 0; i < txtRecord.length(); i++) {
      char c = txtRecord.charAt(i);
      if (!inquote) {
        if (c == ' ') {
          continue;
        } else if (c == '"') {
          inquote = true;
          continue;
        }
      } else {
        if (c == '"') {
          inquote = false;
          continue;
        } else if (c == '\\') {
          c = txtRecord.charAt(++i);
          assert c == '"' || c == '\\';
        }
      }
      sb.append(c);
    }
    return sb.toString();
  }

  private static String getLocalHostname() {
    if (localHostname == null) {
      try {
        localHostname = InetAddress.getLocalHost().getHostName();
      } catch (UnknownHostException e) {
        throw new RuntimeException(e);
      }
    }
    return localHostname;
  }
}

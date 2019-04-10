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
import com.google.common.base.MoreObjects;
import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Throwables;
import com.google.common.base.Verify;
import com.google.common.base.VerifyException;
import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.NameResolver;
import io.grpc.NameResolver.ConfigOrError;
import io.grpc.ProxiedSocketAddress;
import io.grpc.ProxyDetector;
import io.grpc.Status;
import io.grpc.SynchronizationContext;
import io.grpc.internal.SharedResourceHolder.Resource;
import java.io.IOException;
import java.lang.reflect.Constructor;
import java.net.InetAddress;
import java.net.InetSocketAddress;
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
import java.util.concurrent.Executor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * A DNS-based {@link NameResolver}.
 *
 * <p>Each {@code A} or {@code AAAA} record emits an {@link EquivalentAddressGroup} in the list
 * passed to {@link NameResolver.Observer#onResult(ResolutionResult)}.
 *
 * @see DnsNameResolverProvider
 */
final class DnsNameResolver extends NameResolver {

  private static final Logger logger = Logger.getLogger(DnsNameResolver.class.getName());

  private static final String SERVICE_CONFIG_CHOICE_CLIENT_LANGUAGE_KEY = "clientLanguage";
  private static final String SERVICE_CONFIG_CHOICE_PERCENTAGE_KEY = "percentage";
  private static final String SERVICE_CONFIG_CHOICE_CLIENT_HOSTNAME_KEY = "clientHostname";
  private static final String SERVICE_CONFIG_CHOICE_SERVICE_CONFIG_KEY = "serviceConfig";

  // From https://github.com/grpc/proposal/blob/master/A2-service-configs-in-dns.md
  static final String SERVICE_CONFIG_PREFIX = "grpc_config=";
  private static final Set<String> SERVICE_CONFIG_CHOICE_KEYS =
      Collections.unmodifiableSet(
          new HashSet<>(
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
      System.getProperty("io.grpc.internal.DnsNameResolverProvider.enable_jndi", "true");
  private static final String JNDI_LOCALHOST_PROPERTY =
      System.getProperty("io.grpc.internal.DnsNameResolverProvider.enable_jndi_localhost", "false");
  private static final String JNDI_SRV_PROPERTY =
      System.getProperty("io.grpc.internal.DnsNameResolverProvider.enable_grpclb", "false");
  private static final String JNDI_TXT_PROPERTY =
      System.getProperty("io.grpc.internal.DnsNameResolverProvider.enable_service_config", "false");

  /**
   * Java networking system properties name for caching DNS result.
   *
   * <p>Default value is -1 (cache forever) if security manager is installed. If security manager is
   * not installed, the ttl value is {@code null} which falls back to {@link
   * #DEFAULT_NETWORK_CACHE_TTL_SECONDS gRPC default value}.
   *
   * <p>For android, gRPC doesn't attempt to cache; this property value will be ignored.
   */
  @VisibleForTesting
  static final String NETWORKADDRESS_CACHE_TTL_PROPERTY = "networkaddress.cache.ttl";
  /** Default DNS cache duration if network cache ttl value is not specified ({@code null}). */
  @VisibleForTesting
  static final long DEFAULT_NETWORK_CACHE_TTL_SECONDS = 30;

  @VisibleForTesting
  static boolean enableJndi = Boolean.parseBoolean(JNDI_PROPERTY);
  @VisibleForTesting
  static boolean enableJndiLocalhost = Boolean.parseBoolean(JNDI_LOCALHOST_PROPERTY);
  @VisibleForTesting
  static boolean enableSrv = Boolean.parseBoolean(JNDI_SRV_PROPERTY);
  @VisibleForTesting
  static boolean enableTxt = Boolean.parseBoolean(JNDI_TXT_PROPERTY);

  private static final ResourceResolverFactory resourceResolverFactory =
      getResourceResolverFactory(DnsNameResolver.class.getClassLoader());

  @VisibleForTesting
  final ProxyDetector proxyDetector;

  /** Access through {@link #getLocalHostname}. */
  private static String localHostname;

  private final Random random = new Random();

  private volatile AddressResolver addressResolver = JdkAddressResolver.INSTANCE;
  private final AtomicReference<ResourceResolver> resourceResolver = new AtomicReference<>();

  private final String authority;
  private final String host;
  private final int port;
  private final Resource<Executor> executorResource;
  private final long cacheTtlNanos;
  private final SynchronizationContext syncContext;

  // Following fields must be accessed from syncContext
  private final Stopwatch stopwatch;
  private ResolutionResults cachedResolutionResults;
  private boolean shutdown;
  private Executor executor;
  private boolean resolving;

  // The field must be accessed from syncContext, although the methods on an Observer can be called
  // from any thread.
  private NameResolver.Observer observer;

  DnsNameResolver(@Nullable String nsAuthority, String name, Helper helper,
      Resource<Executor> executorResource, Stopwatch stopwatch, boolean isAndroid) {
    Preconditions.checkNotNull(helper, "helper");
    // TODO: if a DNS server is provided as nsAuthority, use it.
    // https://www.captechconsulting.com/blogs/accessing-the-dusty-corners-of-dns-with-java
    this.executorResource = executorResource;
    // Must prepend a "//" to the name when constructing a URI, otherwise it will be treated as an
    // opaque URI, thus the authority and host of the resulted URI would be null.
    URI nameUri = URI.create("//" + checkNotNull(name, "name"));
    Preconditions.checkArgument(nameUri.getHost() != null, "Invalid DNS name: %s", name);
    authority = Preconditions.checkNotNull(nameUri.getAuthority(),
        "nameUri (%s) doesn't have an authority", nameUri);
    host = nameUri.getHost();
    if (nameUri.getPort() == -1) {
      port = helper.getDefaultPort();
    } else {
      port = nameUri.getPort();
    }
    this.proxyDetector = Preconditions.checkNotNull(helper.getProxyDetector(), "proxyDetector");
    this.cacheTtlNanos = getNetworkAddressCacheTtlNanos(isAndroid);
    this.stopwatch = Preconditions.checkNotNull(stopwatch, "stopwatch");
    this.syncContext =
        Preconditions.checkNotNull(helper.getSynchronizationContext(), "syncContext");
  }

  @Override
  public String getServiceAuthority() {
    return authority;
  }

  @Override
  public void start(Observer observer) {
    Preconditions.checkState(this.observer == null, "already started");
    executor = SharedResourceHolder.get(executorResource);
    this.observer = Preconditions.checkNotNull(observer, "observer");
    resolve();
  }

  @Override
  public void refresh() {
    Preconditions.checkState(observer != null, "not started");
    resolve();
  }

  private final class Resolve implements Runnable {
    private final Observer savedObserver;

    Resolve(Observer savedObserver) {
      this.savedObserver = Preconditions.checkNotNull(savedObserver, "savedObserver");
    }

    @Override
    public void run() {
      if (logger.isLoggable(Level.FINER)) {
        logger.finer("Attempting DNS resolution of " + host);
      }
      try {
        resolveInternal();
      } finally {
        syncContext.execute(new Runnable() {
            @Override
            public void run() {
              resolving = false;
            }
          });
      }
    }

    @VisibleForTesting
    void resolveInternal() {
      InetSocketAddress destination =
          InetSocketAddress.createUnresolved(host, port);
      ProxiedSocketAddress proxiedAddr;
      try {
        proxiedAddr = proxyDetector.proxyFor(destination);
      } catch (IOException e) {
        savedObserver.onError(
            Status.UNAVAILABLE.withDescription("Unable to resolve host " + host).withCause(e));
        return;
      }
      if (proxiedAddr != null) {
        if (logger.isLoggable(Level.FINER)) {
          logger.finer("Using proxy address " + proxiedAddr);
        }
        EquivalentAddressGroup server = new EquivalentAddressGroup(proxiedAddr);
        ResolutionResult resolutionResult =
            ResolutionResult.newBuilder()
                .setServers(Collections.singletonList(server))
                .setAttributes(Attributes.EMPTY)
                .build();
        savedObserver.onResult(resolutionResult);
        return;
      }

      ResolutionResults resolutionResults;
      try {
        ResourceResolver resourceResolver = null;
        if (shouldUseJndi(enableJndi, enableJndiLocalhost, host)) {
          resourceResolver = getResourceResolver();
        }
        final ResolutionResults results = resolveAll(
            addressResolver,
            resourceResolver,
            enableSrv,
            enableTxt,
            host);
        resolutionResults = results;
        syncContext.execute(new Runnable() {
            @Override
            public void run() {
              cachedResolutionResults = results;
              if (cacheTtlNanos > 0) {
                stopwatch.reset().start();
              }
            }
          });
        if (logger.isLoggable(Level.FINER)) {
          logger.finer("Found DNS results " + resolutionResults + " for " + host);
        }
      } catch (Exception e) {
        savedObserver.onError(
            Status.UNAVAILABLE.withDescription("Unable to resolve host " + host).withCause(e));
        return;
      }
      // Each address forms an EAG
      List<EquivalentAddressGroup> servers = new ArrayList<>();
      for (InetAddress inetAddr : resolutionResults.addresses) {
        servers.add(new EquivalentAddressGroup(new InetSocketAddress(inetAddr, port)));
      }
      servers.addAll(resolutionResults.balancerAddresses);
      if (servers.isEmpty()) {
        savedObserver.onError(Status.UNAVAILABLE.withDescription(
            "No DNS backend or balancer addresses found for " + host));
        return;
      }

      Attributes.Builder attrs = Attributes.newBuilder();
      if (!resolutionResults.txtRecords.isEmpty()) {
        ConfigOrError serviceConfig =
            parseServiceConfig(resolutionResults.txtRecords, random, getLocalHostname());
        if (serviceConfig != null) {
          if (serviceConfig.getError() != null) {
            savedObserver.onError(serviceConfig.getError());
            return;
          } else {
            @SuppressWarnings("unchecked")
            Map<String, ?> config = (Map<String, ?>) serviceConfig.getConfig();
            attrs.set(GrpcAttributes.NAME_RESOLVER_SERVICE_CONFIG, config);
          }
        }
      } else {
        logger.log(Level.FINE, "No TXT records found for {0}", new Object[]{host});
      }
      ResolutionResult resolutionResult =
          ResolutionResult.newBuilder().setServers(servers).setAttributes(attrs.build()).build();
      savedObserver.onResult(resolutionResult);
    }
  }

  @Nullable
  static ConfigOrError parseServiceConfig(
      List<String> rawTxtRecords, Random random, String localHostname) {
    List<Map<String, ?>> possibleServiceConfigChoices;
    try {
      possibleServiceConfigChoices = parseTxtResults(rawTxtRecords);
    } catch (IOException | RuntimeException e) {
      return ConfigOrError.fromError(
          Status.UNKNOWN.withDescription("failed to parse TXT records").withCause(e));
    }
    Map<String, ?> possibleServiceConfig = null;
    for (Map<String, ?> possibleServiceConfigChoice : possibleServiceConfigChoices) {
      try {
        possibleServiceConfig =
            maybeChooseServiceConfig(possibleServiceConfigChoice, random, localHostname);
      } catch (RuntimeException e) {
        return ConfigOrError.fromError(
            Status.UNKNOWN.withDescription("failed to pick service config choice").withCause(e));
      }
      if (possibleServiceConfig != null) {
        break;
      }
    }
    if (possibleServiceConfig == null) {
      return null;
    }
    return ConfigOrError.fromConfig(possibleServiceConfig);
  }

  private void resolve() {
    if (resolving || shutdown || !cacheRefreshRequired()) {
      return;
    }
    resolving = true;
    executor.execute(new Resolve(observer));
  }

  private boolean cacheRefreshRequired() {
    return cachedResolutionResults == null
        || cacheTtlNanos == 0
        || (cacheTtlNanos > 0 && stopwatch.elapsed(TimeUnit.NANOSECONDS) > cacheTtlNanos);
  }

  @Override
  public void shutdown() {
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

  @VisibleForTesting
  static ResolutionResults resolveAll(
      AddressResolver addressResolver,
      @Nullable ResourceResolver resourceResolver,
      boolean requestSrvRecords,
      boolean requestTxtRecords,
      String name) {
    List<? extends InetAddress> addresses = Collections.emptyList();
    Exception addressesException = null;
    List<EquivalentAddressGroup> balancerAddresses = Collections.emptyList();
    Exception balancerAddressesException = null;
    List<String> txtRecords = Collections.emptyList();
    Exception txtRecordsException = null;

    try {
      addresses = addressResolver.resolveAddress(name);
    } catch (Exception e) {
      addressesException = e;
    }
    if (resourceResolver != null) {
      if (requestSrvRecords) {
        try {
          balancerAddresses =
              resourceResolver.resolveSrv(addressResolver, GRPCLB_NAME_PREFIX + name);
        } catch (Exception e) {
          balancerAddressesException = e;
        }
      }
      if (requestTxtRecords) {
        boolean balancerLookupFailedOrNotAttempted =
            !requestSrvRecords || balancerAddressesException != null;
        boolean dontResolveTxt =
            (addressesException != null) && balancerLookupFailedOrNotAttempted;
        // Only do the TXT record lookup if one of the above address resolutions succeeded.
        if (!dontResolveTxt) {
          try {
            txtRecords = resourceResolver.resolveTxt(SERVICE_CONFIG_NAME_PREFIX + name);
          } catch (Exception e) {
            txtRecordsException = e;
          }
        }
      }
    }
    try {
      if (addressesException != null
          && (balancerAddressesException != null || balancerAddresses.isEmpty())) {
        Throwables.throwIfUnchecked(addressesException);
        throw new RuntimeException(addressesException);
      }
    } finally {
      if (addressesException != null) {
        logger.log(Level.FINE, "Address resolution failure", addressesException);
      }
      if (balancerAddressesException != null) {
        logger.log(Level.FINE, "Balancer resolution failure", balancerAddressesException);
      }
      if (txtRecordsException != null) {
        logger.log(Level.FINE, "ServiceConfig resolution failure", txtRecordsException);
      }
    }
    return new ResolutionResults(addresses, txtRecords, balancerAddresses);
  }

  /**
   *
   * @throws IOException if one of the txt records contains improperly formatted JSON.
   */
  @SuppressWarnings("unchecked")
  @VisibleForTesting
  static List<Map<String, ?>> parseTxtResults(List<String> txtRecords) throws IOException {
    List<Map<String, ?>> possibleServiceConfigChoices = new ArrayList<>();
    for (String txtRecord : txtRecords) {
      if (!txtRecord.startsWith(SERVICE_CONFIG_PREFIX)) {
        logger.log(Level.FINE, "Ignoring non service config {0}", new Object[]{txtRecord});
        continue;
      }
      Object rawChoices = JsonParser.parse(txtRecord.substring(SERVICE_CONFIG_PREFIX.length()));
      if (!(rawChoices instanceof List)) {
        throw new ClassCastException("wrong type " + rawChoices);
      }
      List<?> listChoices = (List<?>) rawChoices;
      possibleServiceConfigChoices.addAll(ServiceConfigUtil.checkObjectList(listChoices));
    }
    return possibleServiceConfigChoices;
  }

  @Nullable
  private static final Double getPercentageFromChoice(Map<String, ?> serviceConfigChoice) {
    if (!serviceConfigChoice.containsKey(SERVICE_CONFIG_CHOICE_PERCENTAGE_KEY)) {
      return null;
    }
    return ServiceConfigUtil.getDouble(serviceConfigChoice, SERVICE_CONFIG_CHOICE_PERCENTAGE_KEY);
  }

  @Nullable
  private static final List<String> getClientLanguagesFromChoice(
      Map<String, ?> serviceConfigChoice) {
    if (!serviceConfigChoice.containsKey(SERVICE_CONFIG_CHOICE_CLIENT_LANGUAGE_KEY)) {
      return null;
    }
    return ServiceConfigUtil.checkStringList(
        ServiceConfigUtil.getList(serviceConfigChoice, SERVICE_CONFIG_CHOICE_CLIENT_LANGUAGE_KEY));
  }

  @Nullable
  private static final List<String> getHostnamesFromChoice(Map<String, ?> serviceConfigChoice) {
    if (!serviceConfigChoice.containsKey(SERVICE_CONFIG_CHOICE_CLIENT_HOSTNAME_KEY)) {
      return null;
    }
    return ServiceConfigUtil.checkStringList(
        ServiceConfigUtil.getList(serviceConfigChoice, SERVICE_CONFIG_CHOICE_CLIENT_HOSTNAME_KEY));
  }

  /**
   * Returns value of network address cache ttl property if not Android environment. For android,
   * DnsNameResolver does not cache the dns lookup result.
   */
  private static long getNetworkAddressCacheTtlNanos(boolean isAndroid) {
    if (isAndroid) {
      // on Android, ignore dns cache.
      return 0;
    }

    String cacheTtlPropertyValue = System.getProperty(NETWORKADDRESS_CACHE_TTL_PROPERTY);
    long cacheTtl = DEFAULT_NETWORK_CACHE_TTL_SECONDS;
    if (cacheTtlPropertyValue != null) {
      try {
        cacheTtl = Long.parseLong(cacheTtlPropertyValue);
      } catch (NumberFormatException e) {
        logger.log(
            Level.WARNING,
            "Property({0}) valid is not valid number format({1}), fall back to default({2})",
            new Object[] {NETWORKADDRESS_CACHE_TTL_PROPERTY, cacheTtlPropertyValue, cacheTtl});
      }
    }
    return cacheTtl > 0 ? TimeUnit.SECONDS.toNanos(cacheTtl) : cacheTtl;
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
  @VisibleForTesting
  static Map<String, ?> maybeChooseServiceConfig(
      Map<String, ?> choice, Random random, String hostname) {
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
    Map<String, ?> sc =
        ServiceConfigUtil.getObject(choice, SERVICE_CONFIG_CHOICE_SERVICE_CONFIG_KEY);
    if (sc == null) {
      throw new VerifyException(String.format(
          "key '%s' missing in '%s'", choice, SERVICE_CONFIG_CHOICE_SERVICE_CONFIG_KEY));
    }
    return sc;
  }

  /**
   * Describes the results from a DNS query.
   */
  @VisibleForTesting
  static final class ResolutionResults {
    final List<? extends InetAddress> addresses;
    final List<String> txtRecords;
    final List<EquivalentAddressGroup> balancerAddresses;

    ResolutionResults(
        List<? extends InetAddress> addresses,
        List<String> txtRecords,
        List<EquivalentAddressGroup> balancerAddresses) {
      this.addresses = Collections.unmodifiableList(checkNotNull(addresses, "addresses"));
      this.txtRecords = Collections.unmodifiableList(checkNotNull(txtRecords, "txtRecords"));
      this.balancerAddresses =
          Collections.unmodifiableList(checkNotNull(balancerAddresses, "balancerAddresses"));
    }

    @Override
    public String toString() {
      return MoreObjects.toStringHelper(this)
          .add("addresses", addresses)
          .add("txtRecords", txtRecords)
          .add("balancerAddresses", balancerAddresses)
          .toString();
    }
  }

  @VisibleForTesting
  void setAddressResolver(AddressResolver addressResolver) {
    this.addressResolver = addressResolver;
  }

  @VisibleForTesting
  void setResourceResolver(ResourceResolver resourceResolver) {
    this.resourceResolver.set(resourceResolver);
  }

  /**
   * {@link ResourceResolverFactory} is a factory for making resource resolvers.  It supports
   * optionally checking if the factory is available.
   */
  interface ResourceResolverFactory {

    /**
     * Creates a new resource resolver.  The return value is {@code null} iff
     * {@link #unavailabilityCause()} is not null;
     */
    @Nullable ResourceResolver newResourceResolver();

    /**
     * Returns the reason why the resource resolver cannot be created.  The return value is
     * {@code null} if {@link #newResourceResolver()} is suitable for use.
     */
    @Nullable Throwable unavailabilityCause();
  }

  /**
   * AddressResolver resolves a hostname into a list of addresses.
   */
  interface AddressResolver {
    List<InetAddress> resolveAddress(String host) throws Exception;
  }

  private enum JdkAddressResolver implements AddressResolver {
    INSTANCE;

    @Override
    public List<InetAddress> resolveAddress(String host) throws UnknownHostException {
      return Collections.unmodifiableList(Arrays.asList(InetAddress.getAllByName(host)));
    }
  }

  /**
   * {@link ResourceResolver} is a Dns ResourceRecord resolver.
   */
  interface ResourceResolver {
    List<String> resolveTxt(String host) throws Exception;

    List<EquivalentAddressGroup> resolveSrv(
        AddressResolver addressResolver, String host) throws Exception;
  }

  @Nullable
  private ResourceResolver getResourceResolver() {
    ResourceResolver rr;
    if ((rr = resourceResolver.get()) == null) {
      if (resourceResolverFactory != null) {
        assert resourceResolverFactory.unavailabilityCause() == null;
        rr = resourceResolverFactory.newResourceResolver();
      }
    }
    return rr;
  }

  @Nullable
  @VisibleForTesting
  static ResourceResolverFactory getResourceResolverFactory(ClassLoader loader) {
    Class<? extends ResourceResolverFactory> jndiClazz;
    try {
      jndiClazz =
          Class.forName("io.grpc.internal.JndiResourceResolverFactory", true, loader)
              .asSubclass(ResourceResolverFactory.class);
    } catch (ClassNotFoundException e) {
      logger.log(Level.FINE, "Unable to find JndiResourceResolverFactory, skipping.", e);
      return null;
    }
    Constructor<? extends ResourceResolverFactory> jndiCtor;
    try {
      jndiCtor = jndiClazz.getConstructor();
    } catch (Exception e) {
      logger.log(Level.FINE, "Can't find JndiResourceResolverFactory ctor, skipping.", e);
      return null;
    }
    ResourceResolverFactory rrf;
    try {
      rrf = jndiCtor.newInstance();
    } catch (Exception e) {
      logger.log(Level.FINE, "Can't construct JndiResourceResolverFactory, skipping.", e);
      return null;
    }
    if (rrf.unavailabilityCause() != null) {
      logger.log(
          Level.FINE,
          "JndiResourceResolverFactory not available, skipping.",
          rrf.unavailabilityCause());
      return null;
    }
    return rrf;
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

  @VisibleForTesting
  static boolean shouldUseJndi(boolean jndiEnabled, boolean jndiLocalhostEnabled, String target) {
    if (!jndiEnabled) {
      return false;
    }
    if ("localhost".equalsIgnoreCase(target)) {
      return jndiLocalhostEnabled;
    }
    // Check if this name looks like IPv6
    if (target.contains(":")) {
      return false;
    }
    // Check if this might be IPv4.  Such addresses have no alphabetic characters.  This also
    // checks the target is empty.
    boolean alldigits = true;
    for (int i = 0; i < target.length(); i++) {
      char c = target.charAt(i);
      if (c != '.') {
        alldigits &= (c >= '0' && c <= '9');
      }
    }
    return !alldigits;
  }
}

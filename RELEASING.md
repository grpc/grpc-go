How to Create a Release of GRPC Java (for Maintainers Only)
===============================================================

Build Environments
------------------
We deploy GRPC to Maven Central under the following systems:
- Ubuntu 14.04 with Docker 13.03.0 that runs CentOS 6.9
- Windows 7 64-bit with Visual Studio
- Mac OS X 10.12.6

Other systems may also work, but we haven't verified them.

Prerequisites
-------------

### Set Up OSSRH Account

If you haven't deployed artifacts to Maven Central before, you need to setup
your OSSRH (OSS Repository Hosting) account.
- Follow the instructions on [this
  page](http://central.sonatype.org/pages/ossrh-guide.html) to set up an
  account with OSSRH.
  - You only need to create the account, not set up a new project
  - Contact a gRPC maintainer to add your account after you have created it.

Common Variables
----------------
Many of the following commands expect release-specific variables to be set. Set
them before continuing, and set them again when resuming.

```bash
$ MAJOR=1 MINOR=7 PATCH=0 # Set appropriately for new release
$ VERSION_FILES=(
  build.gradle
  android/build.gradle
  android-interop-testing/app/build.gradle
  core/src/main/java/io/grpc/internal/GrpcUtil.java
  cronet/build.gradle
  documentation/android-channel-builder.md
  examples/build.gradle
  examples/pom.xml
  examples/android/clientcache/app/build.gradle
  examples/android/helloworld/app/build.gradle
  examples/android/routeguide/app/build.gradle
  examples/example-kotlin/build.gradle
  examples/example-kotlin/android/helloworld/app/build.gradle
  )
```


Branching the Release
---------------------
The first step in the release process is to create a release branch and bump
the SNAPSHOT version. Our release branches follow the naming
convention of `v<major>.<minor>.x`, while the tags include the patch version
`v<major>.<minor>.<patch>`. For example, the same branch `v1.7.x`
would be used to create all `v1.7` tags (e.g. `v1.7.0`, `v1.7.1`).

1. For `master`, change root build files to the next minor snapshot (e.g.
   ``1.8.0-SNAPSHOT``).

   ```bash
   $ git checkout -b bump-version master
   # Change version to next minor (and keep -SNAPSHOT)
   $ sed -i 's/[0-9]\+\.[0-9]\+\.[0-9]\+\(.*CURRENT_GRPC_VERSION\)/'$MAJOR.$((MINOR+1)).0'\1/' \
     "${VERSION_FILES[@]}"
   $ sed -i s/$MAJOR.$MINOR.$PATCH/$MAJOR.$((MINOR+1)).0/ \
     compiler/src/test{,Lite,Nano}/golden/Test{,Deprecated}Service.java.txt
   $ ./gradlew build
   $ git commit -a -m "Start $MAJOR.$((MINOR+1)).0 development cycle"
   ```
2. Go through PR review and submit.
3. Create the release branch starting just before your commit and push it to GitHub:

   ```bash
   $ git fetch upstream
   $ git checkout -b v$MAJOR.$MINOR.x \
     $(git log --pretty=format:%H --grep "^Start $MAJOR.$((MINOR+1)).0 development cycle$" upstream/master)^
   $ git push upstream v$MAJOR.$MINOR.x
   ```
4. Go to [Travis CI settings](https://travis-ci.org/grpc/grpc-java/settings) and
   add a _Cron Job_:
   * Branch: `v$MAJOR.$MINOR.x`
   * Interval: `weekly`
   * Options: `Do not run if there has been a build in the last 24h`
   * Click _Add_ button
5. Continue with Google-internal steps at go/grpc/java/releasing.
6. Create a milestone for the next release.
7. Move items out of the release milestone that didn't make the cut. Issues that
   may be backported should stay in the release milestone. Treat issues with the
   'release blocker' label with special care.

Tagging the Release
-------------------

1. Verify there are no open issues in the release milestone. Open issues should
   either be deferred or resolved and the fix backported.
2. For vMajor.Minor.x branch, change `README.md` to refer to the next release
   version. _Also_ update the version numbers for protoc if the protobuf library
   version was updated since the last release.

   ```bash
   $ git checkout v$MAJOR.$MINOR.x
   $ git pull upstream v$MAJOR.$MINOR.x
   $ git checkout -b release
   # Bump documented versions. Don't forget protobuf version
   $ ${EDITOR:-nano -w} README.md
   $ git commit -a -m "Update README to reference $MAJOR.$MINOR.$PATCH"
   ```
3. Change root build files to remove "-SNAPSHOT" for the next release version
   (e.g. `0.7.0`). Commit the result and make a tag:

   ```bash
   # Change version to remove -SNAPSHOT
   $ sed -i 's/-SNAPSHOT\(.*CURRENT_GRPC_VERSION\)/\1/' "${VERSION_FILES[@]}"
   $ sed -i s/-SNAPSHOT// compiler/src/test{,Lite,Nano}/golden/TestService.java.txt
   $ ./gradlew build
   $ git commit -a -m "Bump version to $MAJOR.$MINOR.$PATCH"
   $ git tag -a v$MAJOR.$MINOR.$PATCH -m "Version $MAJOR.$MINOR.$PATCH"
   ```
4. Change root build files to the next snapshot version (e.g. `0.7.1-SNAPSHOT`).
   Commit the result:

   ```bash
   # Change version to next patch and add -SNAPSHOT
   $ sed -i 's/[0-9]\+\.[0-9]\+\.[0-9]\+\(.*CURRENT_GRPC_VERSION\)/'$MAJOR.$MINOR.$((PATCH+1))-SNAPSHOT'\1/' \
     "${VERSION_FILES[@]}"
   $ sed -i s/$MAJOR.$MINOR.$PATCH/$MAJOR.$MINOR.$((PATCH+1))-SNAPSHOT/ compiler/src/test{,Lite,Nano}/golden/TestService.java.txt
   $ ./gradlew build
   $ git commit -a -m "Bump version to $MAJOR.$MINOR.$((PATCH+1))-SNAPSHOT"
   ```
5. Go through PR review and push the release tag and updated release branch to
   GitHub:

   ```bash
   $ git checkout v$MAJOR.$MINOR.x
   $ git merge --ff-only release
   $ git push upstream v$MAJOR.$MINOR.x
   $ git push upstream v$MAJOR.$MINOR.$PATCH
   ```
6. Close the release milestone.

Build Artifacts
---------------

Trigger build as described in "Auto releasing using kokoro" at
go/grpc/java/releasing.

It runs three jobs on Kokoro, one on each platform. See their scripts:
`linux_artifacts.sh`, `windows.bat`, and `unix.sh` (called directly for OS X;
called within the Docker environment on Linux). The mvn-artifacts/ outputs of
each script is combined into a single folder and then processed by
`upload_artifacts.sh`, which signs the files and uploads to Sonatype.

Releasing on Maven Central
--------------------------

Once all of the artifacts have been pushed to the staging repository, the
repository should have been closed by `upload_artifacts.sh`. Closing triggers
several sanity checks on the repository. If this completes successfully, the
repository can then be `released`, which will begin the process of pushing the
new artifacts to Maven Central (the staging repository will be destroyed in the
process). You can see the complete process for releasing to Maven Central on the
[OSSRH site](http://central.sonatype.org/pages/releasing-the-deployment.html).

Build interop container image
-----------------------------

We have containers for each release to detect compatibility regressions with old
releases. Generate one for the new release by following the
[GCR image generation instructions](https://github.com/grpc/grpc/blob/master/tools/interop_matrix/README.md#step-by-step-instructions-for-adding-a-gcr-image-for-a-new-release-for-compatibility-test).

Update README.md
----------------
After waiting ~1 day and verifying that the release appears on [Maven
Central](http://mvnrepository.com/), cherry-pick the commit that updated the
README into the master branch and go through review process.

```
$ git checkout -b bump-readme master
$ git cherry-pick v$MAJOR.$MINOR.$PATCH^
```

Update version referenced by tutorials
--------------------------------------

Update the `grpc_java_release_tag` in
[\_data/config.yml](https://github.com/grpc/grpc.github.io/blob/master/_data/config.yml)
of the grpc.github.io repository.

Notify the Community
--------------------
Finally, document and publicize the release.

1. Add [Release Notes](https://github.com/grpc/grpc-java/releases) for the new tag.
   The description should include any major fixes or features since the last release.
   You may choose to add links to bugs, PRs, or commits if appropriate.
2. Post a release announcement to [grpc-io](https://groups.google.com/forum/#!forum/grpc-io)
   (`grpc-io@googlegroups.com`). The title should be something that clearly identifies
   the release (e.g.`GRPC-Java <tag> Released`).
    1. Check if JCenter has picked up the new release (https://jcenter.bintray.com/io/grpc/)
       and include its availability in the release announcement email. JCenter should mirror
       everything on Maven Central, but users have reported delays.

Update Hosted Javadoc
---------------------

Now we need to update gh-pages with the new Javadoc:

```bash
git checkout gh-pages
rm -r javadoc/
wget -O grpc-all-javadoc.jar "http://search.maven.org/remotecontent?filepath=io/grpc/grpc-all/$MAJOR.$MINOR.$PATCH/grpc-all-$MAJOR.$MINOR.$PATCH-javadoc.jar"
unzip -d javadoc grpc-all-javadoc.jar
patch -p1 < ga.patch
rm grpc-all-javadoc.jar
rm -r javadoc/META-INF/
git add -A javadoc
git commit -m "Javadoc for $MAJOR.$MINOR.$PATCH"
```

Push gh-pages to the main repository and verify the current version is [live
on grpc.io](https://grpc.io/grpc-java/javadoc/).

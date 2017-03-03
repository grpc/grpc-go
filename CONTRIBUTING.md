# How to submit a bug report

If you received an error message, please include it and any exceptions.

We commonly need to know what platform you are on:
 * JDK/JRE version (i.e., ```java -version```)
 * Operating system (i.e., ```uname -a```)

# How to contribute

We definitely welcome patches and contributions to grpc! Here are some
guideline and information about how to do so.

## Before getting started

In order to protect both you and ourselves, you will need to sign the
[Contributor License Agreement](https://cla.developers.google.com/clas).

We follow the [Google Java Style
Guide](https://google-styleguide.googlecode.com/svn/trunk/javaguide.html). Our
build automatically will provide warnings for style issues.
[Eclipse](https://raw.githubusercontent.com/google/styleguide/gh-pages/eclipse-java-google-style.xml)
and
[IntelliJ](https://raw.githubusercontent.com/google/styleguide/gh-pages/intellij-java-google-style.xml)
style configurations are commonly useful. For IntelliJ 14, copy the style to
`~/.IdeaIC14/config/codestyles/`, start IntelliJ, go to File > Settings > Code
Style, and set the Scheme to `GoogleStyle`.

If planning on making a large change, feel free to [create an issue on
GitHub](https://github.com/grpc/grpc-java/issues/new), visit the [#grpc IRC
channel on Freenode](http://webchat.freenode.net/?channels=grpc), or send an
email to [grpc-io@googlegroups.com](grpc-io@googlegroups.com) to discuss
beforehand.

## Pull Requests & Commits

We have few conventions for keeping history clean and making code reviews easier
for reviewers:

* First line of commit messages should be in format of

  `package-name: summary of change`

  where the summary finishes the sentence: `This commit improves gRPC to ____________.`

  for example:

  `core,netty,interop-testing: add capacitive duractance to turbo encabulators`

* Every time you receive a feedback on your pull request, push changes that
  address it as a separate one or multiple commits with a descriptive commit
  message (try avoid using vauge `addressed pr feedback` type of messages).

  Project maintainers are obligated to squash those commits into one
  merging.

## Proposing changes

Make sure that `./gradlew build` (`gradlew build` on Windows) completes
successfully without any new warnings. Then create a Pull Request with your
changes. When the changes are accepted, they will be merged or cherry-picked by
a gRPC core developer.

## Running tests

### Jetty ALPN setup for IntelliJ

The tests in interop-testing project require jetty-alpn agent running in the background
otherwise they'll fail. Here are instructions on how to setup IntellJ IDEA to enable running
those tests in IDE:

* Settings -> Build Tools -> Gradle -> Runner -> select Gradle Test Runner
* View -> Tool Windows -> Gradle -> Edit Run Configuration -> Defaults -> JUnit -> Before lauch -> + -> Run Gradle task, enter the task in the build.gradle that sets the javaagent.

Step 1 must be taken, otherwise by the default JUnit Test Runner running a single test in IDE will trigger all the tests.

@rem ##########################################################################
@rem
@rem Script to set up Kokoro worker and run Windows tests
@rem
@rem ##########################################################################

type c:\VERSION

@rem Enter repo root
cd /d %~dp0\..\..

set WORKSPACE=T:\src\github\grpc-java
set ESCWORKSPACE=%WORKSPACE:\=\\%


@rem Clear JAVA_HOME to prevent a different Java version from being used
set JAVA_HOME=
set PATH=C:\Program Files\java\jdk1.8.0_152\bin;%PATH%

cmd.exe /C "%WORKSPACE%\buildscripts\kokoro\windows32.bat" || exit /b 1
cmd.exe /C "%WORKSPACE%\buildscripts\kokoro\windows64.bat" || exit /b 1

IF DEFINED MVN_ARTIFACTS (
  mkdir mvn-artifacts
  move artifacts\x86_64 mvn-artifacts\x86_64
  move artifacts\x86_32 mvn-artifacts\x86_32
)

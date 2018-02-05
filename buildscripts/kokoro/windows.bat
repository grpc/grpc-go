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

mkdir grpc-java-helper

cd grpc-java-helper

@rem Clear JAVA_HOME to prevent a different Java version from being used
set JAVA_HOME=
set PATH=C:\Program Files\java\jdk1.8.0_152\bin;%PATH%
call "%VS120COMNTOOLS%\vsvars32.bat"
call "%WORKSPACE%\buildscripts\make_dependencies.bat"

cd "%WORKSPACE%"

echo targetArch=x86_32> gradle.properties
echo failOnWarnings=true>> gradle.properties
echo vcProtobufLibs=%ESCWORKSPACE%\\grpc-java-helper\\protobuf-%PROTOBUF_VER%\\cmake\\build\\Release>> gradle.properties
echo vcProtobufInclude=%ESCWORKSPACE%\\grpc-java-helper\\protobuf-%PROTOBUF_VER%\\cmake\\build\\include>> gradle.properties

cmd.exe /C "%WORKSPACE%\gradlew.bat build"
set GRADLEEXIT=%ERRORLEVEL%

@rem Rename test results .xml files to format parsable by Kokoro
@echo off
for /r %%F in (TEST-*.xml) do (
  mkdir "%%~dpnF"
  move "%%F" "%%~dpnF\sponge_log.xml" >NUL
)
@echo on

exit %GRADLEEXIT%

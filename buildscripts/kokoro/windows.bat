@rem ##########################################################################
@rem
@rem Script to set up Kokoro worker and run Windows tests
@rem
@rem ##########################################################################

@rem Enter repo root
cd /d %~dp0\..\..

set WORKSPACE=T:\src\github\grpc-java
set ESCWORKSPACE=%WORKSPACE:\=\\%

mkdir grpc-java-helper

@rem Install 7za
@rem TODO(mattkwong): After Windows tests are no longer running on Jenkins, 7za
@rem doesn't need to be installed and make_dependencies.bat can use unzip instead
wget http://www.7-zip.org/a/7za920.zip
unzip 7za920.zip
mv 7za.exe grpc-java-helper

cd grpc-java-helper

call "%VS120COMNTOOLS%\vsvars32.bat"
call "%WORKSPACE%\buildscripts\make_dependencies.bat"

cd "%WORKSPACE%"

echo targetArch=x86_32> gradle.properties
echo failOnWarnings=true>> gradle.properties
echo vcProtobufLibs=%ESCWORKSPACE%\\grpc-java-helper\\protobuf-%PROTOBUF_VER%\\cmake\\build\\Release>> gradle.properties
echo vcProtobufInclude=%ESCWORKSPACE%\\grpc-java-helper\\protobuf-%PROTOBUF_VER%\\cmake\\build\\include>> gradle.properties

cmd.exe /C "%WORKSPACE%\gradlew.bat build"

@rem Rename test results .xml files to format parsable by Kokoro
for /r %%F in (TEST-*.xml) do (
  mkdir "%%~dpnF"
  move "%%F" "%%~dpnF\sponge_log.xml"
)

exit %%ERRORLEVEL%%

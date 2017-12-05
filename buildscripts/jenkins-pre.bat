if NOT EXIST grpc-java-helper mkdir grpc-java-helper
cd grpc-java-helper

call "%VS120COMNTOOLS%\vsvars32.bat"
call "%WORKSPACE%\buildscripts\make_dependencies.bat"

cd "%WORKSPACE%"

set ESCWORKSPACE=%WORKSPACE:\=\\%

echo targetArch=x86_32> gradle.properties
echo failOnWarnings=true>> gradle.properties
echo vcProtobufLibs=%ESCWORKSPACE%\\grpc-java-helper\\protobuf-%PROTOBUF_VER%\\cmake\\build\\Release>> gradle.properties
echo vcProtobufInclude=%ESCWORKSPACE%\\grpc-java-helper\\protobuf-%PROTOBUF_VER%\\cmake\\build\\include>> gradle.properties

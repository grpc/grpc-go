CC=g++
CFLAGS=-c -Wall `pkg-config --cflags protobuf` -std=c++11
LDFLAGS=-g
LDLIBS=`pkg-config --libs protobuf` -lprotoc
SOURCES=go_generator.cc go_plugin.cc
OBJECTS=$(SOURCES:.cc=.o)
EXECUTABLE=go_plugin

all: $(EXECUTABLE)

$(EXECUTABLE): $(OBJECTS)
	$(CC) $(LDFLAGS) $(OBJECTS) -o $@ $(LDLIBS)

.cc.o:
	$(CC) $(CFLAGS) $< -o $@

clean:
	$(RM) $(OBJECTS) $(EXECUTABLE)

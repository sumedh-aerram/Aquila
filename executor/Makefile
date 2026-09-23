# Native Linux (and Darwin rlimit) process supervisor. Not a container runtime.
CXX      ?= c++
CXXFLAGS ?= -std=c++20 -O2 -Wall -Wextra -Werror -Iinclude
LDFLAGS  ?=

.PHONY: all test clean

all: aquila-exec aquila-exec-test

aquila-exec: src/exec.cpp src/main.cpp include/aquila/exec.hpp
	$(CXX) $(CXXFLAGS) -o $@ src/exec.cpp src/main.cpp $(LDFLAGS)

aquila-exec-test: src/exec.cpp tests/exec_test.cpp include/aquila/exec.hpp
	$(CXX) $(CXXFLAGS) -o $@ src/exec.cpp tests/exec_test.cpp $(LDFLAGS)

test: aquila-exec-test
	./aquila-exec-test

clean:
	rm -f aquila-exec aquila-exec-test

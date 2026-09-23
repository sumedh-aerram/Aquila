#include "aquila/exec.hpp"

#include <cstdlib>
#include <iostream>
#include <string>

namespace {

int fail(const char* msg) {
  std::cerr << "exec_test: " << msg << "\n";
  return 1;
}

}  // namespace

int main() {
  {
    const aquila::Result r = aquila::run({}, aquila::Limits{});
    if (r.error.find("command") == std::string::npos) {
      return fail("empty argv");
    }
  }
  {
    const aquila::Result r =
        aquila::run({"/bin/true", "/var/run/docker.sock"}, aquila::Limits{});
    if (r.error.find("docker.sock") == std::string::npos) {
      return fail("docker.sock");
    }
  }
  {
    aquila::Limits lim;
    lim.timeout_ms = 5000;
    const aquila::Result r = aquila::run({"/bin/echo", "ok"}, lim);
    if (!r.error.empty() || r.exit_code != 0 || r.timed_out) {
      return fail("echo");
    }
  }
  {
    aquila::Limits lim;
    lim.timeout_ms = 200;
    const aquila::Result r = aquila::run({"/bin/sleep", "5"}, lim);
    if (!r.timed_out) {
      return fail("timeout");
    }
  }
#ifndef __linux__
  {
    aquila::Limits lim;
    lim.net_none = true;
    const aquila::Result r = aquila::run({"/bin/true"}, lim);
    if (r.error.find("linux") == std::string::npos) {
      return fail("net isolation darwin");
    }
  }
#endif
  return 0;
}

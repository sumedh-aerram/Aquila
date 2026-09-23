#pragma once

#include <cstdint>
#include <string>
#include <vector>

namespace aquila {

struct Limits {
  int timeout_ms = 5000;
  std::uint64_t as_bytes = 64ull * 1024 * 1024;
  int cpu_seconds = 2;
  int nproc = 64;
  int nofile = 64;
  bool net_none = false;
};

struct Result {
  int exit_code = -1;
  int signal = 0;
  bool timed_out = false;
  int duration_ms = 0;
  std::string error;
};

// run executes argv[0] with rlimits and a timeout. It never mounts docker.sock.
Result run(const std::vector<std::string>& argv, const Limits& lim);

}  // namespace aquila

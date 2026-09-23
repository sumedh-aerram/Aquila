#include "aquila/exec.hpp"

#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iostream>
#include <string>
#include <vector>

namespace {

void usage(std::ostream& os) {
  os << "usage: aquila-exec [--timeout-ms N] [--as-bytes N] [--cpu-seconds N] "
        "[--nproc N] [--nofile N] [--net none] [--result PATH] -- command...\n";
}

bool parse_i(const char* s, int& out) {
  char* end = nullptr;
  const long v = std::strtol(s, &end, 10);
  if (end == s || *end != '\0' || v < 0 || v > 1'000'000'000L) {
    return false;
  }
  out = static_cast<int>(v);
  return true;
}

bool parse_u64(const char* s, std::uint64_t& out) {
  char* end = nullptr;
  const unsigned long long v = std::strtoull(s, &end, 10);
  if (end == s || *end != '\0') {
    return false;
  }
  out = static_cast<std::uint64_t>(v);
  return true;
}

std::string json_escape(const std::string& s) {
  std::string o;
  o.reserve(s.size());
  for (char c : s) {
    switch (c) {
      case '\\':
        o += "\\\\";
        break;
      case '"':
        o += "\\\"";
        break;
      case '\n':
        o += "\\n";
        break;
      default:
        o += c;
        break;
    }
  }
  return o;
}

std::string to_json(const aquila::Result& r) {
  std::string err = json_escape(r.error);
  return std::string{"{\"exit_code\":"} + std::to_string(r.exit_code) +
         ",\"signal\":" + std::to_string(r.signal) +
         ",\"timed_out\":" + (r.timed_out ? "true" : "false") +
         ",\"duration_ms\":" + std::to_string(r.duration_ms) + ",\"error\":\"" +
         err + "\"}\n";
}

}  // namespace

int main(int argc, char** argv) {
  aquila::Limits lim;
  std::string result_path;
  int i = 1;
  for (; i < argc; ++i) {
    const char* a = argv[i];
    if (std::strcmp(a, "--") == 0) {
      ++i;
      break;
    }
    if (std::strcmp(a, "--help") == 0 || std::strcmp(a, "-h") == 0) {
      usage(std::cout);
      return 0;
    }
    if (std::strcmp(a, "--net") == 0) {
      if (i + 1 >= argc || std::strcmp(argv[++i], "none") != 0) {
        usage(std::cerr);
        return 2;
      }
      lim.net_none = true;
      continue;
    }
    if (std::strcmp(a, "--result") == 0) {
      if (i + 1 >= argc) {
        usage(std::cerr);
        return 2;
      }
      result_path = argv[++i];
      continue;
    }
    int n = 0;
    std::uint64_t u = 0;
    if (std::strcmp(a, "--timeout-ms") == 0 && i + 1 < argc && parse_i(argv[++i], n)) {
      lim.timeout_ms = n;
      continue;
    }
    if (std::strcmp(a, "--as-bytes") == 0 && i + 1 < argc && parse_u64(argv[++i], u)) {
      lim.as_bytes = u;
      continue;
    }
    if (std::strcmp(a, "--cpu-seconds") == 0 && i + 1 < argc && parse_i(argv[++i], n)) {
      lim.cpu_seconds = n;
      continue;
    }
    if (std::strcmp(a, "--nproc") == 0 && i + 1 < argc && parse_i(argv[++i], n)) {
      lim.nproc = n;
      continue;
    }
    if (std::strcmp(a, "--nofile") == 0 && i + 1 < argc && parse_i(argv[++i], n)) {
      lim.nofile = n;
      continue;
    }
    usage(std::cerr);
    return 2;
  }
  std::vector<std::string> cmd;
  for (; i < argc; ++i) {
    cmd.emplace_back(argv[i]);
  }
  const aquila::Result r = aquila::run(cmd, lim);
  const std::string body = to_json(r);
  if (!result_path.empty()) {
    std::ofstream f(result_path);
    if (!f) {
      std::cerr << "aquila-exec: cannot write result\n";
      return 2;
    }
    f << body;
  } else {
    std::cerr << body;
  }
  if (!r.error.empty() && !r.timed_out) {
    return 2;
  }
  if (r.timed_out) {
    return 124;
  }
  return r.exit_code >= 0 && r.exit_code < 128 ? r.exit_code : 1;
}

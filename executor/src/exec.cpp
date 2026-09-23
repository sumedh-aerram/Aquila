#include "aquila/exec.hpp"

#include <cerrno>
#include <chrono>
#include <cstring>
#include <string>
#include <vector>

#include <signal.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#ifdef __linux__
#include <sched.h>
#endif

namespace aquila {
namespace {

bool forbidden(const std::vector<std::string>& argv) {
  for (const auto& a : argv) {
    if (a.find("docker.sock") != std::string::npos) {
      return true;
    }
  }
  return false;
}

void set_limit(int resource, rlim_t v) {
  rlimit lim{v, v};
  (void)::setrlimit(resource, &lim);
}

void apply_limits(const Limits& lim) {
  if (lim.as_bytes > 0) {
    set_limit(RLIMIT_AS, static_cast<rlim_t>(lim.as_bytes));
  }
  if (lim.cpu_seconds > 0) {
    set_limit(RLIMIT_CPU, static_cast<rlim_t>(lim.cpu_seconds));
  }
  if (lim.nproc > 0) {
    set_limit(RLIMIT_NPROC, static_cast<rlim_t>(lim.nproc));
  }
  if (lim.nofile > 0) {
    set_limit(RLIMIT_NOFILE, static_cast<rlim_t>(lim.nofile));
  }
#ifdef __linux__
  if (lim.net_none) {
    if (::unshare(CLONE_NEWNET) != 0) {
      _exit(125);
    }
  }
#endif
}

std::vector<char*> c_argv(const std::vector<std::string>& argv) {
  std::vector<char*> out;
  out.reserve(argv.size() + 1);
  for (const auto& a : argv) {
    out.push_back(const_cast<char*>(a.c_str()));
  }
  out.push_back(nullptr);
  return out;
}

}  // namespace

Result run(const std::vector<std::string>& argv, const Limits& lim) {
  Result out;
  if (argv.empty() || argv[0].empty()) {
    out.error = "command required";
    return out;
  }
  if (forbidden(argv)) {
    out.error = "docker.sock is forbidden";
    return out;
  }
#ifndef __linux__
  if (lim.net_none) {
    out.error = "net isolation requires linux";
    return out;
  }
#endif

  const auto t0 = std::chrono::steady_clock::now();
  const pid_t pid = ::fork();
  if (pid < 0) {
    out.error = std::strerror(errno);
    return out;
  }
  if (pid == 0) {
    if (::setpgid(0, 0) != 0) {
      _exit(127);
    }
    apply_limits(lim);
    auto cargs = c_argv(argv);
    ::execvp(cargs[0], cargs.data());
    _exit(127);
  }

  const int timeout_ms = lim.timeout_ms > 0 ? lim.timeout_ms : 5000;
  int status = 0;
  bool timed_out = false;
  for (;;) {
    const pid_t w = ::waitpid(pid, &status, WNOHANG);
    if (w < 0) {
      if (errno == EINTR) {
        continue;
      }
      ::kill(-pid, SIGKILL);
      (void)::waitpid(pid, &status, 0);
      out.error = std::strerror(errno);
      return out;
    }
    if (w == pid) {
      break;
    }
    const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
                             std::chrono::steady_clock::now() - t0)
                             .count();
    if (elapsed >= timeout_ms) {
      timed_out = true;
      ::kill(-pid, SIGKILL);
      (void)::waitpid(pid, &status, 0);
      break;
    }
    timespec ts{};
    ts.tv_nsec = 10 * 1000 * 1000;
    ::nanosleep(&ts, nullptr);
  }

  const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(
                           std::chrono::steady_clock::now() - t0)
                           .count();
  out.duration_ms = static_cast<int>(elapsed);
  out.timed_out = timed_out;
  if (timed_out) {
    out.exit_code = 137;
    out.signal = SIGKILL;
    return out;
  }
  if (WIFEXITED(status)) {
    out.exit_code = WEXITSTATUS(status);
  } else if (WIFSIGNALED(status)) {
    out.signal = WTERMSIG(status);
    out.exit_code = 128 + out.signal;
  }
  return out;
}

}  // namespace aquila

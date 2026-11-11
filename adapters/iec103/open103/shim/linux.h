// SPDX-License-Identifier: GPL-3.0-or-later
//
// Linux stand-ins for the Windows names XVincentX/Open103's headers use. Put
// in front of every translation unit with -include; Open103's sources are
// compiled unchanged.
#pragma once
#include <cstdio>
#include <cstring>
#include <ctime>
#include <sstream>
#include <string>
#include <sys/time.h>

// gettimeofday.h is Windows-only (FILETIME); its include guard keeps it out.
#define GETTIMEOFDAY_H 1

inline void OutputDebugStringA(const std::string &s) { std::fprintf(stderr, "[open103] %s\n", s.c_str()); }
inline void OutputDebugStringA(const char *s) { std::fprintf(stderr, "[open103] %s\n", s); }

// IEC8705103Manager.h calls Logger::ToString, which Open103 never defines.
struct Logger {
  template <class T>
  static std::string ToString(const T &v) {
    std::ostringstream o;
    o << v;
    return o.str();
  }
};

inline int localtime_s(struct tm *out, const time_t *t) { return localtime_r(t, out) ? 0 : 1; }

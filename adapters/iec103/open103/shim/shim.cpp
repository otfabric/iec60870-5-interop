// SPDX-License-Identifier: GPL-3.0-or-later
//
// A C interface to XVincentX/Open103's IEC8705103Manager for the adapter
// (Program.cs, through P/Invoke), and the CommunicationPort Open103 needs:
// one FT 1.2 frame per Read, over a TCP connection. Open103's protocol code
// is used as it is; everything here is the port and the calls.
#include <arpa/inet.h>
#include <fcntl.h>
#include <netdb.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <poll.h>
#include <sys/socket.h>
#include <unistd.h>

#include <algorithm>
#include <chrono>
#include <fstream>
#include <iomanip>
#include <vector>

// The handlers of the transmission of disturbance data (DisturbanceRequest,
// DisturbanceTransfer, ...) are private; the public DisturbanceData that
// dispatches to them flows off its end without a return for the types 23 to
// 30, undefined behaviour that GCC compiles to a trap or a jump. The shim
// therefore makes the class's members visible and dispatches as
// DisturbanceData does (o103_disturbance below). The standard headers are
// included above, so only Open103's own headers see the definition.
#define private public
#include "IEC8705103Manager.h"
#undef private

namespace {

using clock_type = std::chrono::steady_clock;

struct Deadline {};  // thrown out of Open103's loops when a call ran out of time

class TcpPort : public CommunicationPort {
 public:
  TcpPort(int fd, int serviceMs) : CommunicationPort("tcp"), fd_(fd), serviceMs_(serviceMs) {}
  ~TcpPort() override { ::close(fd_); }

  int Read(unsigned char *packet, int maxlen) override {
    check();
    std::vector<unsigned char> f;
    auto until = clock_type::now() + std::chrono::milliseconds(serviceMs_);
    // Start character, then the rest of the frame by its format.
    while (true) {
      int c = byte(until);
      if (c < 0) return c == -2 ? -1 : 0;
      if (c == 0xE5 || c == 0x10 || c == 0x68) {
        f.push_back(static_cast<unsigned char>(c));
        break;
      }
    }
    size_t need = f[0] == 0xE5 ? 1 : f[0] == 0x10 ? 5 : 4;
    while (f.size() < need) {
      int c = byte(until);
      if (c < 0) return c == -2 ? -1 : 0;
      f.push_back(static_cast<unsigned char>(c));
      if (f[0] == 0x68 && f.size() == 4) need = static_cast<size_t>(f[1]) + 6;
    }
    if (static_cast<int>(f.size()) > maxlen) return 0;
    std::memcpy(packet, f.data(), f.size());
    frames.push_back(f);
    log("rx", f.data(), f.size());
    return static_cast<int>(f.size());
  }

  int Write(unsigned char *packet, int len) override {
    check();
    log("tx", packet, static_cast<size_t>(len));
    ssize_t n = ::send(fd_, packet, static_cast<size_t>(len), MSG_NOSIGNAL);
    if (n < 0) closed = true;
    return static_cast<int>(n);
  }

  void begin(int budgetMs) {
    frames.clear();
    deadline_ = clock_type::now() + std::chrono::milliseconds(budgetMs);
  }

  std::vector<std::vector<unsigned char>> frames;
  bool closed = false;

 private:
  void check() const {
    if (clock_type::now() > deadline_ || closed) throw Deadline{};
  }

  int byte(clock_type::time_point until) {
    while (true) {
      if (pos_ < len_) return buf_[pos_++];
      auto left = std::chrono::duration_cast<std::chrono::milliseconds>(until - clock_type::now()).count();
      if (left <= 0) return -1;
      pollfd p{fd_, POLLIN, 0};
      int r = ::poll(&p, 1, static_cast<int>(left));
      if (r <= 0) return -1;
      ssize_t n = ::recv(fd_, buf_, sizeof buf_, 0);
      if (n <= 0) {
        closed = true;
        return -2;
      }
      pos_ = 0;
      len_ = static_cast<size_t>(n);
    }
  }

  static void log(const char *dir, const unsigned char *b, size_t n) {
    std::fprintf(stderr, "[open103] %s ", dir);
    for (size_t i = 0; i < n; i++) std::fprintf(stderr, "%02X", b[i]);
    std::fprintf(stderr, "\n");
  }

  int fd_;
  int serviceMs_;
  unsigned char buf_[512];
  size_t pos_ = 0, len_ = 0;
  clock_type::time_point deadline_ = clock_type::now() + std::chrono::hours(1);
};

struct Session {
  TcpPort *port;
  IEC8705103Manager *manager;
};

int connectTo(const char *host, int port, int timeoutMs, char *err, int errlen) {
  addrinfo hints{}, *res = nullptr;
  hints.ai_socktype = SOCK_STREAM;
  char service[16];
  std::snprintf(service, sizeof service, "%d", port);
  if (int rc = ::getaddrinfo(host, service, &hints, &res); rc != 0) {
    std::snprintf(err, static_cast<size_t>(errlen), "%s: %s", host, gai_strerror(rc));
    return -1;
  }
  int fd = -1;
  for (addrinfo *a = res; a != nullptr && fd < 0; a = a->ai_next) {
    fd = ::socket(a->ai_family, a->ai_socktype, a->ai_protocol);
    if (fd < 0) continue;
    int flags = ::fcntl(fd, F_GETFL, 0);
    ::fcntl(fd, F_SETFL, flags | O_NONBLOCK);
    int rc = ::connect(fd, a->ai_addr, a->ai_addrlen);
    if (rc < 0 && errno == EINPROGRESS) {
      pollfd p{fd, POLLOUT, 0};
      int soerr = 0;
      socklen_t sl = sizeof soerr;
      if (::poll(&p, 1, timeoutMs) == 1 && ::getsockopt(fd, SOL_SOCKET, SO_ERROR, &soerr, &sl) == 0 && soerr == 0) rc = 0;
      else errno = soerr ? soerr : ETIMEDOUT;
    }
    if (rc < 0) {
      std::snprintf(err, static_cast<size_t>(errlen), "%s:%d: %s", host, port, std::strerror(errno));
      ::close(fd);
      fd = -1;
      continue;
    }
    ::fcntl(fd, F_SETFL, flags);
    int one = 1;
    ::setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof one);
  }
  ::freeaddrinfo(res);
  return fd;
}

// Runs one call of Open103 within a time budget: 1 true, 0 false, -1 out of time or connection gone.
template <class F>
int call(Session *s, int budgetMs, F f) {
  s->port->begin(budgetMs);
  try {
    return f() ? 1 : 0;
  } catch (const Deadline &) {
    return -1;
  }
}

}  // namespace

extern "C" {

void *o103_open(const char *host, int port, int connectMs, int address, int serviceMs, char *err, int errlen) {
  int fd = connectTo(host, port, connectMs, err, errlen);
  if (fd < 0) return nullptr;
  auto *s = new Session;
  s->port = new TcpPort(fd, serviceMs);
  s->manager = new IEC8705103Manager(s->port, static_cast<unsigned char>(address));
  return s;
}

int o103_connected(void *h) { return static_cast<Session *>(h)->port->closed ? 0 : 1; }

// The library's start-up: reset of the remote link, status of link, class 1
// until the identification arrived.
int o103_station_init(void *h, int budgetMs) {
  auto *s = static_cast<Session *>(h);
  return call(s, budgetMs, [&] { return s->manager->StationInit(); });
}

int o103_poll(void *h, int cls, int budgetMs) {
  auto *s = static_cast<Session *>(h);
  return call(s, budgetMs, [&] {
    const void *p = nullptr;
    size_t n = 0;
    return s->manager->GetNextADSU(&p, &n, static_cast<unsigned char>(cls));
  });
}

int o103_general_interrogation(void *h, int scn, int budgetMs) {
  auto *s = static_cast<Session *>(h);
  return call(s, budgetMs, [&] { return s->manager->GeneralInterrogation(static_cast<unsigned char>(scn)); });
}

int o103_time_sync(void *h, long long unixSeconds, int budgetMs) {
  auto *s = static_cast<Session *>(h);
  return call(s, budgetMs, [&] {
    time_t t = static_cast<time_t>(unixSeconds);
    timeval tv{};
    return s->manager->TimeSync(&t, &tv);
  });
}

int o103_general_command(void *h, int fun, int inf, int dco, int rii, int budgetMs) {
  auto *s = static_cast<Session *>(h);
  return call(s, budgetMs, [&] {
    return s->manager->CommandTrasmission(static_cast<IEC8705103Manager::Command>(inf), static_cast<unsigned char>(dco),
                                          static_cast<unsigned char>(rii), fun);
  });
}

// The frames the port read during the last call: each as one length octet
// pair (big endian) and the frame. Returns the number of octets written.
int o103_frames(void *h, unsigned char *out, int outlen) {
  auto *s = static_cast<Session *>(h);
  int n = 0;
  for (const auto &f : s->port->frames) {
    if (n + 2 + static_cast<int>(f.size()) > outlen) break;
    out[n++] = static_cast<unsigned char>(f.size() >> 8);
    out[n++] = static_cast<unsigned char>(f.size() & 0xff);
    std::memcpy(out + n, f.data(), f.size());
    n += static_cast<int>(f.size());
  }
  return n;
}

// Hands one received ASDU of the transmission of disturbance data (types 23
// to 31) to IEC8705103Manager's handler for its type, as DisturbanceData
// dispatches them; the handler sends the next order or acknowledgement
// itself. Returns 1 when the ASDU 31 ended the whole disturbance data and
// was acknowledged positively (TOO 64), 0 for any other ASDU, -1 out of
// time. The handlers read their fixed layouts past the end of a short ASDU,
// so the ASDU is handed over in a zeroed buffer of the largest frame.
int o103_disturbance(void *h, const unsigned char *asdu, int len, int budgetMs) {
  auto *s = static_cast<Session *>(h);
  unsigned char buf[512] = {0};
  std::memcpy(buf, asdu, static_cast<size_t>(std::min(len, 255)));
  return call(s, budgetMs, [&] {
    IEC8705103Manager *m = s->manager;
    switch (buf[0]) {
      case 23:
        m->DisturbanceRequest(buf);
        return false;
      case 26:
        m->DisturbanceTransfer(buf);
        return false;
      case 27:
        m->DisturbanceChannel(buf);
        return false;
      case 28:
        m->DisturbanceTags(buf);
        return false;
      case 29:
        m->DisturbanceTagsGet(buf);
        return false;
      case 30:
        m->DisturbanceChannelGet(buf);
        return false;
      case 31:
        return m->DisturbanceEnd(buf);
      default:
        return false;
    }
  });
}

// What IEC8705103Manager holds of the last disturbance (GetDisturbanceData):
// FaultNumber, SamplingTime, the channel count and elements per channel,
// and the time of day of the first value.
void o103_disturbance_head(void *h, int *out) {
  const auto &d = static_cast<Session *>(h)->manager->GetDisturbanceData();
  out[0] = d.FaultNumber;
  out[1] = d.SamplingTime;
  out[2] = d.ChannelList.Count;
  out[3] = d.ChannelList.ChannelElements;
  out[4] = d.startTime.GetMilliseconds();
  out[5] = d.startTime.GetMinutes();
  out[6] = d.startTime.GetHours();
  out[7] = d.TagsList.TagsCount;
}

// The tags of ASDU 29 number i as Open103 stored them: TAP, then FUN, INF,
// DPI per tag. Returns the number of tags (NOT).
int o103_disturbance_tags(void *h, int i, int *out, int max) {
  const auto &d = static_cast<Session *>(h)->manager->GetDisturbanceData();
  if (i < 0 || i >= d.TagsList.TagsCount || i >= MAX_DIST_COUNT) return 0;
  const auto &t = d.TagsList.TagsHeader[i];
  out[0] = t.TAP;
  int n = std::min<int>(t.NOT, (max - 1) / 3);
  for (int k = 0; k < n; k++) {
    out[1 + 3 * k] = t.TagsValue[k].FType;
    out[2 + 3 * k] = t.TagsValue[k].In;
    out[3 + 3 * k] = t.TagsValue[k].DIP;
  }
  return n;
}

// Channel acc as Open103 stored it: RPV, RSV, RFA and its first n values
// (SDV, indexed by the value number Open103 computed). Returns the ACC of
// the last ASDU 30 stored for it (0: none).
int o103_disturbance_channel(void *h, int acc, float *ratings, int *values, int n) {
  const auto &d = static_cast<Session *>(h)->manager->GetDisturbanceData();
  if (acc < 0 || acc >= MAX_DIST_COUNT) return 0;
  const auto &c = d.ChannelList.Channels[acc];
  ratings[0] = c.RPV;
  ratings[1] = c.RSV;
  ratings[2] = c.RFA;
  for (int k = 0; k < n && k < MAX_SDV_COUNT; k++) values[k] = c.SDV[k];
  return c.Header.ACC;
}

void o103_close(void *h) {
  auto *s = static_cast<Session *>(h);
  delete s->manager;  // deletes nothing of the port
  delete s->port;
  delete s;
}
}

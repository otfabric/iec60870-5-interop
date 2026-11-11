/* SPDX-License-Identifier: GPL-3.0-or-later */
/*
 * The serial line of lib60870-C is a tty; the contract's line is one TCP
 * connection that carries the FT 1.2 byte stream unchanged, like a terminal
 * server. This file is the converter between the two: a pseudo terminal whose
 * slave side lib60870 opens as its serial port, and a thread that copies
 * bytes between the pty's master side and the TCP connection.
 *
 * The pty lives as long as the process, so a station keeps its link state
 * machine (and lib60870 its port) across connections. While no connection is
 * up, what the station writes is read and dropped, as on a line nobody
 * listens to.
 */
#include "common.h"

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <poll.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <termios.h>
#include <unistd.h>

struct Bridge {
    int master;
    int slaveKeep; /* keeps the slave side open, so the master never sees EIO */
    char path[128];
    int listenFd;
    int conn;
    char peer[80];
    pthread_t thread;
    bool started;
    atomic_bool running;
    atomic_int closedCount;
    atomic_int openedCount;
    atomic_int armedCount; /* the station is ready for connection number armedCount */
    atomic_bool closed;
};

Bridge* Bridge_create(void)
{
    Bridge* b = calloc(1, sizeof(*b));
    b->conn = -1;
    b->listenFd = -1;
    b->master = posix_openpt(O_RDWR | O_NOCTTY);
    if (b->master < 0 || grantpt(b->master) != 0 || unlockpt(b->master) != 0) {
        logf_("cannot open a pseudo terminal: %s", strerror(errno));
        free(b);
        return NULL;
    }
    snprintf(b->path, sizeof(b->path), "%s", ptsname(b->master));
    b->slaveKeep = open(b->path, O_RDWR | O_NOCTTY);
    if (b->slaveKeep < 0) {
        logf_("cannot open %s: %s", b->path, strerror(errno));
        free(b);
        return NULL;
    }
    /* Raw before lib60870 opens it: it changes only some of the flags. */
    struct termios t;
    tcgetattr(b->slaveKeep, &t);
    cfmakeraw(&t);
    tcsetattr(b->slaveKeep, TCSANOW, &t);
    atomic_store(&b->running, true);
    return b;
}

const char* Bridge_ptyPath(Bridge* b) { return b->path; }
int Bridge_closedCount(Bridge* b) { return atomic_load(&b->closedCount); }
int Bridge_openedCount(Bridge* b) { return atomic_load(&b->openedCount); }
int Bridge_armedCount(Bridge* b) { return atomic_load(&b->armedCount); }
void Bridge_arm(Bridge* b, int n) { atomic_store(&b->armedCount, n); }
bool Bridge_isClosed(Bridge* b) { return atomic_load(&b->closed); }

static bool writeAll(int fd, const uint8_t* p, ssize_t n)
{
    while (n > 0) {
        ssize_t w = write(fd, p, (size_t)n);
        if (w < 0) {
            if (errno == EINTR || errno == EAGAIN)
                continue;
            return false;
        }
        p += w;
        n -= w;
    }
    return true;
}

static void peerName(int fd, char* out, size_t size)
{
    struct sockaddr_storage ss;
    socklen_t len = sizeof(ss);
    char host[64] = "?";
    int port = 0;
    if (getpeername(fd, (struct sockaddr*)&ss, &len) == 0) {
        if (ss.ss_family == AF_INET) {
            struct sockaddr_in* s = (struct sockaddr_in*)&ss;
            inet_ntop(AF_INET, &s->sin_addr, host, sizeof(host));
            port = ntohs(s->sin_port);
        } else if (ss.ss_family == AF_INET6) {
            struct sockaddr_in6* s = (struct sockaddr_in6*)&ss;
            inet_ntop(AF_INET6, &s->sin6_addr, host, sizeof(host));
            port = ntohs(s->sin6_port);
        }
    }
    snprintf(out, size, "%s:%d", host, port);
}

static void connectionEvent(const char* name, const char* peer)
{
    cJSON* e = cJSON_CreateObject();
    cJSON_AddStringToObject(e, "event", name);
    cJSON_AddStringToObject(e, "peer", peer);
    emitJson(e);
}

static void endConnection(Bridge* b, bool server)
{
    close(b->conn);
    b->conn = -1;
    atomic_fetch_add(&b->closedCount, 1);
    atomic_store(&b->closed, true);
    if (server)
        connectionEvent("connection-closed", b->peer);
}

/* Copies what is readable; false when the TCP side ended. */
static bool pump(Bridge* b, struct pollfd* fds, int nfds)
{
    uint8_t buf[512];
    (void)nfds;
    if (fds[0].revents & (POLLIN | POLLERR | POLLHUP)) {
        ssize_t n = read(b->master, buf, sizeof(buf));
        if (n > 0 && b->conn >= 0 && !writeAll(b->conn, buf, n))
            return false;
    }
    if (b->conn >= 0 && (fds[1].revents & (POLLIN | POLLERR | POLLHUP))) {
        ssize_t n = read(b->conn, buf, sizeof(buf));
        if (n <= 0)
            return false;
        if (!writeAll(b->master, buf, n))
            return false;
    }
    return true;
}

static void* serveThread(void* arg)
{
    Bridge* b = arg;
    while (atomic_load(&b->running)) {
        /* The connection's bytes wait until the station is ready for it. */
        bool armed = atomic_load(&b->armedCount) == atomic_load(&b->openedCount);
        struct pollfd fds[3] = {
            { .fd = b->master, .events = POLLIN },
            { .fd = b->conn, .events = armed ? POLLIN : 0 },
            { .fd = b->listenFd, .events = POLLIN },
        };
        if (poll(fds, 3, armed ? 100 : 2) < 0) {
            if (errno == EINTR)
                continue;
            break;
        }
        if (fds[2].revents & POLLIN) {
            int fd = accept(b->listenFd, NULL, NULL);
            if (fd >= 0) {
                if (b->conn >= 0) {
                    /* One line, one controlling station. */
                    char other[80];
                    peerName(fd, other, sizeof(other));
                    logf_("refusing %s: a connection is already being served", other);
                    close(fd);
                } else {
                    int one = 1;
                    setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
                    b->conn = fd;
                    atomic_fetch_add(&b->openedCount, 1);
                    peerName(fd, b->peer, sizeof(b->peer));
                    connectionEvent("connection-opened", b->peer);
                    continue; /* poll the new connection next */
                }
            }
        }
        if (!pump(b, fds, 3) && b->conn >= 0)
            endConnection(b, true);
    }
    if (b->conn >= 0)
        endConnection(b, true);
    return NULL;
}

static void* attachThread(void* arg)
{
    Bridge* b = arg;
    while (atomic_load(&b->running) && b->conn >= 0) {
        struct pollfd fds[2] = {
            { .fd = b->master, .events = POLLIN },
            { .fd = b->conn, .events = POLLIN },
        };
        if (poll(fds, 2, 100) < 0) {
            if (errno == EINTR)
                continue;
            break;
        }
        if (!pump(b, fds, 2))
            endConnection(b, false);
    }
    return NULL;
}

bool Bridge_serve(Bridge* b, int listenFd)
{
    b->listenFd = listenFd;
    b->started = pthread_create(&b->thread, NULL, serveThread, b) == 0;
    return b->started;
}

bool Bridge_attach(Bridge* b, int fd)
{
    int one = 1;
    setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
    b->conn = fd;
    b->started = pthread_create(&b->thread, NULL, attachThread, b) == 0;
    return b->started;
}

void Bridge_stop(Bridge* b)
{
    atomic_store(&b->running, false);
    if (b->started)
        pthread_join(b->thread, NULL);
    if (b->conn >= 0)
        close(b->conn);
    if (b->listenFd >= 0)
        close(b->listenFd);
    close(b->slaveKeep);
    close(b->master);
    free(b);
}

int tcpListen(const char* bindAddress, int port, char* addr, size_t addrSize)
{
    struct addrinfo hints = { .ai_family = AF_UNSPEC, .ai_socktype = SOCK_STREAM, .ai_flags = AI_PASSIVE };
    struct addrinfo* res;
    char service[16];
    snprintf(service, sizeof(service), "%d", port);
    if (getaddrinfo(bindAddress, service, &hints, &res) != 0)
        return -1;
    int fd = socket(res->ai_family, SOCK_STREAM, 0);
    int one = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    if (fd < 0 || bind(fd, res->ai_addr, res->ai_addrlen) != 0 || listen(fd, 4) != 0) {
        if (fd >= 0)
            close(fd);
        freeaddrinfo(res);
        return -1;
    }
    freeaddrinfo(res);
    snprintf(addr, addrSize, "%s:%d", bindAddress, port);
    return fd;
}

int tcpConnect(const char* host, int port, int timeoutMs, char* err, size_t errSize)
{
    struct addrinfo hints = { .ai_family = AF_UNSPEC, .ai_socktype = SOCK_STREAM };
    struct addrinfo* res;
    char service[16];
    snprintf(service, sizeof(service), "%d", port);
    int rc = getaddrinfo(host, service, &hints, &res);
    if (rc != 0) {
        snprintf(err, errSize, "%s: %s", host, gai_strerror(rc));
        return -1;
    }
    int fd = -1;
    for (struct addrinfo* ai = res; ai != NULL && fd < 0; ai = ai->ai_next) {
        fd = socket(ai->ai_family, SOCK_STREAM, 0);
        if (fd < 0)
            continue;
        fcntl(fd, F_SETFL, fcntl(fd, F_GETFL) | O_NONBLOCK);
        if (connect(fd, ai->ai_addr, ai->ai_addrlen) != 0 && errno != EINPROGRESS) {
            snprintf(err, errSize, "connect: %s", strerror(errno));
            close(fd);
            fd = -1;
            continue;
        }
        struct pollfd p = { .fd = fd, .events = POLLOUT };
        int soerr = 0;
        socklen_t len = sizeof(soerr);
        if (poll(&p, 1, timeoutMs) != 1 || getsockopt(fd, SOL_SOCKET, SO_ERROR, &soerr, &len) != 0 || soerr != 0) {
            snprintf(err, errSize, "connect: %s", soerr ? strerror(soerr) : "timeout");
            close(fd);
            fd = -1;
            continue;
        }
        fcntl(fd, F_SETFL, fcntl(fd, F_GETFL) & ~O_NONBLOCK);
    }
    freeaddrinfo(res);
    return fd;
}

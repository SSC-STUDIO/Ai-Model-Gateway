from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import requests, os, json, time, pathlib, sys, logging, signal, uuid, threading
from logging.handlers import RotatingFileHandler
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

LOG = pathlib.Path(os.environ.get(
    'OPENCODE_PROXY_LOG',
    str(pathlib.Path(__file__).resolve().parent / 'opencode-proxy.log')
))
TARGET_ORIGIN = os.environ.get(
    'OPENCODE_PROXY_TARGET',
    'https://opencode.ai'
)
LOG_LEVEL = os.environ.get(
    'OPENCODE_PROXY_LOG_LEVEL',
    'INFO'
).upper()
MAX_BODY_BYTES = int(os.environ.get(
    'OPENCODE_PROXY_MAX_BODY_BYTES',
    str(100 * 1024 * 1024),  # 100 MB default
))
# CORS: allowed origin. '*' allows any origin; set to a specific value in production.
CORS_ORIGIN = os.environ.get(
    'OPENCODE_PROXY_CORS_ORIGIN',
    '*'
)
# Upstream request timeouts (seconds): connect timeout and read timeout.
CONNECT_TIMEOUT = int(os.environ.get('OPENCODE_PROXY_CONNECT_TIMEOUT', '10'))
READ_TIMEOUT = int(os.environ.get('OPENCODE_PROXY_READ_TIMEOUT', '300'))
# Interval (seconds) for periodic stats auto-save to prevent data loss on crash.
STATS_SAVE_INTERVAL = int(os.environ.get('OPENCODE_PROXY_STATS_SAVE_INTERVAL', '60'))

_LEVEL_MAP = {
    'DEBUG': logging.DEBUG,
    'INFO': logging.INFO,
    'WARNING': logging.WARNING,
    'ERROR': logging.ERROR,
}

# ---------------------------------------------------------------------------
# Persistent HTTP session with connection pooling for upstream requests
# ---------------------------------------------------------------------------
_upstream_session = requests.Session()
# Pool: 10 connections per host, keep alive 60s
_adapter = HTTPAdapter(
    pool_connections=10,
    pool_maxsize=20,
    max_retries=Retry(
        total=0,  # retries handled at proxy level
        connect=0,
        read=0,
        redirect=0,
        status=0,
        other=0,
        backoff_factor=0,
    ),
)
_upstream_session.mount('http://', _adapter)
_upstream_session.mount('https://', _adapter)
_upstream_session.headers.update({
    'User-Agent': 'AI-Model-Gateway/1.4 opencode-proxy',
    'Connection': 'keep-alive',
})

# ---------------------------------------------------------------------------
# Logger with rotation: 10 MB per file, keep 5 backups
# ---------------------------------------------------------------------------
logger = logging.getLogger('opencode-proxy')
logger.setLevel(_LEVEL_MAP.get(LOG_LEVEL, logging.INFO))
_rfh = RotatingFileHandler(
    str(LOG), maxBytes=10 * 1024 * 1024, backupCount=5, encoding='utf-8'
)
_rfh.setFormatter(logging.Formatter('%(asctime)s %(levelname)s %(message)s'))
logger.addHandler(_rfh)

# Also log to stderr so the console isn't silent
_stderr_h = logging.StreamHandler(sys.stderr)
_stderr_h.setFormatter(logging.Formatter('%(asctime)s %(levelname)s %(message)s'))
logger.addHandler(_stderr_h)

# ---------------------------------------------------------------------------
# Paths that should NOT be proxied (health / internal)
# ---------------------------------------------------------------------------
HEALTH_PATHS = frozenset({'/health', '/healthz', '/-/health'})
BLACKLIST_HEADERS = frozenset({
    'host', 'content-length', 'connection', 'accept-encoding',
})

# Streaming detection — only text/event-stream is truly streaming by content-type.
# text/plain is NOT included: non-streaming endpoints (error messages, plain text
# responses) commonly use it. When a provider sends SSE over text/plain and the
# client requested stream=true, the is_streaming flag covers that case.
_STREAM_CT_PREFIXES = ('text/event-stream',)

# ---------------------------------------------------------------------------
# Stats persistence path — same directory as the log file, JSON format
# ---------------------------------------------------------------------------
STATS_FILE = LOG.with_suffix('.stats.json')

# ---------------------------------------------------------------------------
# Accumulated proxy statistics (thread-safe)
# ---------------------------------------------------------------------------
_stats_lock = threading.Lock()
_stats = {
    'started_at': time.time(),
    'total_requests': 0,
    'total_errors': 0,
    'total_bytes_sent': 0,
    'total_latency_ms': 0,
    'status_codes': {},   # {200: N, 502: N, ...}
    'errors': {},         # {'timeout': N, 'connection_error': N, ...}
}

def _load_stats():
    """Merge saved stats from disk so cumulative totals survive restart."""
    global _stats
    if not STATS_FILE.exists():
        return
    try:
        saved = json.loads(STATS_FILE.read_text('utf-8'))
        with _stats_lock:
            # Sum merge-only fields; 'started_at' stays the current boot time.
            _stats['total_requests'] += saved.get('total_requests', 0)
            _stats['total_errors'] += saved.get('total_errors', 0)
            _stats['total_bytes_sent'] += saved.get('total_bytes_sent', 0)
            _stats['total_latency_ms'] += saved.get('total_latency_ms', 0)
            for sc, n in saved.get('status_codes', {}).items():
                _stats['status_codes'][sc] = _stats['status_codes'].get(sc, 0) + n
            for err, n in saved.get('errors', {}).items():
                _stats['errors'][err] = _stats['errors'].get(err, 0) + n
        logger.info('stats restored from %s (%d prior requests)',
                    STATS_FILE.name, saved.get('total_requests', 0))
    except Exception as exc:
        logger.warning('could not load saved stats from %s: %s', STATS_FILE.name, exc)


def _save_stats():
    """Persist current stats to disk so they survive restart."""
    try:
        with _stats_lock:
            snapshot = dict(_stats)
        # started_at is per-run; write a sentinel so load merges correctly
        snapshot.pop('started_at', None)
        STATS_FILE.parent.mkdir(parents=True, exist_ok=True)
        STATS_FILE.write_text(json.dumps(snapshot, indent=2, ensure_ascii=False), 'utf-8')
    except Exception as exc:
        logger.warning('could not save stats to %s: %s', STATS_FILE.name, exc)

# ---------------------------------------------------------------------------
# Periodic auto-save: background thread that persists stats at a configurable
# interval so cumulative totals survive a hard crash (SIGKILL, OOM, power loss)
# where the graceful shutdown handler never runs.
# ---------------------------------------------------------------------------
_stats_saver_stop = threading.Event()


def _stats_auto_save_loop():
    """Daemon thread: wake every STATS_SAVE_INTERVAL seconds and persist stats."""
    while not _stats_saver_stop.wait(STATS_SAVE_INTERVAL):
        _save_stats()
    # Final save on thread exit
    _save_stats()

# ---------------------------------------------------------------------------
# Active request tracking for graceful drain on shutdown
# ---------------------------------------------------------------------------
_active_requests = set()
_active_requests_lock = threading.Lock()


class GracefulHTTPServer(ThreadingHTTPServer):
    """ThreadingHTTPServer that tracks in-flight request threads so the
    shutdown handler can drain them before closing the upstream session."""

    allow_reuse_address = True

    def process_request(self, request, client_address):
        t = threading.Thread(
            target=self._handle_with_tracking,
            args=(request, client_address),
        )
        t.daemon = True
        t.start()

    def _handle_with_tracking(self, request, client_address):
        t = threading.current_thread()
        with _active_requests_lock:
            _active_requests.add(t)
        try:
            self.finish_request(request, client_address)
        except Exception:
            self.handle_error(request, client_address)
        finally:
            try:
                self.shutdown_request(request)
            except Exception:
                pass
            with _active_requests_lock:
                _active_requests.discard(t)


def _record_stats(status_code, bytes_sent, elapsed_ms, error_type=None):
    with _stats_lock:
        _stats['total_requests'] += 1
        _stats['total_bytes_sent'] += bytes_sent
        _stats['total_latency_ms'] += elapsed_ms
        sc = str(status_code)
        _stats['status_codes'][sc] = _stats['status_codes'].get(sc, 0) + 1
        if error_type:
            _stats['total_errors'] += 1
            _stats['errors'][error_type] = _stats['errors'].get(error_type, 0) + 1


class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, fmt, *args):
        # Suppress default http.server logging — we use our own logger
        return

    def _send_cors_headers(self, extra_origin=None):
        """Attach standard CORS headers so browser-based clients can use the proxy."""
        origin = extra_origin or CORS_ORIGIN
        self.send_header('Access-Control-Allow-Origin', origin)
        if origin != '*':
            self.send_header('Vary', 'Origin')
        self.send_header('Access-Control-Allow-Methods',
                         'GET, POST, PUT, DELETE, PATCH, OPTIONS, HEAD')
        self.send_header('Access-Control-Allow-Headers',
                         'Authorization, Content-Type, X-Api-Key, X-Request-Id')
        self.send_header('Access-Control-Max-Age', '86400')
        self.send_header('Access-Control-Expose-Headers', 'X-Request-Id')

    # ---- Health check with stats ----
    def _health(self):
        with _stats_lock:
            total = _stats['total_requests']
            avg_lat = (
                round(_stats['total_latency_ms'] / total) if total > 0 else 0
            )
            uptime = round(time.time() - _stats['started_at'])
        with _active_requests_lock:
            active = len(_active_requests)
        body = json.dumps({
            'status': 'ok',
            'proxy': 'opencode',
            'target': TARGET_ORIGIN,
            'uptime_seconds': uptime,
            'active_requests': active,
            'stats': {
                'total_requests': total,
                'total_errors': _stats['total_errors'],
                'total_bytes_sent': _stats['total_bytes_sent'],
                'avg_latency_ms': avg_lat,
                'status_codes': _stats['status_codes'],
                'errors': _stats['errors'],
                'persisted': STATS_FILE.exists(),
            },
            'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        }).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self._send_cors_headers()
        self.end_headers()
        self.wfile.write(body)

    # ---- OPTIONS preflight handler ----
    def _cors_preflight(self):
        """Handle CORS preflight requests without forwarding to upstream."""
        request_id = uuid.uuid4().hex[:12]
        logger.info('[%s] CORS preflight %s %s', request_id, self.command, self.path)
        self.send_response(204)
        self._send_cors_headers()
        self.send_header('X-Request-Id', request_id)
        self.send_header('Content-Length', '0')
        self.end_headers()

    # ---- Actual proxy logic ----
    def _proxy(self):
        request_id = uuid.uuid4().hex[:12]
        n = int(self.headers.get('content-length') or 0)

        # Reject oversized requests early
        if n > MAX_BODY_BYTES:
            logger.warning('[%s] request body too large: %d bytes (limit %d)',
                           request_id, n, MAX_BODY_BYTES)
            out = json.dumps({
                'error': 'request_too_large',
                'detail': f'Max body size is {MAX_BODY_BYTES} bytes',
            }).encode()
            self.send_response(413)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.send_header('X-Request-Id', request_id)
            self._send_cors_headers()
            self.end_headers()
            self.wfile.write(out)
            _record_stats(413, 0, 0, 'request_too_large')
            return

        body = self.rfile.read(n) if n else b''
        target = TARGET_ORIGIN + self.path

        # Build forwarded headers: strip hop-by-hop, inject request id for tracing
        headers = {
            k: v for k, v in self.headers.items()
            if k.lower() not in BLACKLIST_HEADERS
        }
        headers['X-Request-Id'] = request_id
        # Forward the original client's User-Agent so upstream can debug real clients
        # rather than seeing only the proxy's default UA from the session.
        ua = self.headers.get('User-Agent', '')
        if ua:
            headers['X-Forwarded-User-Agent'] = ua
        rec = {
            'request_id': request_id,
            'ts': time.strftime('%Y-%m-%d %H:%M:%S'),
            'method': self.command,
            'path': self.path,
            'target': target,
            'headers': {
                k: ('[REDACTED]' if k.lower() in ('authorization', 'x-api-key') else v)
                for k, v in headers.items()
            },
        }
        bytes_sent = 0
        is_streaming = False
        upstream_streaming = False
        _headers_sent = False
        t0 = time.monotonic()
        elapsed = 0
        error_type = None

        try:
            if body:
                try:
                    rec['request_json'] = json.loads(body.decode('utf-8'))
                    # Detect streaming request from body
                    req_json = rec['request_json']
                    if isinstance(req_json, dict) and req_json.get('stream'):
                        is_streaming = True
                except Exception:
                    rec['request_body'] = body[:5000].decode('utf-8', 'replace')

            resp = _upstream_session.request(
                self.command, target,
                headers=headers, data=body,
                timeout=(CONNECT_TIMEOUT, READ_TIMEOUT), stream=True,
            )
            rec['status_code'] = resp.status_code
            rec['response_headers'] = dict(resp.headers.items())

            # Determine if upstream is actually streaming
            ct = resp.headers.get('content-type', '')
            upstream_streaming = ct.startswith(_STREAM_CT_PREFIXES) or is_streaming
            rec['streaming'] = upstream_streaming

            # Send response headers to client
            self.send_response(resp.status_code)
            self.send_header('X-Request-Id', request_id)
            # For HEAD requests, forward Content-Length but strip encoding headers
            _skip = {'content-encoding', 'transfer-encoding', 'connection'}
            if self.command != 'HEAD':
                _skip.add('content-length')
            for k, v in resp.headers.items():
                if k.lower() not in _skip:
                    self.send_header(k, v)

            if self.command == 'HEAD':
                # RFC 7231 §4.3.2: HEAD must not include a message body.
                # Upstream Content-Length was already forwarded above.
                self._send_cors_headers()
                self.end_headers()
                _headers_sent = True

            elif upstream_streaming:
                # Streaming: use Transfer-Encoding: chunked
                self.send_header('Transfer-Encoding', 'chunked')
                self._send_cors_headers()
                self.end_headers()
                _headers_sent = True

                # Forward chunks as they arrive
                for chunk in resp.iter_content(chunk_size=8192):
                    if chunk:
                        self.wfile.write(f'{len(chunk):X}\r\n'.encode())
                        self.wfile.write(chunk)
                        self.wfile.write(b'\r\n')
                        bytes_sent += len(chunk)
                # Final chunk
                self.wfile.write(b'0\r\n\r\n')
            else:
                # Non-streaming: buffer and forward with Content-Length
                out = resp.content
                bytes_sent = len(out)
                rec['response_text'] = resp.text[:12000]
                self.send_header('Content-Length', str(len(out)))
                self._send_cors_headers()
                self.end_headers()
                _headers_sent = True
                self.wfile.write(out)

            elapsed = round((time.monotonic() - t0) * 1000)
            rec['bytes_sent'] = bytes_sent
            rec['elapsed_ms'] = elapsed

        except requests.exceptions.Timeout as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            timeout_type = 'connect_timeout' if 'connect' in str(e).lower() else 'read_timeout'
            error_type = timeout_type
            logger.warning('[%s] upstream %s after %dms: %s %s',
                           request_id, timeout_type, elapsed, self.command, self.path)
            if not _headers_sent:
                out = json.dumps(
                    {'error': f'upstream_{timeout_type}', 'detail': repr(e)},
                    ensure_ascii=False,
                ).encode('utf-8')
                self.send_response(504)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)

        except requests.exceptions.ConnectionError as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'connection_error'
            logger.error('[%s] upstream connection error: %s %s — %s',
                         request_id, self.command, self.path, e)
            if not _headers_sent:
                out = json.dumps(
                    {'error': 'upstream_unreachable', 'detail': repr(e)},
                    ensure_ascii=False,
                ).encode('utf-8')
                self.send_response(502)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)

        except (BrokenPipeError, ConnectionResetError) as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'client_disconnect'
            logger.warning('[%s] client disconnected after %dms: %s %s',
                           request_id, elapsed, self.command, self.path)

        except Exception as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'proxy_error'
            logger.exception('[%s] proxy error for %s %s', request_id, self.command, self.path)
            if not _headers_sent:
                out = json.dumps(
                    {'error': 'proxy_error', 'detail': repr(e)},
                    ensure_ascii=False,
                ).encode('utf-8')
                self.send_response(502)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(out)))
                self.send_header('X-Request-Id', request_id)
                self._send_cors_headers()
                self.end_headers()
                self.wfile.write(out)

        finally:
            elapsed_ms = rec.get('elapsed_ms', round((time.monotonic() - t0) * 1000))
            stream_tag = ' [streaming]' if upstream_streaming else ''
            # Structured log line (human-readable in rotated log)
            log_line = (
                f"[{request_id}] "
                f"{self.command} {self.path} -> "
                f"{rec.get('status_code', 'ERR')} "
                f"{bytes_sent}B {elapsed_ms}ms"
                f"{stream_tag}"
                f" ({rec.get('proxy_error', 'ok')})"
            )
            if rec.get('proxy_error'):
                logger.warning(log_line)
            else:
                logger.info(log_line)
            _record_stats(rec.get('status_code', 0), bytes_sent, elapsed_ms, error_type)

    # ---- Dispatchers ----
    def do_GET(self):     self._dispatch()
    def do_POST(self):    self._dispatch()
    def do_PUT(self):     self._dispatch()
    def do_DELETE(self):  self._dispatch()
    def do_PATCH(self):   self._dispatch()
    def do_OPTIONS(self): self._dispatch()
    def do_HEAD(self):    self._dispatch()

    def _dispatch(self):
        if self.command == 'OPTIONS':
            self._cors_preflight()
        elif self.path in HEALTH_PATHS:
            self._health()
        else:
            self._proxy()


# ---------------------------------------------------------------------------
# Graceful shutdown on SIGINT / SIGTERM
# ---------------------------------------------------------------------------
_server = None
_start_time = None

def _shutdown(sig, frame):
    global _server
    if _server:
        uptime = round(time.monotonic() - _start_time) if _start_time else 0
        with _active_requests_lock:
            active = len(_active_requests)
        logger.info('shutting down (signal=%s, uptime=%ds, requests=%d, active=%d)',
                     sig, uptime, _stats['total_requests'], active)
        print(f'shutting down (signal={sig}, uptime={uptime}s, '
              f'requests={_stats["total_requests"]}, active={active})', flush=True)
        # Stop periodic auto-save first so it doesn't race with the final save
        _stats_saver_stop.set()
        # Stop accepting new connections
        _server.shutdown()
        # Drain in-flight requests: wait up to 30s for active threads to finish
        drain_timeout = int(os.environ.get('OPENCODE_PROXY_DRAIN_TIMEOUT', '30'))
        if active > 0 and drain_timeout > 0:
            logger.info('draining %d active request(s) (timeout=%ds)...', active, drain_timeout)
            deadline = time.monotonic() + drain_timeout
            while time.monotonic() < deadline:
                with _active_requests_lock:
                    remaining = len(_active_requests)
                if remaining == 0:
                    break
                logger.info('draining: %d request(s) still active...', remaining)
                time.sleep(1)
            with _active_requests_lock:
                remaining = len(_active_requests)
            if remaining:
                logger.warning('drain timeout: %d request(s) still active, forcing close',
                               remaining)
            else:
                logger.info('drain complete: all requests finished')
        else:
            logger.info('no active requests, skipping drain')
    # Persist stats so totals carry over to the next run
    _save_stats()
    # Close persistent connections to upstream
    _upstream_session.close()

signal.signal(signal.SIGINT, _shutdown)
signal.signal(signal.SIGTERM, _shutdown)


if __name__ == '__main__':
    port = int(os.environ.get('OPENCODE_PROXY_PORT', '18082'))
    _start_time = time.monotonic()
    _stats['started_at'] = time.time()
    _load_stats()
    # Start periodic stats auto-save daemon (protects against crash data loss)
    if STATS_SAVE_INTERVAL > 0:
        _saver = threading.Thread(target=_stats_auto_save_loop, daemon=True)
        _saver.start()
        logger.info('stats auto-save enabled (interval=%ds)', STATS_SAVE_INTERVAL)
    logger.info('opencode proxy listening on 127.0.0.1:%d -> %s (max_body=%dMB, cors=%s)',
                port, TARGET_ORIGIN, MAX_BODY_BYTES // (1024 * 1024), CORS_ORIGIN)
    print(f'opencode proxy listening on 127.0.0.1:{port} -> {TARGET_ORIGIN} '
          f'(max_body={MAX_BODY_BYTES // (1024 * 1024)}MB, cors={CORS_ORIGIN})', flush=True)
    _server = GracefulHTTPServer(('127.0.0.1', port), H)
    _server.serve_forever()

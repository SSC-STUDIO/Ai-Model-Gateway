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

    # ---- Health check with stats ----
    def _health(self):
        with _stats_lock:
            total = _stats['total_requests']
            avg_lat = (
                round(_stats['total_latency_ms'] / total) if total > 0 else 0
            )
            uptime = round(time.time() - _stats['started_at'])
            body = json.dumps({
                'status': 'ok',
                'proxy': 'opencode',
                'target': TARGET_ORIGIN,
                'uptime_seconds': uptime,
                'stats': {
                    'total_requests': total,
                    'total_errors': _stats['total_errors'],
                    'total_bytes_sent': _stats['total_bytes_sent'],
                    'avg_latency_ms': avg_lat,
                    'status_codes': _stats['status_codes'],
                    'errors': _stats['errors'],
                },
                'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
            }).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

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
            self.end_headers()
            self.wfile.write(out)
            _record_stats(413, 0, 0, 'request_too_large')
            return

        body = self.rfile.read(n) if n else b''
        target = TARGET_ORIGIN + self.path

        headers = {
            k: v for k, v in self.headers.items()
            if k.lower() not in BLACKLIST_HEADERS
        }
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
                headers=headers, data=body, timeout=(10, 300), stream=True,
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
            for k, v in resp.headers.items():
                if k.lower() not in (
                    'content-encoding', 'transfer-encoding',
                    'connection', 'content-length',
                ):
                    self.send_header(k, v)

            if upstream_streaming:
                # Streaming: use Transfer-Encoding: chunked
                self.send_header('Transfer-Encoding', 'chunked')
                self.end_headers()

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
                self.end_headers()
                self.wfile.write(out)

            elapsed = round((time.monotonic() - t0) * 1000)
            rec['bytes_sent'] = bytes_sent
            rec['elapsed_ms'] = elapsed

        except requests.exceptions.Timeout as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'timeout'
            logger.warning('[%s] upstream timeout after %dms: %s %s',
                           request_id, elapsed, self.command, self.path)
            out = json.dumps(
                {'error': 'upstream_timeout', 'detail': repr(e)},
                ensure_ascii=False,
            ).encode('utf-8')
            self.send_response(504)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.send_header('X-Request-Id', request_id)
            self.end_headers()
            self.wfile.write(out)

        except requests.exceptions.ConnectionError as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'connection_error'
            logger.error('[%s] upstream connection error: %s %s — %s',
                         request_id, self.command, self.path, e)
            out = json.dumps(
                {'error': 'upstream_unreachable', 'detail': repr(e)},
                ensure_ascii=False,
            ).encode('utf-8')
            self.send_response(502)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.send_header('X-Request-Id', request_id)
            self.end_headers()
            self.wfile.write(out)

        except Exception as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            error_type = 'proxy_error'
            logger.exception('[%s] proxy error for %s %s', request_id, self.command, self.path)
            out = json.dumps(
                {'error': 'proxy_error', 'detail': repr(e)},
                ensure_ascii=False,
            ).encode('utf-8')
            self.send_response(502)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.send_header('X-Request-Id', request_id)
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
        if self.path in HEALTH_PATHS:
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
        logger.info('shutting down (signal=%s, uptime=%ds, requests=%d)',
                     sig, uptime, _stats['total_requests'])
        print(f'shutting down (signal={sig}, uptime={uptime}s, requests={_stats["total_requests"]})', flush=True)
        _server.shutdown()
    # Close persistent connections to upstream
    _upstream_session.close()

signal.signal(signal.SIGINT, _shutdown)
signal.signal(signal.SIGTERM, _shutdown)


if __name__ == '__main__':
    port = int(os.environ.get('OPENCODE_PROXY_PORT', '18082'))
    _start_time = time.monotonic()
    _stats['started_at'] = time.time()
    logger.info('opencode proxy listening on 127.0.0.1:%d -> %s (max_body=%dMB)',
                port, TARGET_ORIGIN, MAX_BODY_BYTES // (1024 * 1024))
    print(f'opencode proxy listening on 127.0.0.1:{port} -> {TARGET_ORIGIN} '
          f'(max_body={MAX_BODY_BYTES // (1024 * 1024)}MB)', flush=True)
    _server = ThreadingHTTPServer(('127.0.0.1', port), H)
    _server.serve_forever()

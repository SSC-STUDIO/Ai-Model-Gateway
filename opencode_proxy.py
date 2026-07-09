from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import requests, os, json, time, pathlib, sys, logging, signal
from logging.handlers import RotatingFileHandler

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

_LEVEL_MAP = {
    'DEBUG': logging.DEBUG,
    'INFO': logging.INFO,
    'WARNING': logging.WARNING,
    'ERROR': logging.ERROR,
}

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

# Streaming detection — these response content types are SSE / streaming
_STREAM_CT_PREFIXES = ('text/event-stream', 'text/plain',)


class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, fmt, *args):
        # Suppress default http.server logging — we use our own logger
        return

    # ---- Health check (no upstream needed) ----
    def _health(self):
        body = json.dumps({
            'status': 'ok',
            'proxy': 'opencode',
            'target': TARGET_ORIGIN,
            'ts': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
        }).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    # ---- Actual proxy logic ----
    def _proxy(self):
        n = int(self.headers.get('content-length') or 0)
        body = self.rfile.read(n) if n else b''
        target = TARGET_ORIGIN + self.path

        headers = {
            k: v for k, v in self.headers.items()
            if k.lower() not in BLACKLIST_HEADERS
        }
        rec = {
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

            resp = requests.request(
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
            logger.warning('upstream timeout after %dms: %s %s',
                           elapsed, self.command, self.path)
            out = json.dumps(
                {'error': 'upstream_timeout', 'detail': repr(e)},
                ensure_ascii=False,
            ).encode('utf-8')
            self.send_response(504)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.end_headers()
            self.wfile.write(out)

        except requests.exceptions.ConnectionError as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            logger.error('upstream connection error: %s %s — %s',
                         self.command, self.path, e)
            out = json.dumps(
                {'error': 'upstream_unreachable', 'detail': repr(e)},
                ensure_ascii=False,
            ).encode('utf-8')
            self.send_response(502)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.end_headers()
            self.wfile.write(out)

        except Exception as e:
            rec['proxy_error'] = repr(e)
            elapsed = round((time.monotonic() - t0) * 1000)
            rec['elapsed_ms'] = elapsed
            logger.exception('proxy error for %s %s', self.command, self.path)
            out = json.dumps(
                {'error': 'proxy_error', 'detail': repr(e)},
                ensure_ascii=False,
            ).encode('utf-8')
            self.send_response(502)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(out)))
            self.end_headers()
            self.wfile.write(out)

        finally:
            elapsed_ms = rec.get('elapsed_ms', round((time.monotonic() - t0) * 1000))
            stream_tag = ' [streaming]' if upstream_streaming else ''
            # Structured log line (human-readable in rotated log)
            log_line = (
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
        logger.info('shutting down (signal=%s, uptime=%ds)', sig, uptime)
        print(f'shutting down (signal={sig}, uptime={uptime}s)', flush=True)
        _server.shutdown()

signal.signal(signal.SIGINT, _shutdown)
signal.signal(signal.SIGTERM, _shutdown)


if __name__ == '__main__':
    port = int(os.environ.get('OPENCODE_PROXY_PORT', '18082'))
    _start_time = time.monotonic()
    logger.info('opencode proxy listening on 127.0.0.1:%d -> %s', port, TARGET_ORIGIN)
    print(f'opencode proxy listening on 127.0.0.1:{port} -> {TARGET_ORIGIN}', flush=True)
    _server = ThreadingHTTPServer(('127.0.0.1', port), H)
    _server.serve_forever()

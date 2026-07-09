from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import requests, os, json, time, pathlib, sys, logging
from logging.handlers import RotatingFileHandler

LOG = pathlib.Path(os.environ.get(
    'OPENCODE_PROXY_LOG',
    str(pathlib.Path(__file__).resolve().parent / 'opencode-proxy.log')
))
TARGET_ORIGIN = os.environ.get(
    'OPENCODE_PROXY_TARGET',
    'https://opencode.ai'
)

# ---------------------------------------------------------------------------
# Logger with rotation: 10 MB per file, keep 5 backups
# ---------------------------------------------------------------------------
logger = logging.getLogger('opencode-proxy')
logger.setLevel(logging.INFO)
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
        try:
            if body:
                try:
                    rec['request_json'] = json.loads(body.decode('utf-8'))
                except Exception:
                    rec['request_body'] = body[:5000].decode('utf-8', 'replace')

            resp = requests.request(
                self.command, target,
                headers=headers, data=body, timeout=300, stream=False,
            )
            rec['status_code'] = resp.status_code
            rec['response_headers'] = dict(resp.headers.items())
            rec['response_text'] = resp.text[:12000]
            out = resp.content

            self.send_response(resp.status_code)
            for k, v in resp.headers.items():
                if k.lower() not in (
                    'content-encoding', 'transfer-encoding',
                    'connection', 'content-length',
                ):
                    self.send_header(k, v)
            self.send_header('Content-Length', str(len(out)))
            self.end_headers()
            self.wfile.write(out)

        except requests.exceptions.Timeout as e:
            rec['proxy_error'] = repr(e)
            logger.warning('upstream timeout: %s %s', self.command, self.path)
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
            # Structured log line (human-readable in rotated log)
            log_line = (
                f"{self.command} {self.path} -> "
                f"{rec.get('status_code', 'ERR')} "
                f"({rec.get('proxy_error', 'ok')})"
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


if __name__ == '__main__':
    port = int(os.environ.get('OPENCODE_PROXY_PORT', '18082'))
    logger.info('opencode proxy listening on 127.0.0.1:%d -> %s', port, TARGET_ORIGIN)
    print(f'opencode proxy listening on 127.0.0.1:{port} -> {TARGET_ORIGIN}', flush=True)
    ThreadingHTTPServer(('127.0.0.1', port), H).serve_forever()
